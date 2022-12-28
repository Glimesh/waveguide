package ftl_orchestrator

import (
	"encoding/binary"

	"github.com/Glimesh/waveguide/pkg/control"
)

// Message Types
const (
	TypeIntro     = 0
	TypeOutro     = 1
	TypeNodeState = 2
	// 3 - 15 	Reserved 	Reserved for future use (server state messaging)
	TypeChannelSubscription = 16
	TypeStreamPublishing    = 17
	TypeStreamRelaying      = 20
	// 19 - 31 	Reserved 	Reserved for future use
	// 32 - 63 	Reserved 	Reserved for future use
)

// IntroMessage Sent on connect with identifying information.
type IntroMessage struct {
	VersionMajor    uint8
	VersionMinor    uint8
	VersionRevision uint8
	RelayLayer      uint8
	RegionCode      string
	Hostname        string
}

func (im *IntroMessage) Encode() []byte {
	var buf []byte

	buf = append(buf, im.VersionMajor, im.VersionMinor, im.VersionRevision, im.RelayLayer)

	regionCodeLength := make([]byte, 2)
	binary.LittleEndian.PutUint16(regionCodeLength, uint16(len(im.RegionCode)))

	binary.LittleEndian.Uint16([]byte{0, 21})

	buf = append(buf, regionCodeLength...)
	buf = append(buf, []byte(im.RegionCode)...)
	buf = append(buf, []byte(im.Hostname)...)

	return buf
}
func DecodeIntroMessage(buf []byte) IntroMessage {
	regionEnd := 6 + binary.LittleEndian.Uint16(buf[4:6])

	return IntroMessage{
		VersionMajor:    buf[0],
		VersionMinor:    buf[1],
		VersionRevision: buf[2],
		RelayLayer:      buf[3],
		RegionCode:      string(buf[6:regionEnd]),
		Hostname:        string(buf[regionEnd:]),
	}
}

// OutroMessage Sent on disconnect with information on the reason for disconnect.
type OutroMessage struct {
	Reason string
}

func (im *OutroMessage) Encode() []byte {
	var buf []byte

	buf = append(buf, []byte(im.Reason)...)

	return buf
}
func DecodeOutroMessage(buf []byte) OutroMessage {
	return OutroMessage{}
}

// NodeStateMessage Sent periodically by nodes to indicate their current state.
type NodeStateMessage struct {
	CurrentLoad uint32
	MaximumLoad uint32
}

func (im *NodeStateMessage) Encode() []byte {
	var buf []byte

	currentLoad := make([]byte, 4)
	binary.LittleEndian.PutUint32(currentLoad, im.CurrentLoad)
	maximumLoad := make([]byte, 4)
	binary.LittleEndian.PutUint32(maximumLoad, im.MaximumLoad)

	buf = append(buf, currentLoad...)
	buf = append(buf, maximumLoad...)

	return buf
}
func DecodeNodeStateMessage(buf []byte) NodeStateMessage {
	return NodeStateMessage{}
}

// ChannelSubscriptionMessage Indicates whether streams for a given channel should be relayed to this node.
type ChannelSubscriptionMessage struct {
	Context   uint8
	ChannelID control.ChannelID
	StreamKey string
}

func (im *ChannelSubscriptionMessage) Encode() []byte {
	var buf []byte

	channelID := make([]byte, 4)
	binary.LittleEndian.PutUint32(channelID, uint32(im.ChannelID))

	buf = append(buf, im.Context)
	buf = append(buf, channelID...)
	buf = append(buf, []byte(im.StreamKey)...)

	return buf
}
func DecodeChannelSubscriptionMessage(buf []byte) ChannelSubscriptionMessage {
	return ChannelSubscriptionMessage{}
}

// StreamPublishingMessage Indicates that a new stream is now available (or unavailable) from this connection.
type StreamPublishingMessage struct {
	Context   uint8
	ChannelID control.ChannelID
	StreamID  control.StreamID
}

