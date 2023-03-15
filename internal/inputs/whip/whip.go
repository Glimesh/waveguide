package whip

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/Glimesh/waveguide/pkg/types"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

const PC_TIMEOUT = time.Minute * 5

type WHIPSource struct {
	log     logrus.FieldLogger
	control *control.Control

	peerConnectionsMutex sync.RWMutex
	peerConnections      map[types.ChannelID]*webrtc.PeerConnection

	// Listen address of the FS server in the ip:port format
	Address   string
	VideoFile string `mapstructure:"video_file"`
	AudioFile string `mapstructure:"audio_file"`
}

func New(address, videoFile, audioFile string) *WHIPSource {
	return &WHIPSource{
		Address:              address,
		VideoFile:            videoFile,
		AudioFile:            audioFile,
		peerConnectionsMutex: sync.RWMutex{},
		peerConnections:      make(map[types.ChannelID]*webrtc.PeerConnection),
	}
}

func (s *WHIPSource) SetControl(ctrl *control.Control) {
	s.control = ctrl
}

func (s *WHIPSource) SetLogger(log logrus.FieldLogger) {
	s.log = log
}

func (s *WHIPSource) Listen(ctx context.Context) {
	s.log.Infof("Registering WHIP http endpoints")

	s.control.RegisterHandleFunc("/whip/endpoint/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", "*")

		// This function allows for the channel ID to be passed in via the URL /whip/endpoint/1234
		// or alternatively via the stream key 1234-somekey

		strChannelID := path.Base(r.URL.Path)
		streamKey := r.Header.Get("Authorization")
		if streamKey == "" {
			errUnauthorized(w, r)
			return
		}

		if strings.HasPrefix(streamKey, "Bearer ") {
			// Remove Bearer info, will need to research why this is being sent.
			streamKey = strings.Replace(streamKey, "Bearer ", "", 1)
		}

		split := strings.Split(streamKey, "-")
		if len(split) > 1 {
			// Filter out the Channel ID prefix from the stream key
			strChannelID = split[0]
			streamKey = split[1]
		}
		intChannelID, err := strconv.Atoi(strChannelID)
		if err != nil {
			errWrongParams(w, r)
			return
		}
		channelID := types.ChannelID(intChannelID)

		if r.Method == http.MethodDelete {
			// The client wants to end the stream
			s.cleanupPeerConnection(channelID)
			s.control.StopStream(channelID)

			w.WriteHeader(http.StatusOK)

			fmt.Fprintf(w, "")
			return
		}

		err = s.control.Authenticate(channelID, types.StreamKey(streamKey))
		if err != nil {
			errUnauthorized(w, r)
			return
		}

		offer, err := io.ReadAll(r.Body)
		if err != nil || len(offer) == 0 {
			s.log.Error(err)
			errWrongParams(w, r)
			return
		}

		stream, ctx, err := s.control.StartStream(channelID)
		if err != nil {
			s.log.Error(err)
			errCustom(w, r, "Problem starting the stream")
			return
		}

		ttl := time.Now().Add(PC_TIMEOUT)

		peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			s.log.Error(err)
			errCustom(w, r, "Problem creating the peer connection")
			return
		}

		videoTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion")
		if err != nil {
			s.log.Error(err)
			return
		}

		audioTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "pion")
		if err != nil {
			s.log.Error(err)
			return
		}

		stream.AddTrack(videoTrack, webrtc.MimeTypeH264)
		stream.AddTrack(audioTrack, webrtc.MimeTypeOpus)

		stream.ReportMetadata(
			control.AudioCodecMetadata(webrtc.MimeTypeOpus),
			control.VideoCodecMetadata(webrtc.MimeTypeH264),
			control.ClientVendorNameMetadata("waveguide-whip-input"),
			control.ClientVendorVersionMetadata("0.0.1"),
		)

		if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {

			s.log.Error(err)
			return
		} else if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			s.log.Error(err)
			return
		}

		peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			codec := remoteTrack.Codec()

			if codec.MimeType == webrtc.MimeTypeOpus {
				s.log.Info("Got Opus track, sending to audio track")
				for {
					if ctx.Err() != nil {
						return
					}

					p, _, err := remoteTrack.ReadRTP()
					if err != nil {
						s.log.Error(err)
						return
					}
					audioTrack.WriteRTP(p)
					stream.ReportMetadata(control.AudioPacketsMetadata(len(p.Payload)))
				}
			} else if codec.MimeType == webrtc.MimeTypeH264 {
				s.log.Info("Got H264 track, sending to video track")
				for {
					if ctx.Err() != nil {
						return
					}

					p, _, err := remoteTrack.ReadRTP()
					if err != nil {
						s.log.Error(err)
						return
					}
					videoTrack.WriteRTP(p)
					stream.ReportMetadata(control.VideoPacketsMetadata(len(p.Payload)))
				}
			}
		})

		peerConnection.OnICEConnectionStateChange(func(i webrtc.ICEConnectionState) {
			if i == webrtc.ICEConnectionStateFailed {
				if err := peerConnection.Close(); err != nil {
					s.log.Info(err)
					return
				}
			}
		})

		peerConnection.OnConnectionStateChange(func(pcs webrtc.PeerConnectionState) {
			shouldClose := false
			switch pcs {
			case webrtc.PeerConnectionStateClosed:
				shouldClose = true
			case webrtc.PeerConnectionStateDisconnected:
				shouldClose = true
			case webrtc.PeerConnectionStateFailed:
				shouldClose = true
			}

			if shouldClose {
				s.cleanupPeerConnection(channelID)
				s.control.StopStream(channelID)
			}
		})

		s.addPeerConnection(channelID, peerConnection)
		s.startPeerConnectionTimeout(channelID)

		if err := peerConnection.SetRemoteDescription(webrtc.SessionDescription{
			SDP:  string(offer),
			Type: webrtc.SDPTypeOffer,
		}); err != nil {
			s.log.Error(err)
			errWrongParams(w, r)
			return
		}

		gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
		answer, err := peerConnection.CreateAnswer(nil)

		if err != nil {
			s.log.Error(err)
			errWrongParams(w, r)
			return
		} else if err = peerConnection.SetLocalDescription(answer); err != nil {
			s.log.Error(err)
			errWrongParams(w, r)
			return
		}

		<-gatherComplete

		w.Header().Add("Access-Control-Expose-Headers", "expire")
		w.Header().Add("Content-Type", "application/sdp")
		w.Header().Add("Expire", ttl.Format(http.TimeFormat))

		fmt.Fprint(w, peerConnection.LocalDescription().SDP)
	})
}

