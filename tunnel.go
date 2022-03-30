package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/openconfig/grpctunnel/bidi"
	tpb "github.com/openconfig/grpctunnel/proto/tunnel"
	"github.com/openconfig/grpctunnel/tunnel"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netns"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type tunnelDestinationClient struct {
	// gRPC connection towards the tunnel server
	conn *grpc.ClientConn
	// the gRPC tunnel client
	client *tunnel.Client
	// map of handler to targets
	targets map[string]*tunnelTargetDetails
}

type tunnelTargetDetails struct {
	ID          string
	Type        string
	dialAddress string
}

func (a *app) startTunnel(ctx context.Context, tn string, tunnelConfig *tunnelCfg) error {
	if tunnelConfig.Tunnel.AdminState == adminDisable {
		tunnelConfig.Tunnel.OperState = operDown
		tunnelConfig.Tunnel.OperStateDownReason.Value = "admin down"
		a.updateTunnelTelemetry(tn, tunnelConfig)
		return nil
	}
	destinations := make(map[string]*destination)
	for dName := range tunnelConfig.Tunnel.Destination {
		if dest, ok := a.config.app.Destination[dName]; ok {
			destinations[dName] = dest
			continue
		}
	}
	log.Infof("tunnel=%s destinations=%+v", tn, destinations)
	if len(destinations) == 0 {
		tunnelConfig.Tunnel.OperState = operDown
		tunnelConfig.Tunnel.OperStateDownReason.Value = "no destinations found"
		a.updateTunnelTelemetry(tn, tunnelConfig)
		return fmt.Errorf("%v", tunnelConfig.Tunnel.OperStateDownReason.Value)
	}

	for dn, dest := range destinations {
		destState, ok := tunnelConfig.Tunnel.Destination[dn]
		if !ok {
			log.Errorf("destination %s not found under tunnel.destination", dn)
			continue
		}
		// check if tunnelDestination is already running
		if destState.OperState == operUp {
			log.Infof("tunnel %s, destination %s is already oper UP", tn, dn)
			continue
		}
		destState.OperState = operStarting
		a.updateTunnelDestinationTelemetry(tn, dn, destState)
		// TODO: add entry to tunnelTargets map
		if a.tunnelClients[tn] == nil {
			a.tunnelClients[tn] = make(map[string]*tunnelDestinationClient)
		}
		a.tunnelClients[tn][dn] = new(tunnelDestinationClient)
		//
		go a.startTunnelDestination(ctx, tn, dn, tunnelConfig, dest, destState)
	}
	return nil
}

//
func (a *app) stopTunnel(ctx context.Context, tn string) {
	tuns, ok := a.tunnelClients[tn]
	if !ok {
		return
	}
	for dn := range tuns {
		a.stopTunnelDestination(ctx, tn, dn)
		if _, ok := a.config.app.Tunnel[tn]; ok {
			if _, ok = a.config.app.Tunnel[tn].Tunnel.Destination[dn]; ok {
				a.config.app.Tunnel[tn].Tunnel.Destination[dn].OperState = operDown
				a.config.app.Tunnel[tn].Tunnel.Destination[dn].OperStateDownReason.Value = "tunnel stopped"
				a.updateTunnelDestinationTelemetry(tn, dn, a.config.app.Tunnel[tn].Tunnel.Destination[dn])
			}
		}
	}
}

func (a *app) tunnelHandlerFunc(tn, dn string) func(t tunnel.Target, i io.ReadWriteCloser) error {
	return func(t tunnel.Target, i io.ReadWriteCloser) error {
		var dialAddr string
		var targets map[string]*tunnelTargetDetails
		// a.m.RLock()
		if tun, ok := a.tunnelClients[tn]; ok {
			if dest, ok := tun[dn]; ok && dest != nil {
				targets = dest.targets
			}
		}
		// a.m.RUnlock()
		if len(targets) == 0 {
			return fmt.Errorf("tunnel=%s, destination=%s: no matching target found %+v", tn, dn, t)
		}
		log.Debugf("tunnel=%s, destination=%s, targets=%+v", tn, dn, targets)
		for _, target := range targets {
			if t.ID == target.ID && t.Type == target.Type {
				dialAddr = target.dialAddress
				break
			}
		}
		if len(dialAddr) == 0 {
			return fmt.Errorf("not matching dial address found for target: %+v", t)
		}

		network := "tcp"
		// change network to "unix" if the dial address starts with "unix://"
		if strings.HasPrefix(dialAddr, "unix://") {
			network = "unix"
			dialAddr = strings.TrimPrefix(dialAddr, "unix://")
		}
		log.Infof("dialing network=%s, address=%s for target %+v", network, dialAddr, t)
		conn, err := net.Dial(network, dialAddr)
		if err != nil {
			return fmt.Errorf("failed to dial %s: %v", dialAddr, err)
		}
		// start bidirectional copy
		if err = bidi.Copy(i, conn); err != nil {
			return fmt.Errorf("bidi copy error: %v", err)
		}
		return nil
	}
}

