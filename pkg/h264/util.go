package h264

import (
	"fmt"

	"github.com/pion/rtp"
)

func IsKeyframe(payload []byte) bool {
	if len(payload) < 1 {
		return false
	}
	nalu := payload[0] & 0x1F
	if nalu == 0 {
		// reserved
		return false
	} else if nalu <= 23 {
		// simple NALU
		return nalu == 5
	} else if nalu == 24 || nalu == 25 || nalu == 26 || nalu == 27 {
		// STAP-A, STAP-B, MTAP16 or MTAP24
		i := 1
		if nalu == 25 || nalu == 26 || nalu == 27 {
			// skip DON
			i += 2
		}
		for i < len(payload) {
			if i+2 > len(payload) {
				return false
			}
			length := uint16(payload[i])<<8 |
				uint16(payload[i+1])
			i += 2
			if i+int(length) > len(payload) {
				return false
			}
			offset := 0
			if nalu == 26 {
				offset = 3
			} else if nalu == 27 {
				offset = 4
			}
			if offset >= int(length) {
				return false
			}
			n := payload[i+offset] & 0x1F
			if n == 7 {
				return true
			} else if n >= 24 {
				// is this legal?
				fmt.Println("Non-simple NALU within a STAP")
			}
			i += int(length)
		}
		if i == len(payload) {
			return false
		}
		return false
	} else if nalu == 28 || nalu == 29 {
		// FU-A or FU-B
		if len(payload) < 2 {
			return false
		}
		if (payload[1] & 0x80) == 0 {
			// not a starting fragment
			return false
		}
		return payload[1]&0x1F == 7
	}
	return false
}

func IsKeyframePart(packetPayload []byte) bool {
	// Thank you Hayden :)
	// Is this packet part of a keyframe?
	if len(packetPayload) < 2 {
		return false
	}
	isKeyframePart := false
	nalType := packetPayload[0] & 0b00011111
	// nalType 7 = Sequence Parameter Set / nalType 8 = Picture Parameter Set
	// nalType 5 = IDR
	// nalType 28 = Fragmentation unit (FU-A)
	// nalType 29 = Fragmentation unit (FU-A)
	if (nalType == 7) || (nalType == 8) {
		// SPS often precedes an IDR (Instantaneous Decoder Refresh) aka Keyframe
		// and provides information on how to decode it. We should keep this around.
		isKeyframePart = true
	} else if nalType == 5 {
		// Managed to fit an entire IDR into one packet!
		isKeyframePart = true
	} else if nalType == 28 || nalType == 29 {

		// See https://tools.ietf.org/html/rfc3984#section-5.8
		fragmentType := packetPayload[1] & 0b00011111
		// fragmentType 7 = Fragment of SPS
		// fragmentType 5 = Fragment of IDR
		if (fragmentType == 7) || (fragmentType == 5) {
			isKeyframePart = true
		}
	}

	return isKeyframePart
}

func AppendNalUnitsForLibav(payloads []*rtp.Packet) []byte {
	keyframeDataBuffer := make([]byte, 0)

	// We need to shove all of the keyframe NAL units into a buffer to feed into libav
	for _, packet := range payloads {
		payload := packet.Payload
		if len(payload) < 2 {
			// Invalid packet payload
			continue
		}

		// Parse out H264 packet data
		fragmentType := payload[0] & 0b00011111 // 0x1F
		// uint8_t nalType      = *(payload+1) & 0b00011111; // 0x1F
		startBit := payload[1] & 0b10000000 // 0x80
		// uint8_t endBit       = *(payload+1) & 0b01000000; // 0x40

		// For fragmented types, start bits are special, they have some extra data in the NAL header
		// that we need to include.
		if fragmentType == 28 {
			if startBit > 0 {
				// Write the start code
				keyframeDataBuffer = append(keyframeDataBuffer, 0x00, 0x00, 0x01)

				// Write the re-constructed header
				firstByte := (payload[0] & 0b11100000) | (payload[1] & 0b00011111)
				keyframeDataBuffer = append(keyframeDataBuffer, firstByte)
			}

			// Write the rest of the payload
			keyframeDataBuffer = append(keyframeDataBuffer, payload[2:]...)
		} else {
			// Write the start code
			keyframeDataBuffer = append(keyframeDataBuffer, 0x00, 0x00, 0x01)

			// Write the rest of the payload
			keyframeDataBuffer = append(keyframeDataBuffer, payload...)
		}

	}

	return keyframeDataBuffer
}
