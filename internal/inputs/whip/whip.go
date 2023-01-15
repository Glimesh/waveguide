package whip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

type WHIPSource struct {
	log     logrus.FieldLogger
	config  WHIPSourceConfig
	control *control.Control

	peerConnections []*webrtc.PeerConnection
}

type WHIPSourceConfig struct {
	// Listen address of the FS server in the ip:port format
	Address   string
	VideoFile string `mapstructure:"video_file"`
	AudioFile string `mapstructure:"audio_file"`
}

func New(config WHIPSourceConfig) *WHIPSource {
	return &WHIPSource{
		config: config,
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
		channelID := control.ChannelID(intChannelID)

		err = s.control.Authenticate(channelID, control.StreamKey(streamKey))
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

		stream, err := s.control.StartStream(channelID)
		if err != nil {
			s.log.Error(err)
			errCustom(w, r, "Problem starting the stream")
			return
		}
		defer func() {
			s.control.StopStream(channelID)
		}()

		peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			s.log.Error(err)
			errCustom(w, r, "Problem creating the peer connection")
			return
		}

		audioTrack, videoTrack, err := createTracksForStream(streamKey)
		if err != nil {
			s.log.Error(err)
			errWrongParams(w, r)
			return
		}

		stream.AddTrack(audioTrack, webrtc.MimeTypeOpus)
		stream.AddTrack(videoTrack, webrtc.MimeTypeH264)

		peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			var localTrack *webrtc.TrackLocalStaticRTP
			isVideo := false
			if strings.HasPrefix(remoteTrack.Codec().RTPCodecCapability.MimeType, "audio") {
				localTrack = audioTrack
			} else {
				localTrack = videoTrack
				isVideo = true
			}

			rtpBuf := make([]byte, 1500)
			for {
				rtpRead, _, readErr := remoteTrack.Read(rtpBuf)
				switch {
				case errors.Is(readErr, io.EOF):
					return
				case readErr != nil:
					s.log.Info(readErr)
					return
				}

				if _, writeErr := localTrack.Write(rtpBuf[:rtpRead]); writeErr != nil && !errors.Is(writeErr, io.ErrClosedPipe) {
					s.log.Info(writeErr)
					return
				}
				if isVideo {
					stream.ReportMetadata(control.VideoPacketsMetadata(rtpRead))
				} else {
					stream.ReportMetadata(control.AudioPacketsMetadata(rtpRead))
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

		fmt.Println(string(offer))

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

		s.peerConnections = append(s.peerConnections, peerConnection)

		fmt.Fprint(w, peerConnection.LocalDescription().SDP)
	})
}

func createTracksForStream(streamKey string) (*webrtc.TrackLocalStaticRTP, *webrtc.TrackLocalStaticRTP, error) {
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion")
	if err != nil {
		return nil, nil, err
	}

	audioTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "pion")
	if err != nil {
		return nil, nil, err
	}

	return videoTrack, audioTrack, nil
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
func errNotFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	w.Header().Set("Content-Type", "plain/text")
	w.Write([]byte("Not found"))
}