//
func (a *app) stopAll(ctx context.Context) {
	for tn := range a.tunnelClients {
		a.stopTunnel(ctx, tn)
	}
	a.tunnelClients = make(map[string]map[string]*tunnelDestinationClient)
}

func (a *app) startAll(ctx context.Context) {
	for tn, tun := range a.config.app.Tunnel {
		err := a.startTunnel(ctx, tn, tun)
		if err != nil {
			log.Errorf("failed to start tunnel %s: %v", tn, err)
			tun.Tunnel.OperState = operDown
			tun.Tunnel.OperStateDownReason = stringValue{
				Value: fmt.Sprintf("tunnel %s failed: %v", tn, err),
			}
			continue
		}
		tun.Tunnel.OperState = operUp
		tun.Tunnel.OperStateDownReason.Value = ""
		a.updateTunnelTelemetry(tn, tun)
	}
}

//
func (a *app) startTunnelDestination(ctx context.Context, tn, dn string, tunnelConfig *tunnelCfg, dest *destination, destState *destinationState) {
	netIns := dest.Destination.NetworkInstance.Value
	if netIns == "" {
		netIns = "mgmt"
	}
	netInsName := fmt.Sprintf("srbase-%s", netIns)
	n, err := netns.GetFromName(netInsName)
	if err != nil {
		log.Errorf("failed getting NS %q: %v", netInsName, err)
		return
	}
	log.Debugf("tunnel %s, destination %s got namespace: %+v for %s", tn, dn, n, netInsName)

	opts := []grpc.DialOption{
		// grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(_ context.Context, addr string) (net.Conn, error) {
			runtime.LockOSThread()
			err = netns.Set(n)
			if err != nil {
				log.Infof("failed setting NS to %s: %v", n, err)
				return nil, err
			}
			return net.Dial("tcp", addr)
		}),
	}
	if dest.Destination.NoTLS.Value {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true,
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	}
	tunnelServerAddr := fmt.Sprintf("%s:%s", dest.Destination.Address.Value, dest.Destination.Port.Value)
	log.Infof("tunnel %s dialing destination address: %s", tn, tunnelServerAddr)

	defer runtime.UnlockOSThread()
	var conn *grpc.ClientConn
	for {
		select {
		case <-ctx.Done():
			return
		default:
			gnmiCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			conn, err = grpc.DialContext(gnmiCtx, tunnelServerAddr, opts...)
			if err != nil {
				log.Errorf("tunnel %s failed to connect to destination %s, addr=%s: %v",
					tn, dn, tunnelServerAddr, err)
				destState.OperState = operDown
				destState.OperStateDownReason.Value = fmt.Sprintf("failed dial addr=%s: %v", tunnelServerAddr, err)
				a.updateTunnelDestinationTelemetry(tn, dn, destState)
				time.Sleep(5 * time.Second)
				continue
			}
		}
		break
	}
	defer conn.Close()
	// update tunnel destination telemetry
	destState.OperState = operUp
	destState.OperStateDownReason.Value = ""
	a.updateTunnelDestinationTelemetry(tn, dn, destState)
	//
	log.Infof("connection to destination %s, addr=%s successful", dn, tunnelServerAddr)
	// create tunnel client
	client, err := tunnel.NewClient(tpb.NewTunnelClient(conn), tunnel.ClientConfig{
		RegisterHandler: func(t tunnel.Target) error { return nil },
		Handler:         a.tunnelHandlerFunc(tn, dn),
	}, nil)
	if err != nil {
		log.Errorf("tunnel %s failed to create tunnel client: %v", tn, err)
		return
	}
	log.Infof("tunnel client to destination %s, addr=%s successful", dn, tunnelServerAddr)
	// Register and start listening.
	err = client.Register(ctx)
	if err != nil {
		log.Errorf("tunnel %s failed to register: %v", tn, err)
		return
	}
	log.Infof("tunnel client to destination %s, addr=%s registered", dn, tunnelServerAddr)
	if destState.Target == nil {
		destState.Target = make(map[string]*targetState)
	}
	a.m.Lock()
	if _, ok := a.tunnelClients[tn]; !ok {
		a.tunnelClients[tn] = make(map[string]*tunnelDestinationClient)
	}
	a.tunnelClients[tn][dn] = &tunnelDestinationClient{
		conn:    conn,
		client:  client,
		targets: make(map[string]*tunnelTargetDetails),
	}
	a.m.Unlock()
	if len(tunnelConfig.Tunnel.Target) > 0 {
		// create targets
		log.Infof("tunnel %s, destination %s: registering clients", tn, dn)
		for hn, han := range tunnelConfig.Tunnel.Target {
			log.Infof("tunnel %s, destination %s: registering client from handler %s", tn, dn, hn)
			a.startTunnelHandlerDestination(ctx, tn, hn, han, dn, destState, client)
		}
	}
	// blocking call
	client.Start(ctx)
	//

	err = client.Error()
	log.Errorf("tunnel %s destination %s stopped: %v", tn, dn, err)

	a.config.m.Lock()
	tnCfg, ok := a.config.app.Tunnel[tn]
	a.config.m.Unlock()
	if ok {
		if tnCfg.Tunnel.AdminState == adminDisable || a.config.app.AdminState == adminDisable {
			tunnelConfig.Tunnel.OperState = operDown
			tunnelConfig.Tunnel.OperStateDownReason.Value = "admin down"
			a.updateTunnelTelemetry(tn, tnCfg)
			return
		}
	} else {
		return
	}
	tunnelConfig.Tunnel.OperState = operFailed
	tunnelConfig.Tunnel.OperStateDownReason.Value = err.Error()
	a.updateTunnelTelemetry(tn, tunnelConfig)
}

