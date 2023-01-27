package whep

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/google/uuid"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

const PC_TIMEOUT = time.Minute * 5

type WHEPConfig struct {
	// Listen address of the webserver
	Address       string
	Server        string `mapstructure:"server"`
	Https         bool
	HttpsHostname string `mapstructure:"https_hostname"`
	HttpsCert     string `mapstructure:"https_cert"`
	HttpsKey      string `mapstructure:"https_key"`
}

type WHEPServer struct {
	log     logrus.FieldLogger
	config  WHEPConfig
	control *control.Control

	peerConnectionsMutex sync.RWMutex
	peerConnections      map[string]*webrtc.PeerConnection
	debugChannels        map[string]*webrtc.DataChannel
}

func New(config WHEPConfig) *WHEPServer {
	return &WHEPServer{
		config:               config,
		peerConnectionsMutex: sync.RWMutex{},
		peerConnections:      make(map[string]*webrtc.PeerConnection),
		debugChannels:        make(map[string]*webrtc.DataChannel),
	}
}

func (s *WHEPServer) SetControl(ctrl *control.Control) {
	s.control = ctrl
}

func (s *WHEPServer) SetLogger(log logrus.FieldLogger) {
	s.log = log
}

func (s *WHEPServer) Listen(ctx context.Context) {
	s.log.Infof("Registering WHEP http endpoints")

	// Todo: Find better way of fetching this path
	streamTemplate := template.Must(template.ParseFiles("internal/outputs/whep/public/stream.html"))

	// Player (Nothing) => Endpoint (Offer) => Player (Answer)
	s.control.RegisterHandleFunc("/whep/endpoint/", func(w http.ResponseWriter, r *http.Request) {
		strChannelID := path.Base(r.URL.Path)

		w.Header().Add("Access-Control-Allow-Origin", "*")

		channelID, err := strconv.Atoi(strChannelID)
		if err != nil {
			errWrongParams(w, r)
			return
		}

		peerID := uuid.New().String()
		s.log.Infof("WHEP Negotiation: peer=%s status=started offer=none answer=none", peerID)

		ttl := time.Now().Add(PC_TIMEOUT)

		peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			s.log.Error(err)
			errCustom(w, r, "error establishing webrtc connection")
			return
		}
		peerConnection.OnConnectionStateChange(func(pcs webrtc.PeerConnectionState) {
			// Clean up our peer connection state
			// Maybe we don't really worry about the cleanup happening since its a no-op

			switch pcs {
			case webrtc.PeerConnectionStateClosed:
				s.cleanupPeerConnection(peerID)
			case webrtc.PeerConnectionStateDisconnected:
				s.cleanupPeerConnection(peerID)
			case webrtc.PeerConnectionStateFailed:
				s.cleanupPeerConnection(peerID)
			}
		})

		peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
			s.log.Debugf("Connection State has changed %s \n", connectionState.String())
		})

		// peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		// 	fmt.Printf("New DataChannel %s %d\n", d.Label(), d.ID())

		// 	// Register channel opening handling
		// 	d.OnOpen(func() {
		// 		fmt.Printf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", d.Label(), d.ID())

		// 		for range time.NewTicker(5 * time.Second).C {
		// 			message := "hi"
		// 			fmt.Printf("Sending '%s'\n", message)

		// 			// Send the message as text
		// 			sendErr := d.SendText(message)
		// 			if sendErr != nil {
		// 				panic(sendErr)
		// 			}
		// 		}
		// 	})

		// 	// Register text message handling
		// 	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		// 		fmt.Printf("Message from DataChannel '%s': '%s'\n", d.Label(), string(msg.Data))
		// 	})
		// })
		peerConnection.CreateDataChannel("debug", nil)
		peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
			d.OnOpen(func() {
				s.log.Debugf("Debug data channel '%s'-'%d' open", d.Label(), d.ID())

				s.debugChannels[peerID] = d
			})
			d.OnClose(func() {
				s.log.Debugf("Debug data channel '%s'-'%d' closed", d.Label(), d.ID())
				delete(s.debugChannels, peerID)
			})

			// Register text message handling
			d.OnMessage(func(msg webrtc.DataChannelMessage) {
				s.log.Debugf("Debug data channel message from client '%s': '%s'", d.Label(), string(msg.Data))
			})
		})

		// Importantly, the track needs to be added before the offer (duh!)
		tracks, err := s.control.GetTracks(control.ChannelID(channelID))
		if err != nil {
			errNotFound(w, r)
			return
		}
		for _, track := range tracks {
			rtpSender, _ := peerConnection.AddTrack(track.Track)
			go func() {
				// _ := s.log.WithField("peer", peerID)
				for {
					rtcpPackets, _, rtcpErr := rtpSender.ReadRTCP()
					if rtcpErr != nil {
						s.log.Error(rtcpErr)
						return
					}

					debugChannel, ok := s.debugChannels[peerID]
					if !ok {
						continue
					}

					for _, r := range rtcpPackets {
						// Print a string description of the packets
						switch report := r.(type) {
						case *rtcp.ReceiverReport:
							// report := r.(*rtcp.ReceiverReport)
							// var out string
							// for _, i := range report.Reports {
							// 	out += fmt.Sprintf("\t%x\t%d/%d\t%d\n", i.SSRC, i.FractionLost, i.TotalLost, i.LastSequenceNumber)
							// }
							// peerLog.Debugf(out)
							debugChannel.SendText(report.String())
							// peerLog.Debug(report.String())
						case *rtcp.ReceiverEstimatedMaximumBitrate:
							err := debugChannel.SendText(report.String())
							if err != nil {
								s.log.Error(err)
							}
						default:

							if stringer, canString := r.(fmt.Stringer); canString {
								debugChannel.SendText(fmt.Sprintf("Unknown Received RTCP Packet: %v", stringer.String()))
								// peerLog.Debugf("Unknown Received RTCP Packet: %v", stringer.String())
							}
						}
					}
				}
			}()
		}

		s.addPeerConnection(peerID, peerConnection)
		s.startPeerConnectionTimeout(peerID)

		// Used for SDP offer generated by the WHEP endpoint
		offer, err := peerConnection.CreateOffer(nil)
		if err != nil {
			s.log.Error(err)
			errCustom(w, r, "error creating offer")
			return
		}
		gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
		if err = peerConnection.SetLocalDescription(offer); err != nil {
			s.log.Error(err)
			errCustom(w, r, "error setting local description")
			return
		}
		<-gatherComplete

		localDescription := peerConnection.LocalDescription()
		s.log.Infof("WHEP Negotiation: peer=%s status=negotiating offer=created answer=none", peerID)

		w.Header().Add("Access-Control-Expose-Headers", "location, expire")
		w.Header().Add("Content-Type", "application/sdp")
		// Since Load Balancing happens only at the RTRouter, this is just responsible for
		// sending the user to the resource on this server
		w.Header().Add("Location", s.resourceUrl(peerID))
		w.Header().Add("Expire", ttl.Format(http.TimeFormat))
		w.WriteHeader(http.StatusCreated)

		fmt.Fprint(w, string(localDescription.SDP))
	})

	// Player (Nothing) => Endpoint (Offer) => Player (Answer)
	// This function actually finishes the SDP handshake
	// After this the WebRTC connection should be established
	s.control.RegisterHandleFunc("/whep/resource/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Add("Access-Control-Allow-Methods", "PATCH")
			w.Header().Add("Allow", "PATCH")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		unsafePcID := path.Base(r.URL.Path)

		body, err := io.ReadAll(r.Body)
		if unsafePcID == "" || err != nil {
			s.log.Info("Got in here", unsafePcID, body)
			errWrongParams(w, r)
			return
		}
		// Check for lookupPc in peerConnections
		s.log.Infof("WHEP Negotiation: peer=%s status=negotiating offer=accepted answer=created", unsafePcID)

		answer := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: string(body)}
		pc, ok := s.getPeerConnection(unsafePcID)
		if !ok {
			errCustom(w, r, "Unexpected error fetching peer connection")
			return
		}

		if err = pc.SetRemoteDescription(answer); err != nil {
			s.log.Error(err)
			errCustom(w, r, "error setting remote description")

			s.cleanupPeerConnection(unsafePcID)

			return
		}

		s.log.Infof("WHEP Negotiation: peer=%s status=negotiated offer=accepted answer=accepted", unsafePcID)

		w.Header().Add("Content-Type", "application/sdp")

		w.WriteHeader(http.StatusNoContent)

		fmt.Fprintf(w, "")
	})

	s.control.RegisterHandleFunc("/stream/", func(w http.ResponseWriter, r *http.Request) {
		channelID := path.Base(r.URL.Path)
		data := struct {
			ChannelID   string
			EndpointUrl template.HTML
		}{ChannelID: channelID, EndpointUrl: template.HTML(s.endpointUrl(channelID))}

		streamTemplate.Execute(w, data)
	})
}

