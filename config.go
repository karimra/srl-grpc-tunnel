package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nokia/srlinux-ndk-go/ndk"
	log "github.com/sirupsen/logrus"
)

const (
	operUp       = "OPER_STATE_up"
	operDown     = "OPER_STATE_down"
	operStarting = "OPER_STATE_starting"
	operFailed   = "OPER_STATE_failed"
	//
	adminEnable  = "ADMIN_STATE_enable"
	adminDisable = "ADMIN_STATE_disable"

	// app configuration paths
	grpcTunnelPath        = ".system.grpc_tunnel"
	destinationPath       = ".system.grpc_tunnel.destination"
	tunnelPath            = ".system.grpc_tunnel.tunnel"
	tunnelDestinationPath = ".system.grpc_tunnel.tunnel.destination"
	tunnelTargetPath      = ".system.grpc_tunnel.tunnel.target"
)

type config struct {
	m   *sync.Mutex
	trx []*ndk.ConfigNotification
	//
	app *appConfig
	//
	sysInfo  systemInfo
	username string
	password string
}

type appConfig struct {
	AdminState string `json:"admin_state,omitempty"`
	OperState  string `json:"oper_state,omitempty"`
	//
	Destination map[string]*destination `json:"-"`
	Tunnel      map[string]*tunnelCfg   `json:"-"`
}

type destination struct {
	Destination struct {
		Address         stringValue `json:"address,omitempty"`
		Port            stringValue `json:"port,omitempty"`
		Description     stringValue `json:"description,omitempty"`
		NoTLS           boolValue   `json:"no_tls,omitempty"`
		TLSProfile      stringValue `json:"tls_profile,omitempty"`
		NetworkInstance stringValue `json:"network_instance,omitempty"`
	} `json:"destination,omitempty"`
}

type tunnelCfg struct {
	Tunnel struct {
		AdminState          string      `json:"admin_state,omitempty"`
		OperState           string      `json:"oper_state,omitempty"`
		OperStateDownReason stringValue `json:"oper_state_down_reason,omitempty"`
		Description         stringValue `json:"description,omitempty"`

		Target      map[string]*target           `json:"-"`
		Destination map[string]*destinationState `json:"-"`
	} `json:"tunnel,omitempty"`
}

type destinationState struct {
	OperState           string      `json:"oper_state,omitempty"`
	OperStateDownReason stringValue `json:"oper_state_down_reason,omitempty"`

	Target map[string]*targetState `json:"-"`
}

type targetState struct {
	Target struct {
		OperState           string      `json:"oper_state,omitempty"`
		OperStateDownReason stringValue `json:"oper_state_down_reason,omitempty"`
	} `json:"target,omitempty"`
}

type target struct {
	Target struct {
		LocalAddress stringValue `json:"local_address,omitempty"`
		ID           struct {
			NodeName   *boolValue   `json:"node_name,omitempty"`
			UserAgent  *boolValue   `json:"user_agent,omitempty"`
			MacAddress *boolValue   `json:"mac_address,omitempty"`
			Custom     *stringValue `json:"custom,omitempty"`
		} `json:"id,omitempty"`
		Type struct {
			GrpcServer *boolValue   `json:"grpc_server,omitempty"`
			SSHServer  *boolValue   `json:"ssh_server,omitempty"`
			Custom     *stringValue `json:"custom,omitempty"`
		} `json:"type,omitempty"`
	} `json:"target,omitempty"`
}

type stringValue struct {
	Value string `json:"value,omitempty"`
}

// type uint64Value struct {
// 	Value uint64 `json:"value,omitempty"`
// }

type boolValue struct {
	Value bool `json:"value,omitempty"`
}

func newConfig() *config {
	return &config{
		m:   new(sync.Mutex),
		trx: make([]*ndk.ConfigNotification, 0),
		app: &appConfig{
			Destination: make(map[string]*destination),
			Tunnel:      make(map[string]*tunnelCfg),
		},
	}
}

