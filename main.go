package main

import (
	"context"
	"flag"
	"fmt"

	"time"

	agent "github.com/karimra/srl-ndk-demo"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/metadata"
)

const (
	retryInterval        = 2 * time.Second
	agentName            = "grpc-tunnel"
	gnmiServerUnixSocket = "unix:///opt/srlinux/var/run/sr_gnmi_server"
)

var version = "dev"
var debug *bool

func main() {
	debug = flag.Bool("d", false, "turn on debug")
	versionFlag := flag.Bool("v", false, "print version")
	flag.Parse()

	if *versionFlag {
		fmt.Println(version)
		return
	}
	if *debug {
		log.SetLevel(log.DebugLevel)
		log.SetReportCaller(true)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "agent_name", agentName)

CRAGENT:
	app, err := agent.New(ctx, agentName)
	if err != nil {
		log.Errorf("failed to create agent %q: %v", agentName, err)
		log.Infof("retrying in %s", retryInterval)
		time.Sleep(retryInterval)
		goto CRAGENT
	}

	a := newApp(ctx, WithAgent(app))
	//
SYSINFO:
	log.Info("getting system info...")
	sysInfo, err := a.getSystemInfo(ctx)
	if err != nil {
		log.Errorf("failed to get system info %q: %v", agentName, err)
		log.Infof("retrying in %s", retryInterval)
		time.Sleep(retryInterval)
		goto SYSINFO
	}
	log.Infof("system info: %+v", sysInfo)
	a.config.sysInfo = *sysInfo
	log.Info("starting config handler...")
	a.start(ctx)
}
