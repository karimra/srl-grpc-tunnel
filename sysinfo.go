package main

import (
	"context"
	"strings"
	"time"

	"github.com/karimra/gnmic/utils"
	"github.com/openconfig/gnmi/proto/gnmi"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

var sysInfoPaths = []*gnmi.Path{
	{
		Elem: []*gnmi.PathElem{
			{Name: "system"},
			{Name: "name"},
			{Name: "host-name"},
		},
	},
	{
		Elem: []*gnmi.PathElem{
			{Name: "interface",
				Key: map[string]string{"name": "mgmt0"},
			},
			{Name: "subinterface"},
			{Name: "ipv4"},
			{Name: "address"},
			{Name: "status"},
		},
	},
	{
		Elem: []*gnmi.PathElem{
			{Name: "interface",
				Key: map[string]string{"name": "mgmt0"},
			},
			{Name: "subinterface"},
			{Name: "ipv6"},
			{Name: "address"},
			{Name: "status"},
		},
	},
	{
		Elem: []*gnmi.PathElem{
			{Name: "system"},
			{Name: "information"},
			{Name: "version"},
		},
	},
	{
		Elem: []*gnmi.PathElem{
			{Name: "platform"},
			{Name: "chassis"},
		},
	},
}

type systemInfo struct {
	Name                string
	Version             string
	ChassisType         string
	ChassisMacAddress   string
	ChassisCLEICode     string
	ChassisPartNumber   string
	ChassisSerialNumber string
	//NetworkInstance     string
	IPAddrV4 string
	IPAddrV6 string
}

func createGNMIClient(ctx context.Context) (gnmi.GNMIClient, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, retryInterval)
	defer cancel()
	conn, err := grpc.DialContext(timeoutCtx, gnmiServerUnixSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock())
	if err != nil {
		return nil, err
	}
	return gnmi.NewGNMIClient(conn), nil
}

func (a *app) getSystemInfo(ctx context.Context) (*systemInfo, error) {
	ctx = metadata.AppendToOutgoingContext(ctx, "username", a.config.username, "password", a.config.password)
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
START:
	select {
	case <-sctx.Done():
		return nil, ctx.Err()
	default:
		gnmiClient, err := createGNMIClient(sctx)
		if err != nil {
			log.Infof("failed to create a gnmi connection to %q: %v", gnmiServerUnixSocket, err)
			time.Sleep(retryInterval)
			goto START
		}

		rsp, err := gnmiClient.Get(sctx,
			&gnmi.GetRequest{
				Path:     sysInfoPaths,
				Type:     gnmi.GetRequest_STATE,
				Encoding: gnmi.Encoding_ASCII,
			})
		if err != nil {
			log.Errorf("failed Get response: %v", err)
			time.Sleep(retryInterval)
			goto START
		}
		sysInfo := new(systemInfo)

		for _, n := range rsp.GetNotification() {
			for _, u := range n.GetUpdate() {
				path := utils.GnmiPathToXPath(u.GetPath(), true)
				if strings.HasPrefix(path, "interface") {
					if strings.Contains(path, "/ipv4/address/status") {
						ip := getPathKeyVal(u.GetPath(), "address", "ip-prefix")
						sysInfo.IPAddrV4 = strings.Split(ip, "/")[0]
					}
					if strings.Contains(path, "/ipv6/address/status") {
						ip := getPathKeyVal(u.GetPath(), "address", "ip-prefix")
						sysInfo.IPAddrV6 = strings.Split(ip, "/")[0]
					}
				}
				if strings.Contains(path, "system/name") {
					sysInfo.Name = u.GetVal().GetStringVal()
				}
				if strings.Contains(path, "system/information/version") {
					sysInfo.Version = u.GetVal().GetStringVal()
				}
				if strings.Contains(path, "platform/chassis/type") {
					sysInfo.ChassisType = u.GetVal().GetStringVal()
				}
				if strings.Contains(path, "platform/chassis/hw-mac-address") {
					sysInfo.ChassisMacAddress = u.GetVal().GetStringVal()
				}
				if strings.Contains(path, "platform/chassis/part-number") {
					sysInfo.ChassisPartNumber = u.GetVal().GetStringVal()
				}
				if strings.Contains(path, "platform/chassis/clei-code") {
					sysInfo.ChassisCLEICode = u.GetVal().GetStringVal()
				}
				if strings.Contains(path, "platform/chassis/serial-number") {
					sysInfo.ChassisSerialNumber = u.GetVal().GetStringVal()
				}
			}
		}
		log.Debugf("system info: %+v", sysInfo)
		return sysInfo, nil
	}
}

func getPathKeyVal(p *gnmi.Path, elem, key string) string {
	for _, e := range p.GetElem() {
		if e.Name == elem {
			return e.Key[key]
		}
	}
	return ""
}