func (a *app) handleConfigEvent(ctx context.Context, cfg *ndk.ConfigNotification) {
	jsPath := cfg.GetKey().GetJsPath()
	// collect non commit.end config notifications
	if jsPath != ".commit.end" && cfg != nil {
		a.config.trx = append(a.config.trx, cfg)
		return
	}
	a.config.m.Lock()
	defer a.config.m.Unlock()
	// when path is ".commit.end", handle the stored config notifications
	for _, txCfg := range a.config.trx {
		switch txCfg.GetKey().GetJsPath() {
		case grpcTunnelPath:
			a.handleGrpcTunnel(ctx, txCfg)
		case destinationPath:
			a.handleDestination(ctx, txCfg)
		case tunnelPath:
			a.handleTunnel(ctx, txCfg)
		case tunnelTargetPath:
			a.handleTunnelTarget(ctx, txCfg)
		case tunnelDestinationPath:
			a.handleTunnelDestination(ctx, txCfg)
		default:
			log.Errorf("received unexpected config path %q", txCfg.GetKey().GetJsPath())
		}
	}
	// reset transaction array
	a.config.trx = make([]*ndk.ConfigNotification, 0)
}

func (a *app) handleNwInstCfg(ctx context.Context, cfg *ndk.NetworkInstanceNotification) {
	//log.Debugf("received network instance notification: %+v", cfg)
}

// ".system.grpc_tunnel" handlers
func (a *app) handleGrpcTunnel(ctx context.Context, txCfg *ndk.ConfigNotification) {
	switch txCfg.GetOp() {
	case ndk.SdkMgrOperation_Create:
		log.Infof("Create: .system.grpc_tunnel: %+v", txCfg)
		a.handleGrpcTunnelCreate(ctx, txCfg.GetData())
	case ndk.SdkMgrOperation_Update:
		log.Infof("Update: .system.grpc_tunnel: %+v", txCfg)
		a.handleGrpcTunnelChange(ctx, txCfg.GetData())
	case ndk.SdkMgrOperation_Delete:
		log.Infof("Delete: .system.grpc_tunnel: %+v", txCfg)
		a.handleGrpcTunnelDelete(ctx)
	}
}

func (a *app) handleGrpcTunnelCreate(ctx context.Context, cfgData *ndk.ConfigData) {
	newAppCfg := new(appConfig)
	err := json.Unmarshal([]byte(cfgData.GetJson()), newAppCfg)
	if err != nil {
		log.Errorf("failed to unmarshal path %q config %+v", grpcTunnelPath, cfgData)
		return
	}
	a.config.app = newAppCfg
	a.config.app.OperState = operDown
	a.updateRootLevelTelemetry(a.config.app)
}

func (a *app) handleGrpcTunnelChange(ctx context.Context, cfgData *ndk.ConfigData) {
	// unmarshal changed config
	newAppCfg := new(appConfig)
	err := json.Unmarshal([]byte(cfgData.GetJson()), newAppCfg)
	if err != nil {
		log.Errorf("failed to unmarshal path %q config %+v", grpcTunnelPath, cfgData)
		return
	}
	a.config.app.AdminState = newAppCfg.AdminState
	// apply state change
	switch {
	case a.config.app.AdminState == adminDisable && a.config.app.OperState == operUp:
		// stop all tunnels
		a.stopAll(ctx)
		a.config.app.OperState = operDown
	case a.config.app.AdminState == adminEnable && a.config.app.OperState != operUp:
		// start all tunnels
		a.startAll(ctx)
		a.config.app.OperState = operUp
	}
	// update telemetry
	a.updateRootLevelTelemetry(a.config.app)
}

func (a *app) handleGrpcTunnelDelete(ctx context.Context) {
	a.stopAll(ctx)
	a.config.app = &appConfig{
		AdminState:  adminDisable,
		OperState:   operDown,
		Destination: make(map[string]*destination),
		Tunnel:      make(map[string]*tunnelCfg),
	}
	a.updateRootLevelTelemetry(a.config.app)
	a.m.Lock()
	a.tunnelClients = make(map[string]map[string]*tunnelDestinationClient)
	a.m.Unlock()
}

