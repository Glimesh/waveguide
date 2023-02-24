package control

import (
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"

	"github.com/Glimesh/waveguide/pkg/h264"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
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
	lastKeyframe         []byte
	keyframePayloadParts [][]byte
	keyframePackets      []*rtp.Packet

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

type KeyframeInfo struct {
	Payload   []byte
	Timestamp uint32
	Complete  bool
}

func (s *Stream) KeyframeCollector() {
	// keyframeInfo := KeyframeInfo{
	// 	Payload:   make([]byte, 0),
	// 	Timestamp: 0,
	// 	Complete:  false,
	// }
	// packetBuf := make([]*rtp.Packet, 0)
	// sampler := samplebuilder.New(150, &codecs.H264Packet{}, 90000)
	sampler := samplebuilder.New(1024, &codecs.H264Packet{}, 90000)

	for {
		p := <-s.VideoPackets
		sampler.Push(p)

		if h264.IsKeyframePart(p.Payload) && p.Header.Marker {
			sample, ts := sampler.PopWithTimestamp()
			if sample == nil || len(sample.Data) == 0 {
				continue
			}
			// fmt.Printf("Sample: %#v", sample)
			s.lastKeyframe = sample.Data

			saveImage(int(ts), sample.Data)
		}

		// naluType := sample.Data[4] & 0x1F
		// videoKeyframe := (naluType == 7) || (naluType == 8)
		// if videoKeyframe {
		// 	if (s.videoWriter == nil || s.audioWriter == nil) && naluType == 7 {
		// 		p := bytes.SplitN(sample.Data[4:], []byte{0x00, 0x00, 0x00, 0x01}, 2)
		// 		s.lastKeyframe = p
		// 		// if width, height, fps, ok := H264_decode_sps(p[0], uint(len(p[0]))); ok {
		// 		// 	log.Errorf("width:%d, height:%d, fps:%d", width, height, fps)
		// 		// 	s.InitWriter(width, height)
		// 		// }
		// 	}
		// }

		// if h264.IsKeyframePart(p.Payload) {
		// 	// packetBuf = append(packetBuf, p)

		// 	if p.Header.Marker {
		// 		sample := sampler.Pop()
		// 		if sample == nil {
		// 			continue
		// 		}

		// 		saveImage(int(p.SequenceNumber), sample.Data)
		// 		// out := h264.AppendNalUnitsForLibav(packetBuf)
		// 		// fmt.Println(len(out))
		// 		// saveImage(int(p.SequenceNumber), out)
		// 		// packetBuf = make([]*rtp.Packet, 0)
		// 	}
		// }

		// s.videoSampler.Push(p)

		// In order to get a full keyframe, we need to add all matching keyframe packets together with the same timestamp, until a Marker is true. A marker being true is the final packet of the keyframe

		// if h264.IsKeyframe(p.Payload) {
		// 	// If this is our first ever keyframe packet
		// 	if keyframeInfo.Timestamp == 0 {
		// 		keyframeInfo.Timestamp = p.Timestamp
		// 	}

		// 	if p.Timestamp == keyframeInfo.Timestamp {
		// 		// If this packet is within the original keyframe packet timestamp
		// 		keyframeInfo.Payload = append(keyframeInfo.Payload, p.Payload...)
		// 		fmt.Printf("Packet: %#v\n", p.Payload)

		// 		if p.Marker {
		// 			keyframeInfo.Complete = true
		// 			s.lastKeyframe = keyframeInfo.Payload

		// 			fmt.Println("Full Keyframe!")
		// 			// fmt.Printf("%#v", keyframeInfo)
		// 		}
		// 	}
		// }

		// if h264.IsKeyframePart(p.Payload) {
		// 	s.keyframePackets = append(s.keyframePackets, p)

		// 	if p.Header.Marker {
		// 		out := h264.AppendNalUnitsForLibav(s.keyframePackets)
		// 		fmt.Println(len(out))
		// 		saveImage(int(p.SequenceNumber), out)
		// 		// s.lastKeyframe = out
		// 		s.keyframePackets = make([]*rtp.Packet, 0)
		// 	}
		// }

		// // Keyframes can come in parts, we need to add all keyframe parts to a list
		// // and then when we get a packet with the market bit set, that's the final
		// // piece of the frame, and we can update lastKeyframe
		// // if h264.IsKeyframePart(p.Payload) {
		// // fmt.Println(p.SequenceNumber)
		// // Marker is the beginning of a keyframe?
		// if p.Marker {
		// 	fmt.Printf("Marker: %s", p)
		// 	// pktnalus, _ := h264joy.SplitNALUs(s.keyframeParts)
		// 	// data := h264joy.JoinNALUsAnnexb(pktnalus)
		// 	// s.lastKeyframe = s.keyframeParts
		// 	// s.keyframeParts = make([]byte, 0)
		// 	// sample := s.videoSampler.Pop()
		// 	// if sample != nil {
		// 	// 	s.lastKeyframe = sample.Data
		// 	// }

		// 	byteReader := bytes.NewReader(s.keyframeParts)
		// 	reader, err := h264reader.NewReader(byteReader)
		// 	if err != nil {
		// 		fmt.Println(err)
		// 		continue
		// 	}
		// 	nal, err := reader.NextNAL()
		// 	if err != nil {
		// 		fmt.Println(err)
		// 		continue
		// 	}
		// 	s.lastKeyframe = nal.Data

		// 	s.keyframeParts = make([]byte, 0)
		// }

		// s.keyframeParts = append(s.keyframeParts, p.Payload...)
		// // }
	}
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
