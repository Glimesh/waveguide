package control

import (
	"crypto/sha256"
	"fmt"
	"sort"

	"github.com/Glimesh/waveguide/pkg/h264"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
)

func NewKeyframer() *Keyframer {
	return &Keyframer{
		timestamp:    0,
		frameStarted: false,
		packets:      make(map[uint16][]byte),
	}
}

type Keyframer struct {
	lastFullKeyframe []byte
	frameStarted     bool
	timestamp        uint32
	packets          map[uint16][]byte
}

func (kf *Keyframer) Reset() {
	kf.timestamp = 0
	kf.frameStarted = false
	kf.packets = make(map[uint16][]byte)
}

func (kf *Keyframer) KeyFrame(p *rtp.Packet) []byte {
	// fmt.Printf("frameStarted=%t\n", kf.frameStarted)
	// Frame has started, but timestamps don't match, continue
	if kf.frameStarted && kf.timestamp != p.Timestamp {
		return nil
	}

	// Wait until we get a keyframe part with a timestmap,
	// and then use that for collecting future packets
	keyframePart := h264.IsAnyKeyframe(p.Payload)
	if !kf.frameStarted && keyframePart {
		kf.timestamp = p.Timestamp
		kf.frameStarted = true
	}
	if !kf.frameStarted {
		return nil
	}

	kf.packets[p.SequenceNumber] = p.Payload
	if p.Marker {
		// fmt.Println("Got a marker, ending keyframe")
		// We are at the end of the timestamp
		keys := make([]uint16, 0, len(kf.packets))
		for k := range kf.packets {
			keys = append(keys, k)
		}
		sort.Sort(sequenceNumSort(keys))

		codec := &codecs.H264Packet{}

		newFrame := make([]byte, 0)
		for _, k := range keys {
			// fmt.Printf("Writing seq=%d\n", k)
			packet := kf.packets[k]

			data, err := codec.Unmarshal(packet)
			if err != nil {
				fmt.Println(err)
				continue
			}

			newFrame = append(newFrame, data...)
		}

		kf.lastFullKeyframe = newFrame

		return newFrame
	}

	return nil
}

func (kf *Keyframer) Keyframe() []byte {
	return kf.lastFullKeyframe
}

func (kf Keyframer) String() string {
	payload := make([]byte, 0)
	sequences := ""
	keys := make([]uint16, 0, len(kf.packets))
	for k := range kf.packets {
		keys = append(keys, k)
	}
	sort.Sort(sequenceNumSort(keys))
	for _, seq := range keys {
		sequences += fmt.Sprintf("%d,", seq)
		payload = append(payload, kf.packets[seq]...)
	}

	sum := sha256.Sum256(payload)

	s := "Keyframe:\n"
	s += fmt.Sprintf("\tTimestamp:\t%d\n", kf.timestamp)
	s += fmt.Sprintf("\tFrameStarted:\t%t\n", kf.frameStarted)
	s += fmt.Sprintf("\tPackets:\t%d\n", len(kf.packets))
	s += fmt.Sprintf("\tBytes:\t%d\n", len(payload))
	s += fmt.Sprintf("\tHash:\t%x\n", sum)

	s += fmt.Sprintf("\tSequences:\t%s\n", sequences)
	return s
}

type sequenceNumSort []uint16

func (f sequenceNumSort) Len() int {
	return len(f)
}

func (f sequenceNumSort) Less(i, j int) bool {
	return f[i] < f[j]
}

func (f sequenceNumSort) Swap(i, j int) {
	f[i], f[j] = f[j], f[i]
}