// ".system.grpc_tunnel.destination" handlers
func (a *app) handleDestination(ctx context.Context, txCfg *ndk.ConfigNotification) {
	keys := txCfg.GetKey().GetKeys()
	if len(keys) != 1 {
		log.Errorf("unexpected number of keys in path %q: %v: %+v", destinationPath, keys, txCfg)
		return
	}
	dName := keys[0]
	switch txCfg.GetOp() {
	case ndk.SdkMgrOperation_Create:
		a.handleDestinationCreate(ctx, dName, txCfg.GetData())
	case ndk.SdkMgrOperation_Update:
		a.handleDestinationChange(ctx, dName, txCfg.GetData())
	case ndk.SdkMgrOperation_Delete:
		a.handleDestinationDelete(ctx, dName)
	}
}

func (a *app) handleDestinationCreate(ctx context.Context, dName string, cfgData *ndk.ConfigData) {
	newDG := new(destination)
	err := json.Unmarshal([]byte(cfgData.GetJson()), newDG)
	if err != nil {
		log.Errorf("failed to unmarshal path %q config %+v", destinationPath, cfgData)
		return
	}
	if a.config.app.Destination == nil {
		a.config.app.Destination = make(map[string]*destination)
	}
	a.config.app.Destination[dName] = newDG
	a.updateDestinationTelemetry(dName, newDG)
}

func (a *app) handleDestinationChange(ctx context.Context, dName string, cfgData *ndk.ConfigData) {
	newDest := new(destination)
	err := json.Unmarshal([]byte(cfgData.GetJson()), newDest)
	if err != nil {
		log.Errorf("failed to unmarshal path %q config %+v", destinationPath, cfgData)
		return
	}
	if a.config.app.Destination == nil {
		a.config.app.Destination = make(map[string]*destination)
	}
	a.config.app.Destination[dName] = newDest
	a.updateDestinationTelemetry(dName, newDest)
}

func (a *app) handleDestinationDelete(ctx context.Context, dName string) {
	delete(a.config.app.Destination, dName)
	a.deleteDestinationTelemetry(ctx, dName)
}

// ".system.grpc_tunnel.tunnel" handlers
func (a *app) handleTunnel(ctx context.Context, txCfg *ndk.ConfigNotification) {
	keys := txCfg.GetKey().GetKeys()
	if len(keys) != 1 {
		log.Errorf("unexpected number of keys in path %q: %v: %+v", destinationPath, keys, txCfg)
		return
	}
	tn := keys[0]
	switch txCfg.GetOp() {
	case ndk.SdkMgrOperation_Create:
		a.handleTunnelCreate(ctx, tn, txCfg.GetData())
	case ndk.SdkMgrOperation_Update:
		a.handleTunnelChange(ctx, tn, txCfg.GetData())
	case ndk.SdkMgrOperation_Delete:
		a.handleTunnelDelete(ctx, tn)
	}
}

func (a *app) handleTunnelCreate(ctx context.Context, tn string, cfgData *ndk.ConfigData) {
	newTunnel := new(tunnelCfg)
	err := json.Unmarshal([]byte(cfgData.GetJson()), newTunnel)
	if err != nil {
		log.Errorf("failed to unmarshal path %q config %+v", tunnelPath, cfgData)
		return
	}
	if a.config.app.Tunnel == nil {
		a.config.app.Tunnel = make(map[string]*tunnelCfg)
	}
	newTunnel.Tunnel.OperState = operUp
	if newTunnel.Tunnel.AdminState == adminDisable {
		newTunnel.Tunnel.OperState = operDown
	}
	a.config.app.Tunnel[tn] = newTunnel
	a.updateTunnelTelemetry(tn, newTunnel)
}