func (s *WHIPSource) addPeerConnection(channelID types.ChannelID, pc *webrtc.PeerConnection) {
	s.peerConnectionsMutex.Lock()
	defer s.peerConnectionsMutex.Unlock()

	s.peerConnections[channelID] = pc
}
func (s *WHIPSource) getPeerConnection(channelID types.ChannelID) (*webrtc.PeerConnection, bool) {
	s.peerConnectionsMutex.RLock()
	defer s.peerConnectionsMutex.RUnlock()

	val, ok := s.peerConnections[channelID]
	return val, ok
}
func (s *WHIPSource) startPeerConnectionTimeout(channelID types.ChannelID) {
	go func() {
		time.Sleep(PC_TIMEOUT)

		pc, ok := s.getPeerConnection(channelID)
		if ok && pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
			s.log.Infof("Peer %s took too long to connect, rejecting peer.", channelID)
			s.cleanupPeerConnection(channelID)
		}
	}()
}
func (s *WHIPSource) cleanupPeerConnection(channelID types.ChannelID) {
	s.peerConnectionsMutex.Lock()
	defer s.peerConnectionsMutex.Unlock()

	if pc, ok := s.peerConnections[channelID]; ok {
		pc.Close()
	}

	delete(s.peerConnections, channelID)
}

func errCustom(w http.ResponseWriter, r *http.Request, message string) {
	w.WriteHeader(http.StatusBadRequest)
	w.Header().Set("Content-Type", "plain/text")
	w.Write([]byte(message))
}
func errUnauthorized(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusUnauthorized)
	w.Header().Set("Content-Type", "plain/text")
	w.Write([]byte("Unauthorized"))
}
func errWrongParams(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusBadRequest)
	w.Header().Set("Content-Type", "plain/text")
	w.Write([]byte("Invalid Parameters"))
}