func (im *StreamPublishingMessage) Encode() []byte {
	var buf []byte

	channelID := make([]byte, 4)
	binary.LittleEndian.PutUint32(channelID, uint32(im.ChannelID))
	streamID := make([]byte, 4)
	binary.LittleEndian.PutUint32(streamID, uint32(im.StreamID))

	buf = append(buf, im.Context)
	buf = append(buf, channelID...)
	buf = append(buf, streamID...)

	return buf
}
func DecodeStreamPublishingMessage(buf []byte) StreamPublishingMessage {
	channelId := 6 + binary.LittleEndian.Uint32(buf[4:8])
	streamId := 6 + binary.LittleEndian.Uint32(buf[8:12])

	return StreamPublishingMessage{
		Context:   buf[0],
		ChannelID: control.ChannelID(channelId),
		StreamID:  control.StreamID(streamId),
	}
}

// StreamRelayingMessage Contains information used for relaying streams between nodes.
type StreamRelayingMessage struct {
	Context        uint8
	ChannelID      control.ChannelID
	StreamID       control.StreamID
	TargetHostname string
	StreamKey      []byte
}

func (im *StreamRelayingMessage) Encode() []byte {
	var buf []byte

	var channelID []byte
	binary.LittleEndian.PutUint32(channelID, uint32(im.ChannelID))
	var streamID []byte
	binary.LittleEndian.PutUint32(streamID, uint32(im.StreamID))
	targetHostnameLength := make([]byte, 2)
	binary.LittleEndian.PutUint16(targetHostnameLength, uint16(len(im.TargetHostname)))

	buf = append(buf, im.Context)
	buf = append(buf, channelID...)
	buf = append(buf, streamID...)
	buf = append(buf, targetHostnameLength...)
	buf = append(buf, []byte(im.TargetHostname)...)
	buf = append(buf, im.StreamKey...)

	return buf
}
func DecodeStreamRelayingMessage(buf []byte) StreamRelayingMessage {
	channelId := binary.LittleEndian.Uint32(buf[1:5])
	streamId := binary.LittleEndian.Uint32(buf[5:9])
	hostnameEnd := 11 + binary.LittleEndian.Uint16(buf[9:11])

	return StreamRelayingMessage{
		Context:        buf[0],
		ChannelID:      control.ChannelID(channelId),
		StreamID:       control.StreamID(streamId),
		TargetHostname: string(buf[11:hostnameEnd]),
		StreamKey:      buf[hostnameEnd:],
	}
}

// MessageHeader
// |-                       32 bit / 4 byte                       -|
// +---------------------------------------------------------------+
// |  Msg Desc (8)  |   Msg Id (8)   |     Payload Length (16)     |
// +---------------------------------------------------------------+
type MessageHeader struct {
	// Request Serializes from Request / Response
	Request bool
	// Success Serializes from Success / Failure
	Success       bool
	Type          uint8
	ID            uint8
	PayloadLength uint16
}

func (msg MessageHeader) Encode() []byte {
	var headerBytes []byte

	msgDesc := msg.Type
	if !msg.Request {
		msgDesc = msgDesc | 0b10000000
	}
	if !msg.Success {
		msgDesc = msgDesc | 0b01000000
	}
	headerBytes = append(headerBytes, msgDesc)
	headerBytes = append(headerBytes, msg.ID)

	payloadLength := make([]byte, 2)
	binary.LittleEndian.PutUint16(payloadLength, msg.PayloadLength)

	headerBytes = append(headerBytes, payloadLength...)

	return headerBytes
}
func DecodeMessageHeader(buf []byte) *MessageHeader {
	payloadLength := binary.LittleEndian.Uint16(buf[2:4])

	return &MessageHeader{
		Request:       (buf[0] & 0b10000000) == 0,
		Success:       (buf[0] & 0b01000000) == 0,
		Type:          buf[0] & 0b00111111,
		ID:            buf[1],
		PayloadLength: payloadLength,
	}
}
