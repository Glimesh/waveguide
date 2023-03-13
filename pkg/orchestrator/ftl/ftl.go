package ftl_orchestrator

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"net"

	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/sirupsen/logrus"
)

type Client struct {
	ClientHostname string

	config    *Config
	logger    logrus.FieldLogger
	callbacks Callbacks

	connected     bool
	transport     net.Conn
	lastMessageID uint8
}

type Callbacks struct {
	OnIntro               func(message IntroMessage)
	OnOutro               func(message OutroMessage)
	OnNodeState           func(message NodeStateMessage)
	OnChannelSubscription func(message ChannelSubscriptionMessage)
	OnStreamPublishing    func(message StreamPublishingMessage)
	OnStreamRelaying      func(message StreamRelayingMessage)
}

type Config struct {
	// Address of the remote orchestrator ip:port format
	Address string
	// RegionCode we are representing
	RegionCode string
	// Hostname for ourselves, so edges know how to reach us
	Hostname string
	// Handler for callbacks
	Callbacks Callbacks
}

func NewClient(config Config) *Client {
	return &Client{
		ClientHostname: config.Hostname,
		config:         &config,
	}
}

func (client *Client) Name() string {
	return "FTL Orchestrator"
}

func (client *Client) SetLogger(log logrus.FieldLogger) {
	client.logger = log
}

func (client *Client) Connect() error {
	transport, err := net.Dial("tcp", client.config.Address)
	if err != nil {
		return err
	}

	client.transport = transport
	client.lastMessageID = 1
	client.connected = true

	if client.logger == nil {
		client.logger = logrus.New()
	}

	client.callbacks = client.config.Callbacks

	intro := IntroMessage{
		VersionMajor:    0,
		VersionMinor:    0,
		VersionRevision: 0,
		RelayLayer:      0,
		RegionCode:      client.config.RegionCode,
		Hostname:        client.config.Hostname,
	}
	err = client.sendMessage(TypeIntro, intro.Encode())
	if err != nil {
		client.connected = false
		return err
	}

	go client.eternalRead()
	return nil
}

func (client *Client) eternalRead() {
	// I think this has to do something funky
	// https://github.com/Glimesh/janus-ftl-orchestrator/blob/974b55956094d1e1d29e060c8fb056d522a3d153/inc/FtlConnection.h

	scanner := bufio.NewScanner(client.transport)
	scanner.Split(splitOrchestratorMessages)
	for scanner.Scan() {
		if !client.connected {
			return
		}

		// ready to go header + message
		buf := scanner.Bytes()

		client.logger.Debugf("Receiving Orchestrator Hex: %s", insertNth(hex.EncodeToString(buf), 2))

		client.parseMessage(buf)
	}
	if err := scanner.Err(); err != nil {
		// For now if we lose the connection with the orchestrator we crash the server
		client.logger.Fatal("Error decoding Orchestrator input:", err)
	}
}

func splitOrchestratorMessages(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// Return nothing if at end of file and no data passed
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	if len(data) >= 4 {
		messageHeader := DecodeMessageHeader(data)
		messageSize := int(messageHeader.PayloadLength) + 4
		return messageSize, data[0:messageSize], nil
	}

	// If at end of file with data return the data
	if atEOF {
		return len(data), data, nil
	}

	return
}

func (client *Client) Close() error {
	client.logger.Info("Sending Outro Message to Orchestrator")
	if !client.connected {
		// Already closed
		return nil
	}

	// Both of these can error, but we're trying to close the connection anyway
	outro := OutroMessage{Reason: "Going away"}
	client.sendMessage(TypeOutro, outro.Encode())
	// client.transport.Close()

	client.connected = false
	return nil
}

func (client Client) StartStream(channelID control.ChannelID, streamID control.StreamID) error {
	message := StreamPublishingMessage{
		Context:   1,
		ChannelID: channelID,
		StreamID:  streamID,
	}
	return client.sendMessage(TypeStreamPublishing, message.Encode())
}

func (client Client) StopStream(channelID control.ChannelID, streamID control.StreamID) error {
	message := StreamPublishingMessage{
		Context:   0,
		ChannelID: channelID,
		StreamID:  streamID,
	}
	return client.sendMessage(TypeStreamPublishing, message.Encode())
}

func (client *Client) parseMessage(raw []byte) {
	messageHeader := DecodeMessageHeader(raw)
	message := raw[4 : 4+int(messageHeader.PayloadLength)]

	client.handleMessage(*messageHeader, message)
}

func (client *Client) handleMessage(header MessageHeader, payload []byte) {
	if !header.Request {
		// We don't need to bother decoding
		// Responses are meaningless, unless maybe we need them as a confirmation?
		return
	}

	client.logger.Debugf("Got message from Orchestrator: %#v", header)
	switch header.Type {
	case TypeIntro:
		if client.callbacks.OnIntro != nil {
			client.callbacks.OnIntro(DecodeIntroMessage(payload))
		}
	case TypeOutro:
		if client.callbacks.OnOutro != nil {
			client.callbacks.OnOutro(DecodeOutroMessage(payload))
		}
	case TypeNodeState:
		if client.callbacks.OnNodeState != nil {
			client.callbacks.OnNodeState(DecodeNodeStateMessage(payload))
		}
	case TypeChannelSubscription:
		if client.callbacks.OnChannelSubscription != nil {
			client.callbacks.OnChannelSubscription(DecodeChannelSubscriptionMessage(payload))
		}
	case TypeStreamPublishing:
		if client.callbacks.OnStreamPublishing != nil {
			client.callbacks.OnStreamPublishing(DecodeStreamPublishingMessage(payload))
		}
	case TypeStreamRelaying:
		if client.callbacks.OnStreamRelaying != nil {
			client.callbacks.OnStreamRelaying(DecodeStreamRelayingMessage(payload))
		}
	}
}

func (client *Client) sendMessage(messageType uint8, payload []byte) error {
	if !client.connected {
		return errors.New("orchestrator connection is closed")
	}

	// Construct the message
	message := MessageHeader{
		Request:       true,
		Success:       true,
		Type:          messageType,
		ID:            client.lastMessageID,
		PayloadLength: uint16(len(payload)),
	}
	messageBuffer := message.Encode()
	messageBuffer = append(messageBuffer, payload...)

	client.logger.Debugf("Sending Orchestrator Hex: %s", insertNth(hex.EncodeToString(messageBuffer), 2))
	_, err := client.transport.Write(messageBuffer)
	if err != nil {
		return err
	}

	client.lastMessageID += 1

	return nil
}

func insertNth(s string, n int) string {
	var buffer bytes.Buffer
	n1 := n - 1
	l1 := len(s) - 1
	for i, r := range s {
		buffer.WriteRune(r)
		if i%n == n1 && i != l1 {
			buffer.WriteRune(' ')
		}
	}
	return buffer.String()
}