func (a *app) handleTunnelChange(ctx context.Context, tn string, cfgData *ndk.ConfigData) {
	newTunnel := new(tunnelCfg)
	err := json.Unmarshal([]byte(cfgData.GetJson()), newTunnel)
	if err != nil {
		log.Errorf("failed to unmarshal path %q config %+v", tunnelPath, cfgData)
		return
	}
	if a.config.app.Tunnel == nil {
		a.config.app.Tunnel = make(map[string]*tunnelCfg)
	}
	if tun, ok := a.config.app.Tunnel[tn]; ok {
		newTunnel.Tunnel.Target = tun.Tunnel.Target
		newTunnel.Tunnel.Destination = tun.Tunnel.Destination
	}
	log.Infof("tunnel %s, new admin-state=%s, oper-state=%s", tn, newTunnel.Tunnel.AdminState, a.config.app.Tunnel[tn].Tunnel.OperState)
	switch {
	case newTunnel.Tunnel.AdminState == adminEnable && a.config.app.Tunnel[tn].Tunnel.OperState != operUp:
		err := a.startTunnel(ctx, tn, newTunnel)
		if err != nil {
			log.Errorf("failed to start tunnel %s: %v", tn, err)
		}
		newTunnel.Tunnel.OperState = operUp
		newTunnel.Tunnel.OperStateDownReason.Value = ""
	case newTunnel.Tunnel.AdminState == adminDisable && a.config.app.Tunnel[tn].Tunnel.OperState != operDown:
		a.stopTunnel(ctx, tn)
		newTunnel.Tunnel.OperState = operDown
		newTunnel.Tunnel.OperStateDownReason = stringValue{
			Value: "admin down",
		}
	}
	a.config.app.Tunnel[tn] = newTunnel
	a.updateTunnelTelemetry(tn, newTunnel)
}

func (a *app) handleTunnelDelete(ctx context.Context, tn string) {
	// stop all destinations of tunnel
	a.stopTunnel(ctx, tn)
	delete(a.config.app.Tunnel, tn)
	a.deleteTunnelTelemetry(ctx, tn)
}

// ".system.grpc_tunnel.tunnel.destination" handlers
func (a *app) handleTunnelDestination(ctx context.Context, txCfg *ndk.ConfigNotification) {
	keys := txCfg.GetKey().GetKeys()
	if len(keys) != 2 {
		log.Errorf("unexpected number of keys in path %q: %v: %+v", tunnelDestinationPath, keys, txCfg)
		return
	}
	tn := keys[0]
	dn := keys[1]
	switch txCfg.GetOp() {
	case ndk.SdkMgrOperation_Create:
		a.handleTunnelDestinationCreate(ctx, tn, dn, txCfg.GetData())
	case ndk.SdkMgrOperation_Update:
		a.handleTunnelDestinationChange(ctx, tn, dn, txCfg.GetData())
	case ndk.SdkMgrOperation_Delete:
		a.handleTunnelDestinationDelete(ctx, tn, dn)
	}
}

func (a *app) handleTunnelDestinationCreate(ctx context.Context, tn, dn string, cfgData *ndk.ConfigData) {
	newDstState := new(destinationState)
	err := json.Unmarshal([]byte(cfgData.GetJson()), newDstState)
	if err != nil {
		log.Errorf("failed to unmarshal path %q config %+v", tunnelDestinationPath, cfgData)
		return
	}
	if _, ok := a.config.app.Tunnel[tn]; !ok {
		a.config.app.Tunnel[tn] = new(tunnelCfg)
	}
	if a.config.app.Tunnel[tn].Tunnel.Destination == nil {
		a.config.app.Tunnel[tn].Tunnel.Destination = make(map[string]*destinationState)
	}
	tun := a.config.app.Tunnel[tn]
	tun.Tunnel.Destination[dn] = newDstState
	dest := a.config.app.Destination[dn]
	a.updateTunnelDestinationTelemetry(tn, dn, newDstState)
	if tun.Tunnel.AdminState == adminEnable {
		go a.startTunnelDestination(ctx, tn, dn, tun, dest, newDstState)
	}
}

// won't happen until there is config under .grpc_tunnel.tunnel.destination
func (a *app) handleTunnelDestinationChange(ctx context.Context, tn, dn string, cfgData *ndk.ConfigData) {
}

func (a *app) handleTunnelDestinationDelete(ctx context.Context, tn, dn string) {
	a.stopTunnelDestination(ctx, tn, dn)
	delete(a.config.app.Tunnel[tn].Tunnel.Destination, dn)
	a.deleteTunnelDestinationTelemetry(tn, dn)
}

