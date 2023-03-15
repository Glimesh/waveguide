package janus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/Glimesh/waveguide/pkg/types"

	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

type Source struct {
	log     logrus.FieldLogger
	control *control.Control

	channelID types.ChannelID

	// Address to connect to for Janus
	Address   string
	ChannelID int `mapstructure:"channel_id"`
}

func New(address string, channelID int) control.Input {
	return &Source{
		Address:   address,
		ChannelID: channelID,
	}
}

func (s *Source) SetControl(ctrl *control.Control) {
	s.control = ctrl
}

func (s *Source) SetLogger(log logrus.FieldLogger) {
	s.log = log
}

type janusCreateResponse struct {
	Janus       string `json:"janus"`
	Transaction string `json:"transaction"`
	Data        struct {
		Id int `json:"id"`
	} `json:"data"`
}
type JSEP struct {
	Type    string `json:"type"`
	Sdp     string `json:"sdp"`
	Trickle bool   `json:"trickle,omitempty"`
}

type janusFtlOfferResponse struct {
	Janus       string `json:"janus"`
	Transaction string `json:"transaction"`
	Jsep        JSEP   `json:"jsep"`
}

func (s *Source) Listen(ctx context.Context) {
	s.log.Infof("Connecting to janus=%s for channel_id=%d", s.Address, s.ChannelID)

	s.channelID = types.ChannelID(s.ChannelID)

	values := map[string]string{"janus": "create", "transaction": randString()}

	jsonValue, _ := json.Marshal(values)

	// Initial negotiation
	resp, err := http.Post(s.Address, "application/json", bytes.NewBuffer(jsonValue))
	if err != nil {
		panic(err)
	}
	var createResponse janusCreateResponse
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&createResponse); err != nil {
		panic(err)
	}

	sessionUrl := fmt.Sprintf("%s/%d", s.Address, createResponse.Data.Id)
	// Keepalive for the session
	go func() {
		// ticker := time.NewTicker(30 * time.Second)
		keepAlive, _ := json.Marshal(map[string]string{"janus": "keepalive", "session_id": fmt.Sprint(createResponse.Data.Id), "transaction": randString()})

		for {
			r, err := http.Post(sessionUrl, "application/json", bytes.NewBuffer(keepAlive))
			if err != nil {
				panic(err)
			}
			// body, _ := ioutil.ReadAll(r.Body)
			// fmt.Printf("Keepalive: %s\n", body)
			r.Body.Close()
			time.Sleep(20 * time.Second)
		}
	}()

	attachRequest, _ := json.Marshal(map[string]string{"janus": "attach", "plugin": "janus.plugin.ftl", "transaction": randString()})
	resp, err = http.Post(sessionUrl, "application/json", bytes.NewBuffer(attachRequest))
	if err != nil {
		panic(err)
	}
	var attachResponse struct {
		Janus       string
		Transaction string
		Data        struct {
			Id int
		}
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&attachResponse); err != nil {
		panic(err)
	}
	pluginUrl := fmt.Sprintf("%s/%d", sessionUrl, attachResponse.Data.Id)

	watchRequest, _ := json.Marshal(struct {
		Janus       string `json:"janus"`
		Transaction string `json:"transaction"`
		Body        struct {
			Request   string `json:"request"`
			ChannelID int    `json:"channelId"`
		} `json:"body"`
	}{
		Janus:       "message",
		Transaction: randString(),
		Body: struct {
			Request   string `json:"request"`
			ChannelID int    `json:"channelId"`
		}{
			Request:   "watch",
			ChannelID: s.ChannelID,
		},
	})
	_, err = http.Post(pluginUrl, "application/json", bytes.NewBuffer(watchRequest))
	if err != nil {
		panic(err)
	}

	// Long-poll
	go func() {
		for {
			longPoll, err := http.Get(sessionUrl)
			if err != nil {
				panic(err)
			}

			var offerResponse janusFtlOfferResponse
			if err := json.NewDecoder(longPoll.Body).Decode(&offerResponse); err != nil {
				body, _ := io.ReadAll(longPoll.Body)
				s.log.Warningf("Unexpected Long-Poll: %s\n", body)
			} else {
				if offerResponse.Jsep.Sdp != "" {
					s.log.Infof("Got offer: %s", offerResponse.Jsep.Sdp)
					s.negotiate(offerResponse.Jsep.Sdp, pluginUrl)
				}
			}

			// body, _ := ioutil.ReadAll(longPoll.Body)
			// fmt.Printf("Got: %s", body)
			longPoll.Body.Close()
		}
	}()
}

