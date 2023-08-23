package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nokia/srlinux-ndk-go/ndk"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/prototext"
)

func (a *app) updateTelemetryPathConfig(jsPath string, jsData string) {
	log.Infof("updating: %s: %s", jsPath, jsData)
	key := &ndk.TelemetryKey{JsPath: jsPath}
	data := &ndk.TelemetryData{JsonContent: jsData}
	info := &ndk.TelemetryInfo{Key: key, Data: data}
	telReq := &ndk.TelemetryUpdateRequest{
		State: []*ndk.TelemetryInfo{info},
	}
	log.Debugf("Updating telemetry with: %+v", telReq)
	b, err := prototext.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(telReq)
	if err != nil {
		log.Errorf("telemetry request Marshal failed: %+v", err)
	}
	fmt.Printf("%s\n", string(b))
	r1, err := a.agent.TelemetryServiceClient.TelemetryAddOrUpdate(a.ctx, telReq)
	if err != nil {
		log.Errorf("Could not update telemetry key=%s: err=%v", jsPath, err)
		return
	}
	log.Infof("Telemetry add/update status: %s, error_string: %q", r1.GetStatus().String(), r1.GetErrorStr())
}

func (a *app) deleteTelemetryPath(jsPath string) error {
	key := &ndk.TelemetryKey{JsPath: jsPath}
	telReq := &ndk.TelemetryDeleteRequest{}
	telReq.Key = make([]*ndk.TelemetryKey, 0)
	telReq.Key = append(telReq.Key, key)

	b, err := prototext.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(telReq)
	if err != nil {
		log.Errorf("telemetry request Marshal failed: %+v", err)
	}
	fmt.Printf("%s\n", string(b))

	r1, err := a.agent.TelemetryServiceClient.TelemetryDelete(a.ctx, telReq)
	if err != nil {
		log.Errorf("could not delete telemetry for key : %s", jsPath)
		return err
	}
	log.Debugf("telemetry delete status: %s, error_string: %q", r1.GetStatus().String(), r1.GetErrorStr())
	return nil
}

func (a *app) updateRootLevelTelemetry(appCfg *appConfig) {
	jsData, err := json.Marshal(appCfg)
	if err != nil {
		log.Errorf("failed to marshal json data: %v", err)
		return
	}

	a.updateTelemetryPathConfig(grpcTunnelPath, string(jsData))
}

// destination telemetry functions

func (a *app) updateDestinationTelemetry(name string, dgc *destination) {
	jsData, err := json.Marshal(dgc)
	if err != nil {
		log.Errorf("failed to marshal json data: %v", err)
		return
	}
	p := fmt.Sprintf("%s{.name==\"%s\"}", destinationPath, name)
	a.updateTelemetryPathConfig(p, string(jsData))
}

func (a *app) deleteDestinationTelemetry(ctx context.Context, name string) {
	jsPath := fmt.Sprintf("%s{.name==\"%s\"}", destinationPath, name)
	log.Infof("Deleting telemetry path %s", jsPath)
	a.deleteTelemetryPath(jsPath)
}

// tunnel telemetry functions

func (a *app) updateTunnelTelemetry(name string, dgc *tunnelCfg) {
	jsData, err := json.Marshal(dgc)
	if err != nil {
		log.Errorf("failed to marshal json data: %v", err)
		return
	}
	p := fmt.Sprintf("%s{.name==\"%s\"}", tunnelPath, name)
	a.updateTelemetryPathConfig(p, string(jsData))
}

func (a *app) deleteTunnelTelemetry(ctx context.Context, name string) {
	jsPath := fmt.Sprintf("%s{.name==\"%s\"}", tunnelPath, name)
	log.Infof("Deleting telemetry path %s", jsPath)
	a.deleteTelemetryPath(jsPath)
}

// tunnel handler telemetry functions

func (a *app) updateTunnelTargetTelemetry(tName, hName string, h *target) {
	jsData, err := json.Marshal(h)
	if err != nil {
		log.Errorf("failed to marshal json data: %v", err)
		return
	}
	p := fmt.Sprintf("%s{.name==\"%s\"}.target{.name==\"%s\"}", tunnelPath, tName, hName)
	a.updateTelemetryPathConfig(p, string(jsData))
}

func (a *app) deleteTunnelTargetTelemetry(tName, hName string) {
	jsPath := fmt.Sprintf("%s{.name==\"%s\"}.target{.name==\"%s\"}", tunnelPath, tName, hName)
	log.Infof("Deleting telemetry path %s", jsPath)
	a.deleteTelemetryPath(jsPath)
}

// tunnel destination telemetry functions
func (a *app) updateTunnelDestinationTelemetry(tName, dName string, ds *destinationState) {
	jsData, err := json.Marshal(ds)
	if err != nil {
		log.Errorf("failed to marshal json data: %v", err)
		return
	}
	p := fmt.Sprintf("%s{.name==\"%s\"}.destination{.name==\"%s\"}", tunnelPath, tName, dName)
	a.updateTelemetryPathConfig(p, string(jsData))
}

func (a *app) deleteTunnelDestinationTelemetry(tName, dName string) {
	jsPath := fmt.Sprintf("%s{.name==\"%s\"}.destination{.name==\"%s\"}", tunnelPath, tName, dName)
	log.Infof("Deleting telemetry path %s", jsPath)
	a.deleteTelemetryPath(jsPath)
}

// tunnel destination target telemetry functions

func (a *app) updateTunnelDestinationTargetTelemetry(tName, dName, tID, tType string, ts *targetState) {
	jsData, err := json.Marshal(ts)
	if err != nil {
		log.Errorf("failed to marshal json data: %v", err)
		return
	}
	p := fmt.Sprintf("%s{.name==\"%s\"}.destination{.name==\"%s\"}.target{.id==\"%s\"&&.type==\"%s\"}",
		tunnelPath, tName, dName, tID, tType)
	a.updateTelemetryPathConfig(p, string(jsData))
}

func (a *app) deleteTunnelDestinationTargetTelemetry(tName, dName, tID, tType string) {
	jsPath := fmt.Sprintf("%s{.name==\"%s\"}.destination{.name==\"%s\"}.target{.id==\"%s\"&&.type==\"%s\"}",
		tunnelPath, tName, dName, tID, tType)
	log.Infof("Deleting telemetry path %s", jsPath)
	a.deleteTelemetryPath(jsPath)
}
