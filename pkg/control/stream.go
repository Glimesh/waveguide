package control

import (
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"

	"github.com/Glimesh/waveguide/pkg/h264"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
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

	// recentVideoPackets []*rtp.Packet
	lastKeyframe []byte
	// rtpBuffer    bytes.Buffer

	packetBuffer []*rtp.Packet
	Keyframer    *Keyframer

	VideoPackets chan *rtp.Packet
	videoSampler *samplebuilder.SampleBuilder
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

// ReportLastKeyframe works similar to stream.VideoPackets <- packet, except it's used in situations
// where we are converting from other video formats and we easily know the keyframes.
func (s *Stream) ReportLastKeyframe(keyframe []byte) error {
	s.lastKeyframe = keyframe

	return nil
}

func (s *Stream) KeyframeCollector() {

	// for {
	// 	p := <-s.VideoPackets

	// 	keyframe := kfer.WriteRTP(p)
	// 	if keyframe != nil {
	// 		fmt.Printf("!!! STREAM KEYFRAME !!! %s\n\n", kfer)
	// 		saveImage(int(p.SequenceNumber), keyframe)
	// 		os.WriteFile(fmt.Sprintf("%d-stream.h264", p.SequenceNumber), keyframe, 0666)
	// 		kfer.Reset()
	// 	}
	// }
}

func saveImage(n int, buf []byte) {

	var img image.Image
	h264dec, err := h264.NewH264Decoder()
	if err != nil {
		panic(err)
	}
	defer h264dec.Close()
	img, err = h264dec.Decode(buf)
	if err != nil {
		panic(err)
	}
	if img == nil {
		fmt.Println("img is nil")
		return
	}

	imgName := fmt.Sprintf("%d.jpg", n)
	out, _ := os.Create(imgName)
	// buff := new(bytes.Buffer)
	err = jpeg.Encode(out, img, &jpeg.Options{
		Quality: 75,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("Saved image:", imgName)
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
