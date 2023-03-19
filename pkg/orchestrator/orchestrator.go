package orchestrator

import (
	"github.com/Glimesh/waveguide/config"
	"github.com/Glimesh/waveguide/pkg/orchestrator/dummy"
	"github.com/Glimesh/waveguide/pkg/orchestrator/rt"
	"github.com/Glimesh/waveguide/pkg/types"

	"github.com/sirupsen/logrus"
)

type Orchestrator interface {
	// Name of the service, eg: Glimesh
	Name() string
	// Connect to the service
	Connect() error
	// Close the service connection
	Close() error

	SetLogger(logrus.FieldLogger)

	// TODO: Consider removing as public method
	// SendMessage(messageType uint8, payload []byte) error

	StartStream(channelID types.ChannelID, streamID types.StreamID) error
	StopStream(channelID types.ChannelID, streamID types.StreamID) error
	Heartbeat(channelID types.ChannelID) error

	// TODO: Be less specific to the FTL Orchestrator
	// SendIntro(message interface{})
	// SendOutro(message interface{})
	// SendNodeState(message interface{})
	// SendChannelSubscription(message interface{})
	// SendStreamPublishing(message interface{})
	// SendStreamRelaying(message interface{})
}

func New(cfg config.Config, hostname string, logger *logrus.Logger) Orchestrator {
	orCfg := cfg.Orchestrator

	var or Orchestrator

	switch orCfg.Type {
	case "dummy":
		or = dummy.New(dummy.Config{}, hostname)
	case "rt":
		or = rt.New(hostname, orCfg.Endpoint, orCfg.Key, orCfg.WHEPEndpoint)
		// case "ftl":
		// TODO: ftl orchestrator
	default:
		panic("unknown orchestrator type")
	}

	or.SetLogger(logger.WithFields(logrus.Fields{
		"orchestrator": or.Name(),
	}))

	return or
}
