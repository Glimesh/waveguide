package whep

import (
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/acme/autocert"
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

	peerConnections map[string]*webrtc.PeerConnection
}

func New(config WHEPConfig) *WHEPServer {
	return &WHEPServer{
		config:          config,
		peerConnections: make(map[string]*webrtc.PeerConnection),
	}
}

func (s *WHEPServer) SetControl(ctrl *control.Control) {
	s.control = ctrl
}

func (s *WHEPServer) SetLogger(log logrus.FieldLogger) {
	s.log = log
}

func (s *WHEPServer) Listen(ctx context.Context) {
	s.log.Infof("Starting WHEP Server on %s", s.config.Address)

	mux := http.NewServeMux()

	// Todo: Find better way of fetching this path
	streamTemplate := template.Must(template.ParseFiles("internal/outputs/whep/public/stream.html"))

	// Player (Nothing) => Endpoint (Offer) => Player (Answer)
	mux.HandleFunc("/whep/endpoint/", func(w http.ResponseWriter, r *http.Request) {
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

		peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
			ICEServers: []webrtc.ICEServer{
				{
					// URLs: []string{"stun:stun.l.google.com:19302"},
				},
			},
		})
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

		// Importantly, the track needs to be added before the offer (duh!)
		tracks, err := s.control.GetTracks(control.ChannelID(channelID))
		if err != nil {
			errNotFound(w, r)
			return
		}
		for _, track := range tracks {
			peerConnection.AddTrack(track.Track)
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
	mux.HandleFunc("/whep/resource/", func(w http.ResponseWriter, r *http.Request) {
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
		pc := s.getPeerConnection(unsafePcID)

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

	mux.HandleFunc("/stream/", func(w http.ResponseWriter, r *http.Request) {
		channelID := path.Base(r.URL.Path)
		data := struct {
			ChannelID   string
			EndpointUrl template.HTML
		}{ChannelID: channelID, EndpointUrl: template.HTML(s.endpointUrl(channelID))}

		streamTemplate.Execute(w, data)
	})

	switch s.config.Server {
	case "acme":
		s.log.Fatal(http.Serve(autocert.NewListener(s.config.HttpsHostname), logRequest(s.log, mux)))
	case "https":
		s.log.Fatal(httpsServer(s.config.Address, s.config.HttpsCert, s.config.HttpsKey, s.log, mux))
	case "http":
		s.log.Fatal(httpServer(s.config.Address, s.log, mux))
	default:
		s.log.Fatalf("unknown WHEP server option %s", s.config.Server)
	}
}

func httpServer(address string, log logrus.FieldLogger, mux *http.ServeMux) error {
	srv := &http.Server{
		Addr:    address,
		Handler: logRequest(log, mux),
	}
	return srv.ListenAndServe()
}
func httpsServer(address, cert, key string, log logrus.FieldLogger, mux *http.ServeMux) error {
	cfg := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
	}
	srv := &http.Server{
		Addr:         address,
		Handler:      logRequest(log, mux),
		TLSConfig:    cfg,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
	}
	return srv.ListenAndServeTLS(cert, key)
}

func (s *WHEPServer) addPeerConnection(uuid string, pc *webrtc.PeerConnection) {
	s.peerConnections[uuid] = pc
}
func (s *WHEPServer) getPeerConnection(uuid string) *webrtc.PeerConnection {
	return s.peerConnections[uuid]
}
func (s *WHEPServer) startPeerConnectionTimeout(uuid string) {
	go func() {
		time.Sleep(PC_TIMEOUT)

		pc, ok := s.peerConnections[uuid]
		if ok && pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
			s.log.Infof("Peer %s took too long to connect, rejecting peer.", uuid)
			s.cleanupPeerConnection(uuid)
		}
	}()
}
func (s *WHEPServer) cleanupPeerConnection(uuid string) {
	if pc, ok := s.peerConnections[uuid]; ok {
		pc.Close()
	}

	delete(s.peerConnections, uuid)
}

func (s *WHEPServer) serverUrl() string {
	var protocol string
	var host string
	if s.config.Https {
		protocol = "https"
		host = s.config.HttpsHostname
	} else {
		protocol = "http"
		host = s.config.Address
	}

	return fmt.Sprintf("%s://%s", protocol, host)
}
func (s *WHEPServer) endpointUrl(channelID string) string {
	return fmt.Sprintf("%s/whep/endpoint/%s", s.serverUrl(), channelID)
}
func (s *WHEPServer) resourceUrl(uuid string) string {
	return fmt.Sprintf("%s/whep/resource/%s", s.serverUrl(), uuid)
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
