package ftl

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/nack"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	// This packet size is ideal for super fast video, but the theory is there's
	// going to be numerous problems with networking equipment not accepting the
	// packet size, especially for UDP.
	//packetMtu = 2048 // 67 ms -- 32 frames on 240 video -- 213ms on clock
	// I'm guessing for these two, the packet differences are not great enough to
	// overcome the 30/60 fps most people stream at. So you see no difference.
	//packetMtu = 1600 // 100 ms -- 30 frames on 240 video -- UDP MTU allegedly
	//packetMtu = 1500 // 100 ms gtg latency - 144ms on clock
	//packetMtu = 1460 // UDP MTU
	// packetMtu = 1392
	// packetMtu = 1600
	// FTL-SDK recommends 1392 MTU
	packetMtu = 1392

	MaxLineLenBytes  = 1024
	ReadWriteTimeout = time.Minute

	FTL_PAYLOAD_TYPE_SENDER_REPORT = 200
	FTL_PAYLOAD_TYPE_PING          = 250
)

type ConnConfig struct {
	Handler Handler
}

type Handler interface {
	GetHmacKey() (string, error)

	OnConnect(ChannelID) error
	OnPlay(FtlConnectionMetadata) error
	OnVideo(*rtp.Packet) error
	OnAudio(*rtp.Packet) error
	OnClose()
}

type ServerConfig struct {
	Log logrus.FieldLogger
	// OnNewConnect is triggered on any connect to the FTL port, however it's not a
	// qualified FTL client until Handler.OnConnect is called.
	OnNewConnect func(net.Conn) (net.Conn, *ConnConfig)
}

func NewServer(config *ServerConfig) *Server {
	return &Server{
		config: config,
		log:    config.Log,
	}
}

type Server struct {
	config *ServerConfig
	log    logrus.FieldLogger

	listener net.Listener
}

func (srv *Server) Serve(listener net.Listener) error {
	srv.listener = listener

	for {
		// Each client
		socket, err := listener.Accept()
		if err != nil {
			srv.log.Error(err)
			continue
		}

		conn, clientConfig := srv.config.OnNewConnect(socket)

		ftlConn := FtlConnection{
			log:            srv.log,
			transport:      conn,
			handler:        clientConfig.Handler,
			connected:      true,
			mediaConnected: false,
			Metadata:       &FtlConnectionMetadata{},
		}

		go func() {
			lim := &io.LimitedReader{
				R: ftlConn.transport,
				N: MaxLineLenBytes,
			}

			scanner := bufio.NewScanner(lim)
			scanner.Split(scanCRLF)

			_ = ftlConn.transport.SetReadDeadline(time.Now().Add(ReadWriteTimeout))

			for scanner.Scan() {
				// A previous read could have disconnected us already
				if !ftlConn.connected {
					return
				}

				payload := scanner.Text()
				if payload == "" {
					continue
				}

				if err := ftlConn.ProcessCommand(payload); err != nil {
					ftlConn.log.Error(err)
					ftlConn.Close()
					return
				}

				// reset the number of bytes remaining in the LimitReader
				lim.N = MaxLineLenBytes
				// reset the read deadline
				_ = conn.SetReadDeadline(time.Now().Add(ReadWriteTimeout))
			}
			if err := scanner.Err(); err != nil {
				ftlConn.log.Errorf("Invalid input: %s", err)
				ftlConn.Close()
				return
			}
		}()
	}
}

type FtlConnection struct {
	log logrus.FieldLogger

	transport      net.Conn
	mediaTransport *net.UDPConn
	connected      bool
	mediaConnected bool

	handler Handler

	// Unique Channel ID
	channelID int
	//streamKey         string
	assignedMediaPort int

	// Pre-calculated hash we expect the client to return
	hmacPayload []byte
	// Hash the client has actually returned
	clientHmacHash []byte

	hasAuthenticated bool
	hmacRequested    bool

	Metadata *FtlConnectionMetadata
}

type FtlConnectionMetadata struct {
	ProtocolVersion string
	VendorName      string
	VendorVersion   string

	HasVideo         bool
	VideoCodec       string
	VideoHeight      uint
	VideoWidth       uint
	VideoPayloadType uint8
	VideoIngestSsrc  uint

	HasAudio         bool
	AudioCodec       string
	AudioPayloadType uint8
	AudioIngestSsrc  uint
}

