package control

type Stream struct {
	// authenticated is set after the stream has successfully authed with a remote service
	authenticated bool
	// mediaStarted is set after media bytes have come in from the client
	mediaStarted bool

	ChannelID ChannelID
	StreamID  StreamID
	StreamKey StreamKey
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