func (s *Source) negotiate(sdpString string, pluginUrl string) {
	stream, err := s.control.StartStream(types.ChannelID(s.ChannelID))
	if err != nil {
		panic(err)
	}

	videoTrack, videoTrackErr := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion")
	if videoTrackErr != nil {
		panic(videoTrackErr)
	}

	audioTrack, audioTrackErr := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "pion")
	if audioTrackErr != nil {
		panic(videoTrackErr)
	}

	stream.AddTrack(videoTrack, webrtc.MimeTypeH264)
	stream.AddTrack(audioTrack, webrtc.MimeTypeOpus)

	stream.ReportMetadata(
		control.AudioCodecMetadata(webrtc.MimeTypeOpus),
		control.VideoCodecMetadata(webrtc.MimeTypeH264),
		control.ClientVendorNameMetadata("waveguide-janus-input"),
		control.ClientVendorVersionMetadata("0.0.1"),
	)

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdpString,
	}

	// Create a new RTCPeerConnection
	var peerConnection *webrtc.PeerConnection
	peerConnection, err = webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{},
		},
		SDPSemantics: webrtc.SDPSemanticsUnifiedPlanWithFallback,
	})
	if err != nil {
		panic(err)
	}

	// We must offer to send media for Janus to send anything
	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		panic(err)
	} else if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		panic(err)
	}

	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		s.log.Infof("Connection State has changed %s", connectionState.String())
	})

	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		codec := track.Codec()
		s.log.Infof("Got Track: %s", codec.MimeType)
		if codec.MimeType == "audio/opus" {
			s.log.Info("Got Opus track, sending to audio track")
			for {
				if err := s.control.ContextErr(); err != nil {
					return
				}

				p, _, err := track.ReadRTP()
				if err != nil {
					panic(err)
				}
				audioTrack.WriteRTP(p)
				stream.ReportMetadata(control.AudioPacketsMetadata(len(p.Payload)))
			}
		} else if codec.MimeType == "video/H264" {
			s.log.Info("Got H264 track, sending to video track")
			for {
				if err := s.control.ContextErr(); err != nil {
					return
				}

				p, _, err := track.ReadRTP()
				if err != nil {
					panic(err)
				}
				videoTrack.WriteRTP(p)
				stream.ReportMetadata(control.VideoPacketsMetadata(len(p.Payload)))
			}
		}
	})

	if err = peerConnection.SetRemoteDescription(offer); err != nil {
		panic(err)
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	answer, answerErr := peerConnection.CreateAnswer(nil)
	if answerErr != nil {
		panic(answerErr)
	}

	if err = peerConnection.SetLocalDescription(answer); err != nil {
		panic(err)
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	s.log.Infof("Sending answer: %s", peerConnection.LocalDescription().SDP)
	startRequest, _ := json.Marshal(struct {
		Janus       string `json:"janus"`
		Transaction string `json:"transaction"`
		Jsep        JSEP   `json:"jsep"`
		Body        struct {
			Request string `json:"request"`
		} `json:"body"`
	}{
		Janus:       "message",
		Transaction: randString(),
		Body: struct {
			Request string `json:"request"`
		}{
			Request: "start",
		},
		Jsep: JSEP{
			Type:    "answer",
			Sdp:     peerConnection.LocalDescription().SDP,
			Trickle: false,
		},
	})
	_, err = http.Post(pluginUrl, "application/json", bytes.NewBuffer(startRequest))
	if err != nil {
		panic(err)
	}
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randString() string {
	n := 10
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
