package ftl_orchestrator

import (
	"bufio"
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIntroMessage(t *testing.T) {
	assert := assert.New(t)

	intro := IntroMessage{
		VersionMajor:    0,
		VersionMinor:    0,
		VersionRevision: 0,
		RelayLayer:      0,
		RegionCode:      "sea",
		Hostname:        "test",
	}
	payloadBuffer := intro.Encode()

	// Construct the message
	message := MessageHeader{
		Request:       true,
		Success:       true,
		Type:          TypeIntro,
		ID:            1,
		PayloadLength: uint16(len(payloadBuffer)),
	}
	headerBuffer := message.Encode()
	fmt.Printf("HeaderBuffer & PayloadBuffer: %v + %v\n", headerBuffer, payloadBuffer)
	messageBuffer := append(headerBuffer, payloadBuffer...)

	fmt.Printf("MessageBuffer: %v\n", messageBuffer)

	decoded := DecodeIntroMessage(payloadBuffer)

	fmt.Printf("decoded = %#v\n", decoded)

	assert.Equal(intro.VersionMajor, decoded.VersionMajor)
	assert.Equal(intro.VersionMinor, decoded.VersionMinor)
	assert.Equal(intro.VersionRevision, decoded.VersionRevision)
	assert.Equal(intro.RegionCode, decoded.RegionCode)
	assert.Equal(intro.Hostname, decoded.Hostname)

	decodedHeader := DecodeMessageHeader(headerBuffer)
	assert.Equal(message.Request, decodedHeader.Request)
	assert.Equal(message.Success, decodedHeader.Success)
	assert.Equal(message.Type, decodedHeader.Type)
	assert.Equal(message.ID, decodedHeader.ID)
	assert.Equal(message.PayloadLength, decodedHeader.PayloadLength)
}

func TestChannelSubscriptionMessage(t *testing.T) {
	assert := assert.New(t)

	channelSub := ChannelSubscriptionMessage{
		Context:   1,
		ChannelID: 304,
		StreamKey: "some-stream-key",
	}
	payloadBuffer := channelSub.Encode()

	// Construct the message
	message := MessageHeader{
		Request:       false,
		Success:       true,
		Type:          TypeChannelSubscription,
		ID:            1,
		PayloadLength: uint16(len(payloadBuffer)),
	}
	headerBuffer := message.Encode()
	fmt.Printf("HeaderBuffer & PayloadBuffer: %v + %v\n", headerBuffer, payloadBuffer)
	messageBuffer := append(headerBuffer, payloadBuffer...)

	fmt.Printf("MessageBuffer: %v\n", messageBuffer)

	//decoded := DecodeIntroMessage(payloadBuffer)
	//
	//fmt.Printf("decoded = %#v\n", decoded)
	//
	//assert.Equal(intro.VersionMajor, decoded.VersionMajor)
	//assert.Equal(intro.VersionMinor, decoded.VersionMinor)
	//assert.Equal(intro.VersionRevision, decoded.VersionRevision)
	//assert.Equal(intro.RegionCode, decoded.RegionCode)
	//assert.Equal(intro.Hostname, decoded.Hostname)

	decodedHeader := DecodeMessageHeader(headerBuffer)
	assert.Equal(message.Request, decodedHeader.Request)
	assert.Equal(message.Success, decodedHeader.Success)
	assert.Equal(message.Type, decodedHeader.Type)
	assert.Equal(message.ID, decodedHeader.ID)
	assert.Equal(message.PayloadLength, decodedHeader.PayloadLength)
}

func TestMessageSplitting(t *testing.T) {
	assert := assert.New(t)

	scanner := bufio.NewScanner(bytes.NewReader([]byte{128, 1, 0, 0, 20, 0, 52, 0, 1, 48, 1, 0, 0, 1, 0, 0, 0, 9, 0, 108, 105, 110, 117, 120, 45, 100, 101, 118}))
	scanner.Split(splitOrchestratorMessages)
	// First Message
	for scanner.Scan() {
		assert.Equal(scanner.Bytes(), []byte{128, 1, 0, 0})
	}
	// Second message
	for scanner.Scan() {
		assert.Equal(scanner.Bytes(), []byte{20, 0, 52, 0, 1, 48, 1, 0, 0, 1, 0, 0, 0, 9, 0, 108, 105, 110, 117, 120, 45, 100, 101, 118})
	}
}