func (a *app) stopTunnelDestination(ctx context.Context, tn, dn string) {
	if _, ok := a.tunnelClients[tn]; ok {
		if tdc, ok := a.tunnelClients[tn][dn]; ok {
			for _, ttd := range tdc.targets {
				tt := tunnel.Target{ID: ttd.ID, Type: ttd.Type}
				log.Infof("tunnel=%s, destination=%s: deleting target %+v", tn, dn, tt)
				err := tdc.client.DeleteTarget(tt)
				if err != nil {
					log.Errorf("failed to delete target %+v: %v", tt, err)
				}
				log.Infof("tunnel=%s, destination=%s: deleting target telemetry %+v", tn, dn, tt)
				a.deleteTunnelDestinationTargetTelemetry(tn, dn, tt.ID, tt.Type)
			}
			tdc.conn.Close()
		}
	}
}

// start a handler under a tunnel to a specific destination
func (a *app) startTunnelHandlerDestination(ctx context.Context,
	tn string,
	tg string, targetCfg *target,
	dn string, destState *destinationState,
	tunnelClient *tunnel.Client,
) {
	ttd, err := a.newTargetDetails(targetCfg)
	if err != nil {
		log.Errorf("failed to create a targetDetails, tunnel=%s, handler=%s: %v", tn, tg, err)
		return
	}

	a.m.Lock()
	ttc, ok := a.tunnelClients[tn][dn]
	if !ok {
		a.m.Unlock()
		log.Errorf("client not found for tunnel=%s, destination=%s, handler=%s", tn, dn, tg)
		return
	}
	a.m.Unlock()
	ttc.targets[tg] = &ttd
	ts := new(targetState)
	targetName := fmt.Sprintf("%s:::%s", ttd.ID, ttd.Type)
	destState.Target[targetName] = ts

	ts.Target.OperState = operStarting
	a.updateTunnelDestinationTargetTelemetry(tn, dn, ttd.ID, ttd.Type, ts)
	// register target
	log.Infof("tunnel=%s, destination=%s, handler=%s: registering target %+v", tn, dn, tg, ttd)
	err = tunnelClient.NewTarget(tunnel.Target{ID: ttd.ID, Type: ttd.Type})
	if err != nil {
		log.Errorf("tunnel %s failed to register target %v in destination %s: %v", tn, ttd, dn, err)
		ts.Target.OperState = operDown
		ts.Target.OperStateDownReason.Value = err.Error()
		a.updateTunnelDestinationTargetTelemetry(tn, dn, ttd.ID, ttd.Type, ts)
		return
	}
	log.Infof("tunnel=%s, destination=%s, handler=%s: registered target %+v", tn, dn, tg, ttd)
	ts.Target.OperState = operUp
	ts.Target.OperStateDownReason.Value = ""
	a.updateTunnelDestinationTargetTelemetry(tn, dn, ttd.ID, ttd.Type, ts)
}

