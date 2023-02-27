package main

import (
	"context"
	"fmt"
	"sync"

	agent "github.com/karimra/srl-ndk-demo"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/prototext"
)

type Option func(*app)

type app struct {
	config *config
	agent  *agent.Agent
	ctx    context.Context
	//
	m *sync.RWMutex
	// [tunnelName] / [destinationName]
	tunnelClients map[string]map[string]*tunnelDestinationClient
}

func WithAgent(agt *agent.Agent) func(a *app) {
	return func(a *app) {
		a.agent = agt
	}
}

func newApp(ctx context.Context, opts ...Option) *app {
	a := &app{
		config: newConfig(),
		ctx:    ctx,
		//
		m:             new(sync.RWMutex),
		tunnelClients: make(map[string]map[string]*tunnelDestinationClient),
	}

	for _, opt := range opts {
		opt(a)
	}
	return a
}

// starts config notification and network instance notification streams.
// listens to updates from both and calls relevant handlers.
func (a *app) start(ctx context.Context) {
	cfgStream := a.agent.StartConfigNotificationStream(ctx)
	nwInstStream := a.agent.StartNwInstNotificationStream(ctx)
	for {
		select {
		case nwInstEvent := <-nwInstStream:
			log.Debugf("NwInst notification: %+v", nwInstEvent)

			b, err := prototext.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(nwInstEvent)
			if err != nil {
				log.Errorf("NwInst notification Marshal failed: %+v", err)
				continue
			}
			fmt.Printf("%s\n", string(b))

			for _, ev := range nwInstEvent.GetNotification() {
				if nwInst := ev.GetNwInst(); nwInst != nil {
					a.handleNwInstCfg(ctx, nwInst)
					continue
				}
				log.Warnf("got empty nwInst, event: %+v", ev)
			}
		case event := <-cfgStream:
			log.Infof("Config notification: %+v", event)

			b, err := prototext.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(event)
			if err != nil {
				log.Infof("Config notification Marshal failed: %+v", err)
				continue
			}
			fmt.Printf("%s\n", string(b))

			for _, ev := range event.GetNotification() {
				if cfg := ev.GetConfig(); cfg != nil {
					a.handleConfigEvent(ctx, cfg)
					continue
				}
				log.Infof("got empty config, event: %+v", ev)
			}
		case <-ctx.Done():
			return
		}
	}
}
