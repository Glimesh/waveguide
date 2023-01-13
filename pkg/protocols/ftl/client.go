package ftl

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

type Conn struct {
	AssignedMediaPort int

	channelId ChannelID

	controlConn      net.Conn
	controlConnected bool
	controlScanner   *bufio.Reader
	MediaAddr        *net.UDPAddr
	MediaConn        *net.UDPConn

	failedHeartbeats int
	quitTimer        chan bool
}

func Dial(targetHostname string, ftlPort int, channelID ChannelID, streamKey []byte) (conn *Conn, err error) {
	addr := fmt.Sprintf("%s:%d", targetHostname, ftlPort)
	tcpConn, err := net.Dial("tcp", addr)
	if err != nil {
		return &Conn{}, err
	}

	scanner := bufio.NewReader(tcpConn)
	conn = &Conn{
		controlConn:    tcpConn,
		controlScanner: scanner,
		MediaConn:      nil,
		channelId:      channelID,
		quitTimer:      make(chan bool, 1),
	}

	if err = conn.sendAuthentication(channelID, streamKey); err != nil {
		conn.close()
		return conn, err
	}
	time.Sleep(time.Second)
	if err = conn.sendMetadataBatch(); err != nil {
		conn.close()
		return conn, err
	}
	time.Sleep(time.Second)
	if err = conn.sendMediaStart(); err != nil {
		conn.close()
		return conn, err
	}

	mediaAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", targetHostname, conn.AssignedMediaPort))
	if err != nil {
		return conn, err
	}
	conn.MediaAddr = mediaAddr
	conn.MediaConn, err = net.DialUDP("udp", nil, mediaAddr)
	if err != nil {
		return conn, err
	}

	return conn, err
}

func (conn *Conn) Close() error {
	// Stop heartbeat
	conn.quitTimer <- true

	if !conn.controlConnected {
		return nil
	}

	return conn.close()
}

func (conn *Conn) close() error {
	conn.controlConnected = false

	// Since the server likely closed our connection already, don't wait long
	conn.controlConn.SetDeadline(time.Now().Add(time.Second * 1))
	conn.sendControlMessage(requestDisconnect, false)
	conn.controlConn.Close()

	if conn.MediaConn != nil {
		conn.MediaConn.Close()
	}

	return nil
}

func (conn *Conn) sendAuthentication(channelID ChannelID, streamKey []byte) (err error) {
	resp, err := conn.sendControlMessage(requestHmac, true)
	if err != nil {
		return err
	}
	split := strings.Split(resp, " ")

	hmacHexString := split[1]
	decoded, err := hex.DecodeString(hmacHexString)
	if err != nil {
		return err
	}

	hash := hmac.New(sha512.New, streamKey)
	hash.Write(decoded)

	hmacPayload := hash.Sum(nil)

	resp, err = conn.sendControlMessage(fmt.Sprintf(requestConnect, channelID, hex.EncodeToString(hmacPayload)), true)
	if err := checkFtlResponse(resp, err, responseOk); err != nil {
		return err
	}

	return nil
}

func (conn *Conn) sendMetadataBatch() error {
	// fake for now
	attrs := []string{
		// Generic
		fmt.Sprintf(metaProtocolVersion, VersionMajor, VersionMinor),
		fmt.Sprintf(metaVendorName, "rtmp-ingest"),
		fmt.Sprintf(metaVendorVersion, "1.0"),
		// Video
		fmt.Sprintf(metaVideo, "true"),
		fmt.Sprintf(metaVideoCodec, "H264"),
		fmt.Sprintf(metaVideoHeight, 720),
		fmt.Sprintf(metaVideoWidth, 1280),
		fmt.Sprintf(metaVideoPayloadType, 96),
		fmt.Sprintf(metaVideoIngestSSRC, conn.channelId+1),
		// Audio
		fmt.Sprintf(metaAudio, "true"),
		fmt.Sprintf(metaAudioCodec, "OPUS"), // This is a lie, its AAC
		fmt.Sprintf(metaAudioPayloadType, 97),
		fmt.Sprintf(metaAudioIngestSSRC, conn.channelId),
	}
	for _, v := range attrs {
		_, err := conn.sendControlMessage(v, false)
		if err != nil {
			return err
		}
	}

	return nil
}

func (conn *Conn) sendMediaStart() (err error) {
	resp, err := conn.sendControlMessage(requestDot, true)
	if err != nil {
		return err
	}

	matches := clientMediaPortRegex.FindAllStringSubmatch(resp, 1)
	if len(matches) < 1 {
		return err
	}
	conn.AssignedMediaPort, err = strconv.Atoi(matches[0][1])
	if err != nil {
		return err
	}

	return nil
}
func (conn *Conn) Heartbeat() error {
	ticker := time.NewTicker(5 * time.Second)

	for {
		select {
		case <-ticker.C:
			resp, err := conn.sendControlMessage(requestPing, true)

			// Todo: Move these to the response handler
			switch resp {
			case responseServerTerminate:
				conn.close()
				return errors.New("got server termination (410) response, ending immediately")
			case responseInvalidStreamKey:
				conn.close()
				return errors.New("got invalid stream key (405) response, ending immediately")
			case responseInternalServerError:
				conn.close()
				return errors.New("got internal server error (500) response, ending immediately")
			default:
				if err := checkFtlResponse(resp, err, responsePong); err != nil {
					conn.failedHeartbeats += 1
					if conn.failedHeartbeats >= allowedHeartbeatFailures {
						conn.close()
						return err
					}
				} else {
					conn.failedHeartbeats = 0
				}
			}
		case <-conn.quitTimer:
			ticker.Stop()
			return nil
		}
	}
}

func (conn *Conn) sendControlMessage(message string, needResponse bool) (resp string, err error) {
	err = conn.writeControlMessage(message)
	if err != nil {
		return "", err
	}

	if needResponse {
		return conn.readControlMessage()
	}

	return "", nil
}

func (conn *Conn) writeControlMessage(message string) error {
	final := message + "\r\n\r\n"
	log.Printf("FTL SEND: %q", final)
	_, err := conn.controlConn.Write([]byte(final))
	return err
}

// readControlMessage forces a read, and closes the connection if it doesn't get what it wants
func (conn *Conn) readControlMessage() (string, error) {
	// Give the server 5 seconds to respond to our read request
	conn.controlConn.SetReadDeadline(time.Now().Add(time.Second * 5))

	recv, err := conn.controlScanner.ReadString('\n')
	if err != nil {
		return "", err
	}

	log.Printf("FTL RECV: %q", recv)
	return strings.TrimRight(recv, "\n"), nil
}

func checkFtlResponse(resp string, err error, expected string) error {
	if err != nil {
		return err
	}
	if resp != expected {
		return errors.New("unexpected reply from server: " + resp)
	}
	return nil
}
