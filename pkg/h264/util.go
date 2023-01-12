package h264

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
