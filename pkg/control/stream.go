package control

import "github.com/pion/webrtc/v3"

type StreamTrack struct {
	Type  webrtc.RTPCodecType
	Codec string
	Track webrtc.TrackLocal
}
type Stream struct {
	// authenticated is set after the stream has successfully authed with a remote service
	authenticated bool
	// mediaStarted is set after media bytes have come in from the client
	mediaStarted bool
	hasSomeAudio bool
	hasSomeVideo bool

	stopHeartbeat chan bool

	ChannelID ChannelID
	StreamID  StreamID
	StreamKey StreamKey

	tracks        []StreamTrack
	videoCodec    string
	lastFullFrame []byte

	metadata StreamMetadata
}

type StreamMetadata struct {
	AudioCodec        string
	RecvAudioPackets  int
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
	RecvVideoPackets  int
}

type LostPacketsMetadata int
type NackPacketsMetadata int
type RecvPacketsMetadata int
type SourceBitrateMetadata int
type SourcePingMetadata int
type RecvVideoPacketsMetadata int
type RecvAudioPacketsMetadata int

// const (
// 	// AudioCodecType string
// 	IngestServer iota
// 	IngestViewers
// 	LostPackets
// 	NackPackets
// 	RecvPackets
// 	SourceBitrate
// 	// SourcePing
// 	StreamTimeSeconds
// 	VendorName
// 	VendorVersion
// 	VideoCodec
// 	VideoHeight
// 	VideoWidth
// )

// type SourcePing int