func (conn *FtlConnection) SendMessage(message string) error {
	message = message + "\n"
	conn.log.Debugf("FTL SEND: %s", message)
	_, err := conn.transport.Write([]byte(message))
	return err
}

func (conn *FtlConnection) Close() error {
	err := conn.transport.Close()
	conn.connected = false

	if conn.mediaConnected {
		conn.mediaTransport.Close()
		conn.mediaConnected = false
	}

	conn.handler.OnClose()

	return err
}

func (conn *FtlConnection) ProcessCommand(command string) error {
	conn.log.Debugf("FTL RECV: %s", command)
	if command == "HMAC" {
		return conn.processHmacCommand()
	} else if strings.Contains(command, "DISCONNECT") {
		return conn.processDisconnectCommand(command)
	} else if strings.Contains(command, "CONNECT") {
		return conn.processConnectCommand(command)
	} else if strings.Contains(command, "PING") {
		return conn.processPingCommand()
	} else if attributeRegex.MatchString(command) {
		return conn.processAttributeCommand(command)
	} else if command == "." {
		return conn.processDotCommand()
	} else if command == "\n" {
		// Unsure where this comes from exactly, but ignore it
		return nil
	} else {
		conn.log.Warnf("Unknown ingest command: %s", command)
	}
	return nil
}

func (conn *FtlConnection) processHmacCommand() error {
	conn.hmacPayload = make([]byte, hmacPayloadSize)
	rand.Read(conn.hmacPayload)

	encodedPayload := hex.EncodeToString(conn.hmacPayload)

	return conn.SendMessage(fmt.Sprintf(responseHmacPayload, encodedPayload))
}

func (conn *FtlConnection) processDisconnectCommand(message string) error {
	conn.log.Println("Got Disconnect command, closing stuff.")

	return conn.Close()
}

func (conn *FtlConnection) processConnectCommand(message string) error {
	if conn.hmacRequested {
		return ErrMultipleConnect
	}

	conn.hmacRequested = true

	matches := connectRegex.FindAllStringSubmatch(message, 3)
	if len(matches) < 1 {
		return ErrUnexpectedArguments
	}
	args := matches[0]
	if len(args) < 3 {
		// malformed connection string
		return ErrUnexpectedArguments
	}

	channelIdStr := args[1]
	hmacHashStr := args[2]

	channelId, err := strconv.Atoi(channelIdStr)
	if err != nil {
		return ErrUnexpectedArguments
	}

	conn.channelID = channelId

	if err := conn.handler.OnConnect(ChannelID(conn.channelID)); err != nil {
		return err
	}

	hmacKey, err := conn.handler.GetHmacKey()
	if err != nil {
		return err
	}

	hash := hmac.New(sha512.New, []byte(hmacKey))
	hash.Write(conn.hmacPayload)
	conn.hmacPayload = hash.Sum(nil)

	hmacBytes, err := hex.DecodeString(hmacHashStr)
	if err != nil {
		return ErrInvalidHmacHex
	}

	conn.hasAuthenticated = true
	conn.clientHmacHash = hmacBytes

	if !hmac.Equal(conn.clientHmacHash, conn.hmacPayload) {
		return ErrInvalidHmacHash
	}

	return conn.SendMessage(responseOk)
}