// de registers a target (of handler hn) from the server
func (a *app) stopTunnelHandlerDestination(ctx context.Context,
	tn, tg, dn string, dest *destinationState,
	ttd *tunnelDestinationClient) {
	if tt, ok := ttd.targets[tg]; ok {
		err := ttd.client.DeleteTarget(tunnel.Target{ID: tt.ID, Type: tt.Type})
		if err != nil {
			log.Errorf("tunnel=%s, destination=%s, handler=%s: failed deleting target: %v", tn, dn, tg, tt)
		}
		a.deleteTunnelDestinationTargetTelemetry(tn, dn, tt.ID, tt.Type)
		delete(ttd.targets, tg)
	}
}

func (a *app) newTargetDetails(tg *target) (tunnelTargetDetails, error) {
	var ttd tunnelTargetDetails
	// ID
	switch {
	case tg.Target.ID.MacAddress != nil && tg.Target.ID.MacAddress.Value:
		ttd.ID = a.config.sysInfo.ChassisMacAddress
	case tg.Target.ID.UserAgent != nil && tg.Target.ID.UserAgent.Value:
		ttd.ID = fmt.Sprintf("%s:nokia-srl:%s:%s",
			a.config.sysInfo.Name,
			a.config.sysInfo.ChassisType,
			a.config.sysInfo.Version,
		)
	case tg.Target.ID.NodeName != nil && tg.Target.ID.NodeName.Value:
		ttd.ID = a.config.sysInfo.Name
	case tg.Target.ID.Custom != nil && tg.Target.ID.Custom.Value != "":
		tpl, err := template.New("customID").Parse(tg.Target.ID.Custom.Value)
		if err != nil {
			return ttd, fmt.Errorf("failed to parse template: %w", err)
		}
		b := new(bytes.Buffer)
		err = tpl.Execute(b, a.config.sysInfo)
		if err != nil {
			return ttd, fmt.Errorf("failed to execute template: %w", err)
		}
		ttd.ID = b.String()
	}
	// Type and DialAddress
	switch {
	case tg.Target.Type.GrpcServer != nil && tg.Target.Type.GrpcServer.Value:
		ttd.Type = "GNMI_GNOI"
		ttd.dialAddress = gnmiServerUnixSocket
		if tg.Target.LocalAddress.Value != "" {
			ttd.dialAddress = tg.Target.LocalAddress.Value
		}
	case tg.Target.Type.SSHServer != nil && tg.Target.Type.SSHServer.Value:
		ttd.Type = "SSH"
		ttd.dialAddress = "localhost:22"
		if tg.Target.LocalAddress.Value != "" {
			ttd.dialAddress = tg.Target.LocalAddress.Value
		}
	case tg.Target.Type.Custom != nil && tg.Target.Type.Custom.Value != "":
		tpl, err := template.New("customType").Parse(tg.Target.Type.Custom.Value)
		if err != nil {
			return ttd, fmt.Errorf("failed to parse template: %w", err)
		}
		b := new(bytes.Buffer)
		err = tpl.Execute(b, a.config.sysInfo)
		if err != nil {
			return ttd, fmt.Errorf("failed to execute template: %w", err)
		}
		ttd.Type = b.String()
		ttd.dialAddress = tg.Target.LocalAddress.Value
	}
	return ttd, nil
}
