package h264

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

func IsAnyKeyframe(data []byte) bool {
	const (
		typeSTAPA       = 24
		typeSPS         = 7
		naluTypeBitmask = 0x1F
	)

	var word uint32

	payload := bytes.NewReader(data)
	if err := binary.Read(payload, binary.BigEndian, &word); err != nil {
		return false
	}

	naluType := (word >> 24) & naluTypeBitmask
	if naluType == typeSTAPA && word&naluTypeBitmask == typeSPS {
		return true
	} else if naluType == typeSPS {
		return true
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

func WhichKeyframePart(packetPayload []byte) string {
	// Thank you Hayden :)
	// Is this packet part of a keyframe?
	if len(packetPayload) < 2 {
		return "too small"
	}

	nalType := packetPayload[0] & 0b00011111
	// nalType 7 = Sequence Parameter Set / nalType 8 = Picture Parameter Set
	// nalType 5 = IDR
	// nalType 28 = Fragmentation unit (FU-A)
	// nalType 29 = Fragmentation unit (FU-A)
	switch nalType {
	case 7:
		return "7 - Sequence Parameter Set"
	case 8:
		return "8 - Picture Parameter Set"
	case 5:
		return "5 - IDR In one packet"
	case 28:
		fragmentType := packetPayload[1] & 0b00011111
		// fragmentType 7 = Fragment of SPS
		// fragmentType 5 = Fragment of IDR
		if fragmentType == 7 {
			return fmt.Sprintf("%d - %s", nalType, "7 - Fragment of SPS")
		} else if fragmentType == 5 {
			return fmt.Sprintf("%d - %s", nalType, "5 - Fragment of IDR")
		} else {
			return fmt.Sprintf("%d - %d - %s ", nalType, fragmentType, "Unknown Type")
		}
	case 29:
		fragmentType := packetPayload[1] & 0b00011111
		// fragmentType 7 = Fragment of SPS
		// fragmentType 5 = Fragment of IDR
		if fragmentType == 7 {
			return fmt.Sprintf("%d - %s", nalType, "7 - Fragment of SPS")
		} else if fragmentType == 5 {
			return fmt.Sprintf("%d - %s", nalType, "5 - Fragment of IDR")
		} else {
			return fmt.Sprintf("%d - %d - %s ", nalType, fragmentType, "Unknown Type")
		}
	}

	fmt.Printf("%s", hex.Dump(packetPayload))

	return fmt.Sprintf("%d - %s ", nalType, "Unknown Type")
}