func (conn *FtlConnection) processAttributeCommand(message string) error {
	if !conn.hasAuthenticated {
		return ErrConnectBeforeAuth
	}

	matches := attributeRegex.FindAllStringSubmatch(message, 3)
	if len(matches) < 1 || len(matches[0]) < 3 {
		return ErrUnexpectedArguments
	}
	key, value := matches[0][1], matches[0][2]

	switch key {
	case "ProtocolVersion":
		conn.Metadata.ProtocolVersion = value
	case "VendorName":
		conn.Metadata.VendorName = value
	case "VendorVersion":
		conn.Metadata.VendorVersion = value
	// Video
	case "Video":
		conn.Metadata.HasVideo = parseAttributeToBool(value)
	case "VideoCodec":
		conn.Metadata.VideoCodec = value
	case "VideoHeight":
		conn.Metadata.VideoHeight = parseAttributeToUint(value)
	case "VideoWidth":
		conn.Metadata.VideoWidth = parseAttributeToUint(value)
	case "VideoPayloadType":
		conn.Metadata.VideoPayloadType = parseAttributeToUint8(value)
	case "VideoIngestSSRC":
		conn.Metadata.VideoIngestSsrc = parseAttributeToUint(value)
	// Audio
	case "Audio":
		conn.Metadata.HasAudio = parseAttributeToBool(value)
	case "AudioCodec":
		conn.Metadata.AudioCodec = value
	case "AudioPayloadType":
		conn.Metadata.AudioPayloadType = parseAttributeToUint8(value)
	case "AudioIngestSSRC":
		conn.Metadata.AudioIngestSsrc = parseAttributeToUint(value)
	default:
		conn.log.Infof("Unexpected Attribute: %q", message)
	}

	return nil
}

func parseAttributeToUint(input string) uint {
	u, _ := strconv.ParseUint(input, 10, 32)
	return uint(u)
}
func parseAttributeToUint8(input string) uint8 {
	u, _ := strconv.ParseUint(input, 10, 32)
	return uint8(u)
}
func parseAttributeToBool(input string) bool {
	return input == "true"
}

func (conn *FtlConnection) processDotCommand() error {
	if !conn.hasAuthenticated {
		return ErrConnectBeforeAuth
	}

	err := conn.listenForMedia()
	if err != nil {
		return err
	}

	// Push it to a clients map so we can reference it later
	if err := conn.handler.OnPlay(*conn.Metadata); err != nil {
		return err
	}

	return conn.SendMessage(fmt.Sprintf(responseMediaPort, conn.assignedMediaPort))
}

func (conn *FtlConnection) processPingCommand() error {
	return conn.SendMessage(responsePong)
}

func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[0 : len(data)-1]
	}
	return data
}

func scanCRLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.Index(data, []byte{'\r', '\n'}); i >= 0 {
		// We have a full newline-terminated line.
		return i + 2, dropCR(data[0:i]), nil
	}
	// If we're at EOF, we have a final, non-terminated line. Return it.
	if atEOF {
		return len(data), dropCR(data), nil
	}
	// Request more data.
	return 0, nil, nil
}

