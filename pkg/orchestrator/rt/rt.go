package rt

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Glimesh/waveguide/pkg/types"
	"github.com/sirupsen/logrus"
)

type Client struct {
	hostname string

	log logrus.FieldLogger

	connected bool

	// RTRouterEndpoint is the URL of a public RTRouter
	Endpoint string
	// Key is the secret key to be used for stateful changes
	Key string
	// Needs to be hardcoded for now...
	WHEPEndpoint string `mapstructure:"whep_endpoint"`
}

func New(hostname, endpoint, key, whepEndpoint string) *Client {
	return &Client{
		hostname:     hostname,
		Endpoint:     endpoint,
		Key:          key,
		WHEPEndpoint: whepEndpoint,
	}
}

func (client *Client) SetLogger(log logrus.FieldLogger) {
	client.log = log
}

func (client *Client) Name() string {
	return "RT Orchestrator"
}

// Since RTRouter is HTTP, no permanent connection is necessary.
func (client *Client) Connect() error {
	client.connected = true
	return nil
}

// Likely this needs to tell the orchestrator all URLs for this endpoint are no longer valid
func (client *Client) Close() error {
	if !client.connected {
		// Already closed
		return nil
	}

	client.connected = false
	return nil
}

func (client *Client) StartStream(channelID types.ChannelID, streamID types.StreamID) error {
	form := url.Values{}
	form.Add("channel_id", fmt.Sprint(channelID))
	form.Add("endpoint", client.channelEndpoint(channelID))

	req, err := http.NewRequest("POST", client.routerEndpoint("v1/state/start_stream"), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Authorization", client.Key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if status := resp.StatusCode; status != http.StatusAccepted {
		return fmt.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusAccepted)
	}

	return nil
}
func (client *Client) StopStream(channelID types.ChannelID, streamID types.StreamID) error {
	form := url.Values{}
	form.Add("channel_id", fmt.Sprint(channelID))

	req, err := http.NewRequest("POST", client.routerEndpoint("v1/state/stop_stream"), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Authorization", client.Key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if status := resp.StatusCode; status != http.StatusOK {
		return fmt.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusAccepted)
	}

	return nil
}

func (client *Client) Heartbeat(channelID types.ChannelID) error {
	form := url.Values{}
	form.Add("channel_id", fmt.Sprint(channelID))

	req, err := http.NewRequest("POST", client.routerEndpoint("v1/state/heartbeat"), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Authorization", client.Key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if status := resp.StatusCode; status != http.StatusOK {
		return fmt.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusAccepted)
	}

	return nil
}

func (client *Client) routerEndpoint(path string) string {
	return fmt.Sprintf("%s/%s", client.Endpoint, path)
}

func (client *Client) channelEndpoint(channelID types.ChannelID) string {
	return fmt.Sprintf("%s/whep/endpoint/%d", client.WHEPEndpoint, channelID)
}