// ".system.grpc_tunnel.tunnel.target" handlers
func (a *app) handleTunnelTarget(ctx context.Context, txCfg *ndk.ConfigNotification) {
	fmt.Printf("handler config: %v\n", txCfg.GetOp())
	keys := txCfg.GetKey().GetKeys()
	if len(keys) != 2 {
		log.Errorf("unexpected number of keys in path %q: %v: %+v", destinationPath, keys, txCfg)
		return
	}
	tn := keys[0]
	tg := keys[1]
	switch txCfg.GetOp() {
	case ndk.SdkMgrOperation_Create:
		a.handleTunnelTargetCreate(ctx, tn, tg, txCfg.GetData())
	case ndk.SdkMgrOperation_Update:
		a.handleTunnelTargetChange(ctx, tn, tg, txCfg.GetData())
	case ndk.SdkMgrOperation_Delete:
		a.handleTunnelTargetDelete(ctx, tn, tg)
	}
}

func (a *app) handleTunnelTargetCreate(ctx context.Context, tn, tg string, cfgData *ndk.ConfigData) {
	newTarget := new(target)
	err := json.Unmarshal([]byte(cfgData.GetJson()), newTarget)
	if err != nil {
		log.Errorf("failed to unmarshal path %q config %+v", tunnelTargetPath, cfgData)
		return
	}
	if _, ok := a.config.app.Tunnel[tn]; !ok {
		a.config.app.Tunnel[tn] = new(tunnelCfg)
	}
	if a.config.app.Tunnel[tn].Tunnel.Target == nil {
		a.config.app.Tunnel[tn].Tunnel.Target = make(map[string]*target)
	}
	if _, ok := a.config.app.Tunnel[tn]; ok {
		a.config.app.Tunnel[tn].Tunnel.Target[tg] = newTarget
		for dn, dest := range a.config.app.Tunnel[tn].Tunnel.Destination {
			if ttd, ok := a.tunnelClients[tn][dn]; ok {
				a.startTunnelHandlerDestination(ctx, tn, tg, newTarget, dn, dest, ttd.client)
			}
		}
	}

	a.updateTunnelTargetTelemetry(tn, tg, newTarget)
}

func (a *app) handleTunnelTargetChange(ctx context.Context, tn, tg string, cfgData *ndk.ConfigData) {
	newTarget := new(target)
	err := json.Unmarshal([]byte(cfgData.GetJson()), newTarget)
	if err != nil {
		log.Errorf("failed to unmarshal path %q config %+v", tunnelTargetPath, cfgData)
		return
	}
	if a.config.app.Tunnel[tn].Tunnel.Target == nil {
		a.config.app.Tunnel[tn].Tunnel.Target = make(map[string]*target)
	}
	if tun, ok := a.config.app.Tunnel[tn]; ok {
		tun.Tunnel.Target[tg] = newTarget
		for dn, dest := range tun.Tunnel.Destination {
			if tdc, ok := a.tunnelClients[tn][dn]; ok {
				// stop target
				a.stopTunnelHandlerDestination(ctx, tn, tg, dn, dest, tdc)
				delete(tdc.targets, tg)
				// start again
				a.startTunnelHandlerDestination(ctx, tn, tg, newTarget, dn, dest, tdc.client)
			}
		}
	}
	a.updateTunnelTargetTelemetry(tn, tg, newTarget)
}

func (a *app) handleTunnelTargetDelete(ctx context.Context, tn, tg string) {
	if tun, ok := a.config.app.Tunnel[tn]; ok {
		for dn, dest := range tun.Tunnel.Destination {
			if tdc, ok := a.tunnelClients[tn][dn]; ok {
				a.stopTunnelHandlerDestination(ctx, tn, tg, dn, dest, tdc)
				delete(tdc.targets, tg)
			}
		}
	}

	delete(a.config.app.Tunnel[tn].Tunnel.Target, tg)
	a.deleteTunnelTargetTelemetry(tn, tg)
}
