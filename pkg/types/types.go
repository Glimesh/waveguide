package types

import "fmt"

type ChannelID uint32
type StreamID uint32
type StreamKey []byte

func (id ChannelID) String() string {
	return fmt.Sprintf("%d", id)
}

func (id StreamID) String() string {
	return fmt.Sprintf("%d", id)
}

type StreamMetadata struct {
	AudioCodec        string
	IngestServer      string
	IngestViewers     int
	LostPackets       int
	NackPackets       int
	RecvPackets       int
	SourceBitrate     int
	SourcePing        int
	StreamTimeSeconds int
	VendorName        string
	VendorVersion     string
	VideoCodec        string
	VideoHeight       int
	VideoWidth        int
}
