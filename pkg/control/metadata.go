package control

type Metadata func(*Stream)

func AudioPacketsMetadata(packets int) Metadata {
	return func(s *Stream) {
		s.audioPackets += packets
	}
}

func VideoPacketsMetadata(packets int) Metadata {
	return func(s *Stream) {
		s.videoPackets += packets
	}
}

func ClientVendorNameMetadata(name string) Metadata {
	return func(s *Stream) {
		s.clientVendorName = name
	}
}

func ClientVendorVersionMetadata(version string) Metadata {
	return func(s *Stream) {
		s.clientVendorVersion = version
	}
}

func AudioCodecMetadata(codec string) Metadata {
	return func(s *Stream) {
		s.audioCodec = codec
	}
}

func VideoCodecMetadata(codec string) Metadata {
	return func(s *Stream) {
		s.videoCodec = codec
	}
}
func VideoHeightMetadata(height int) Metadata {
	return func(s *Stream) {
		s.videoHeight = height
	}
}
func VideoWidthMetadata(width int) Metadata {
	return func(s *Stream) {
		s.videoWidth = width
	}
}
