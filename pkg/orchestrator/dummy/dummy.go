package dummy

import (
	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/sirupsen/logrus"
)

type Client struct {
	hostname string

	config *Config
	log    logrus.FieldLogger

	connected bool
}

type Callbacks struct {
	OnIntro               func(message interface{})
	OnOutro               func(message interface{})
	OnNodeState           func(message interface{})
	OnChannelSubscription func(message interface{})
	OnStreamPublishing    func(message interface{})
	OnStreamRelaying      func(message interface{})
}

type Config struct {
	// RegionCode we are representing
	RegionCode string
	// Hostname for ourselves, so edges know how to reach us
	Hostname string
	// Logger for orchestrator client messages
	Logger logrus.FieldLogger
	// Handler for callbacks
	Callbacks Callbacks
}

func New(config Config, hostname string) *Client {
	return &Client{
		hostname: hostname,
		config:   &config,
	}
}

func (client *Client) SetLogger(log logrus.FieldLogger) {
	client.log = log
}

func (client *Client) Name() string {
	return "Dummy Orchestrator"
}

func (client *Client) Connect() error {
	client.log.Info("Connecting to Dummy Orchestrator")
	client.connected = true
	return nil
}

func (client *Client) Close() error {
	client.log.Info("Closing connection to Dummy Orchestrator")
	if !client.connected {
		// Already closed
		return nil
	}

	client.connected = false
	return nil
}

func (client *Client) StartStream(channelID control.ChannelID, streamID control.StreamID) error {
	return nil
}
func (client *Client) StopStream(channelID control.ChannelID, streamID control.StreamID) error {
	return nil
}
func (client *Client) Heartbeat(channelID control.ChannelID) error {
	return nil
}
