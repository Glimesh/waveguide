package control

import (
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

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

	tracks []StreamTrack

	metadata StreamMetadata

	// Raw Metadata
	startTime           int64
	lastTime            int64 // Last time the metadata collector ran
	audioBps            int
	videoBps            int
	audioPackets        int
	videoPackets        int
	lastAudioPackets    int
	lastVideoPackets    int
	clientVendorName    string
	clientVendorVersion string
	videoCodec          string
	audioCodec          string
	videoHeight         int
	videoWidth          int

	recentVideoPackets []*rtp.Packet
	lastKeyframe       []byte
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
