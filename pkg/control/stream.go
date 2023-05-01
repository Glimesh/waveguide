package control

import (
	"context"
	"errors"

	"github.com/Glimesh/waveguide/pkg/keyframer"
	"github.com/Glimesh/waveguide/pkg/types"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

type StreamTrack struct {
	Type  webrtc.RTPCodecType
	Codec string
	Track webrtc.TrackLocal
}

type Stream struct {
	log logrus.FieldLogger

	cancelFunc context.CancelFunc
	stopped    bool

	// authenticated is set after the stream has successfully authed with a remote service
	authenticated bool

	whepURI string

	saveVideo   bool
	videoWriter FileWriter

	// mediaStarted is set after media bytes have come in from the client
	mediaStarted bool
	hasSomeAudio bool
	hasSomeVideo bool

	kf            *keyframer.Keyframer
	rtpIngest     chan *rtp.Packet
	lastThumbnail chan []byte
	// channel used to signal thumbnailer to stop
	stopThumbnailer   chan struct{}
	stopHeartbeat     chan struct{}
	requestThumbnail  chan struct{}
	thumbnailReceiver chan *rtp.Packet

	videoWriterChan chan *rtp.Packet

	ChannelID types.ChannelID
	StreamID  types.StreamID
	StreamKey types.StreamKey

	tracks []StreamTrack

	// Raw Metadata
	startTime           int64
	lastTime            int64 // Last time the metadata collector ran
	audioBps            int
	videoBps            int
	totalAudioPackets   int
	totalVideoPackets   int
	lastAudioPackets    int
	lastVideoPackets    int
	clientVendorName    string
	clientVendorVersion string
	videoCodec          string
	audioCodec          string
	videoHeight         int
	videoWidth          int
}

func (s *Stream) AddTrack(track webrtc.TrackLocal, codec string) error {
	// TODO: Needs better support for tracks with different codecs
	if track.Kind() == webrtc.RTPCodecTypeAudio {
		s.hasSomeAudio = true
		s.audioCodec = codec
	} else if track.Kind() == webrtc.RTPCodecTypeVideo {
		s.hasSomeVideo = true
		s.videoCodec = codec
	} else {
		return errors.New("unexpected track kind")
	}

	s.tracks = append(s.tracks, StreamTrack{
		Type:  track.Kind(),
		Track: track,
		Codec: codec,
	})

	return nil
}

func (s *Stream) ReportMetadata(metadatas ...Metadata) error {
	for _, metadata := range metadatas {
		metadata(s)
	}

	return nil
}

func (s *Stream) Stop() {
	s.log.Infof("stopping stream")

	s.stopHeartbeat <- struct{}{} // not being used anywhere, is it really needed?

	s.cancelFunc()
	s.stopped = true
	s.log.Debug("sent stop thumbnailer signal")

	s.log.Debug("canceled stream ctx")
}

func (s *Stream) Stopped() bool {
	return s.stopped
}
