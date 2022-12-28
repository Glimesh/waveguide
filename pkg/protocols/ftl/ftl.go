package ftl

import "regexp"

var (
	connectRegex         = regexp.MustCompile(`CONNECT ([0-9]+) \$([0-9a-f]+)`)
	clientMediaPortRegex = regexp.MustCompile(`200 hi\. Use UDP port (\d+)`)
	attributeRegex       = regexp.MustCompile(`(.+): (.+)`)
)

// Custom Types
type ChannelID uint32
type StreamID uint32

const (
	DefaultPort  = 8084
	VersionMajor = 0
	VersionMinor = 2

	allowedHeartbeatFailures = 5
	hmacPayloadSize          = 128

	// Client Requests
	// All client requests must be prepended with \r\n, but we can do that in the
	// sendMessage function
	requestHmac       = "HMAC"
	requestConnect    = "CONNECT %d $%s"
	requestDot        = "."
	requestPing       = "PING"
	requestDisconnect = "DISCONNECT"

	// Client Metadata
	metaProtocolVersion  = "ProtocolVersion: %d.%d"
	metaVendorName       = "VendorName: %s"
	metaVendorVersion    = "VendorVersion: %s"
	metaVideo            = "Video: %s"
	metaVideoCodec       = "VideoCodec: %s"
	metaVideoHeight      = "VideoHeight: %d"
	metaVideoWidth       = "VideoWidth: %d"
	metaVideoPayloadType = "VideoPayloadType: %d"
	metaVideoIngestSSRC  = "VideoIngestSSRC: %d"
	metaAudio            = "Audio: %s"
	metaAudioCodec       = "AudioCodec: %s"
	metaAudioPayloadType = "AudioPayloadType: %d"
	metaAudioIngestSSRC  = "AudioIngestSSRC: %d"

	// Server Responses
	// Should consider removing the new lines and stripping it out from the responses since it's a protocol default
	responseHmacPayload         = "200 %s"
	responseOk                  = "200"
	responsePong                = "201"
	responseMediaPort           = "200. Use UDP port %d"
	responseServerTerminate     = "410"
	responseInvalidStreamKey    = "405"
	responseInternalServerError = "500"
)