func (s *WHEPServer) addPeerConnection(uuid string, pc *webrtc.PeerConnection) {
	s.peerConnectionsMutex.Lock()
	defer s.peerConnectionsMutex.Unlock()

	s.peerConnections[uuid] = pc
}
func (s *WHEPServer) getPeerConnection(uuid string) (*webrtc.PeerConnection, bool) {
	s.peerConnectionsMutex.RLock()
	defer s.peerConnectionsMutex.RUnlock()

	val, ok := s.peerConnections[uuid]
	return val, ok
}
func (s *WHEPServer) startPeerConnectionTimeout(uuid string) {
	go func() {
		time.Sleep(PC_TIMEOUT)

		pc, ok := s.getPeerConnection(uuid)
		if ok && pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
			s.log.Infof("Peer %s took too long to connect, rejecting peer.", uuid)
			s.cleanupPeerConnection(uuid)
		}
	}()
}
func (s *WHEPServer) cleanupPeerConnection(uuid string) {
	s.peerConnectionsMutex.Lock()
	defer s.peerConnectionsMutex.Unlock()

	if pc, ok := s.peerConnections[uuid]; ok {
		pc.Close()
	}

	delete(s.peerConnections, uuid)
}

func (s *WHEPServer) endpointUrl(channelID string) string {
	return fmt.Sprintf("%s/whep/endpoint/%s", s.control.HttpServerUrl(), channelID)
}
func (s *WHEPServer) resourceUrl(uuid string) string {
	return fmt.Sprintf("%s/whep/resource/%s", s.control.HttpServerUrl(), uuid)
}

func logRequest(log logrus.FieldLogger, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL)
		handler.ServeHTTP(w, r)
	})
}

func errCustom(w http.ResponseWriter, r *http.Request, message string) {
	w.WriteHeader(http.StatusBadRequest)
	w.Header().Set("Content-Type", "plain/text")
	w.Write([]byte(message))
}
func errWrongParams(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusBadRequest)
	w.Header().Set("Content-Type", "plain/text")
	w.Write([]byte("Invalid Parameters"))
}
func errNotFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	w.Header().Set("Content-Type", "plain/text")
	w.Write([]byte("Not found"))
}