func (conn *FtlConnection) listenForMedia() error {
	udpAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		return err
	}
	mediaConn, mediaErr := net.ListenUDP("udp", udpAddr)
	if mediaErr != nil {
		return err
	}

	conn.assignedMediaPort = mediaConn.LocalAddr().(*net.UDPAddr).Port
	conn.mediaTransport = mediaConn
	conn.mediaConnected = true

	conn.log.Infof("Listening for UDP connections on: %d", conn.assignedMediaPort)

	// Create NACK Generator
	generatorFactory, err := nack.NewGeneratorInterceptor()
	if err != nil {
		return err
	}

	generator, err := generatorFactory.NewInterceptor("")
	if err != nil {
		return err
	}

	// Create our interceptor chain with just a NACK Generator
	chain := interceptor.NewChain([]interceptor.Interceptor{generator})

	// Create the writer just for a single SSRC stream
	// this is a callback that is fired everytime a RTP packet is ready to be sent
	streamReader := chain.BindRemoteStream(&interceptor.StreamInfo{
		SSRC:         uint32(conn.Metadata.VideoIngestSsrc),
		RTCPFeedback: []interceptor.RTCPFeedback{{Type: "nack", Parameter: ""}},
	}, interceptor.RTPReaderFunc(func(b []byte, _ interceptor.Attributes) (int, interceptor.Attributes, error) { return len(b), nil, nil }))

	go func() {
		defer func() {
			conn.log.Debug("Cleaning up FTL NACK handler & connection")
			chain.Close()
			generator.Close()
			conn.Close()
		}()

		for rtcpBound, buffer := false, make([]byte, 1500); ; {
			if !conn.mediaConnected {
				return
			}

			n, addr, err := mediaConn.ReadFrom(buffer)
			if err != nil {
				conn.log.Error(errors.Wrap(ErrRead, err.Error()))
				return
			}

			packet := &rtp.Packet{}
			buf := buffer[:n]
			if err = packet.Unmarshal(buf); err != nil {
				// Seems like we encounter situations from OBS where they send us RTP packets without payload.
				// The PayloadType is 122 and you can find examples here: https://go.dev/play/p/H7MLbVeCbMI
				continue
			}

			// The FTL client actually tells us what PayloadType to use for these: VideoPayloadType & AudioPayloadType
			if packet.Header.PayloadType == conn.Metadata.VideoPayloadType {
				if err := conn.handler.OnVideo(packet); err != nil {
					conn.log.Error(errors.Wrap(ErrWrite, err.Error()))
					return
				}

				// Only read video packets into our nack
				if _, _, err := streamReader.Read(buf, nil); err != nil {
					conn.log.Error(err)
					return
				}
			} else if packet.Header.PayloadType == conn.Metadata.AudioPayloadType {
				if err := conn.handler.OnAudio(packet); err != nil {
					conn.log.Error(errors.Wrap(ErrWrite, err.Error()))
					return
				}
			} else {
				// FTL implementation uses the marker bit space for payload types above 127
				// when the payload type is not audio or video. So we need to reconstruct it.
				marker := buf[1] >> 7 & 0x1
				payloadType := marker<<7 | packet.PayloadType

				if payloadType == FTL_PAYLOAD_TYPE_PING {
					// FTL client is trying to measure round trip time (RTT), pong back the same packet
					conn.mediaTransport.WriteTo(buf, addr)
					// conn.log.Infof("Got raw ping of %d size!", len(buf))
				} else if payloadType == FTL_PAYLOAD_TYPE_SENDER_REPORT {
					// We expect this packet to be 28 bytes big.
					if len(buf) != 28 {
						conn.log.Warn("FTL: Invalid sender report packet of length %d (expect 28)", len(buf))
					}
					// char* packet = reinterpret_cast<char*>(rtpHeader);
					// uint32_t ssrc              = ntohl(*reinterpret_cast<uint32_t*>(packet + 4));
					// uint32_t ntpTimestampHigh  = ntohl(*reinterpret_cast<uint32_t*>(packet + 8));
					// uint32_t ntpTimestampLow   = ntohl(*reinterpret_cast<uint32_t*>(packet + 12));
					// uint32_t rtpTimestamp      = ntohl(*reinterpret_cast<uint32_t*>(packet + 16));
					// uint32_t senderPacketCount = ntohl(*reinterpret_cast<uint32_t*>(packet + 20));
					// uint32_t senderOctetCount  = ntohl(*reinterpret_cast<uint32_t*>(packet + 24));

					// uint64_t ntpTimestamp = (static_cast<uint64_t>(ntpTimestampHigh) << 32) |
					//     static_cast<uint64_t>(ntpTimestampLow);

					// TODO: We don't do anything with this information right now, but we ought to log
					// it away somewhere.
					// conn.log.Info("Got sender report!")
				} else {
					conn.log.Info("RTP: Unknown RTP payload type %d (orig %d})\n", payloadType,
						packet.PayloadType)
				}
			}

			// Set the interceptor wide RTCP Writer
			// this is a callback that is fired everytime a RTCP packet is ready to be sent
			if !rtcpBound {
				chain.BindRTCPWriter(interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, _ interceptor.Attributes) (int, error) {
					buf, err := rtcp.Marshal(pkts)
					if err != nil {
						return 0, err
					}

					for _, r := range pkts {
						// Print a string description of the packets
						switch report := r.(type) {
						case *rtcp.TransportLayerNack:
							conn.log.Infof("RTCP: Sending NACK to SSRC=%d for Media SSRC=%d", report.SenderSSRC, report.MediaSSRC)
						default:
							if stringer, canString := r.(fmt.Stringer); canString {
								conn.log.Debugf("RTCP: Unexpected RTCP packet: %s", stringer.String())
							}
						}
					}

					return mediaConn.WriteTo(buf, addr)
				}))

				rtcpBound = true
			}
		}
	}()

	return nil
}
