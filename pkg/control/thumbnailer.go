package control

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/h264writer"
)

type header struct {
	key   string
	value string
}

func (s *Stream) Ingest(ctx context.Context) error {
	logger := s.log.WithField("app", "ingest")
	done := make(chan struct{}, 1)
	go s.startIngestor()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{}) //nolint exhaustive struct
	if err != nil {
		return err
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		cancelRead := make(chan struct{}, 1)
		go func() {
			<-done
			fmt.Println("exiting on track")
		LOOP:
			for {
				select {
				case <-s.lastThumbnail:
					fmt.Println("draining thumbnails")
				default:
					fmt.Println("thumbnail channel drained")
					break LOOP
				}
			}
			cancelRead <- struct{}{}
		}()

		codec := track.Codec()

		if codec.MimeType == "video/H264" {
			for {
				select {
				case <-cancelRead:
					fmt.Println("on track stop signal")
					close(s.rtpIngest)
					return
				default:
					pkt, _, readErr := track.ReadRTP()
					if readErr != nil {
						continue
					}
					s.rtpIngest <- pkt
				}
			}
		}
	})

	sdpHeader := header{"Accept", "application/sdp"}
	resp, err := doHTTPRequest(
		s.whepURI,
		http.MethodPost,
		strings.NewReader(""),
		sdpHeader,
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	offer, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := pc.SetRemoteDescription(
		webrtc.SessionDescription{
			Type: webrtc.SDPTypeOffer,
			SDP:  string(offer),
		}); err != nil {
		return err
	}

	answerSDP, err := pc.CreateAnswer(nil)
	if err != nil {
		return err
	}

	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answerSDP); err != nil {
		return err
	}
	<-gatherComplete

	answer := pc.LocalDescription().SDP
	_, err = doHTTPRequest( //nolint response is ignored
		resp.Header.Get("location"),
		http.MethodPost,
		strings.NewReader(answer),
		sdpHeader,
	)
	if err != nil {
		return err
	}

	<-ctx.Done()
	pc.Close()
	done <- struct{}{}
	logger.Debug("received ctx done signal")

	return nil
}

func doHTTPRequest(uri, method string, body io.Reader, headers ...header) (*http.Response, error) {
	req, err := http.NewRequest(method, uri, body)
	if err != nil {
		return nil, err
	}

	for _, header := range headers {
		req.Header.Set(header.key, header.value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

type Option struct {
	VideoWriter *h264writer.H264Writer
}

func (s *Stream) setupPeerConnection(pc *webrtc.PeerConnection) error {
	sdpHeader := header{"Accept", "application/sdp"}
	resp, err := doHTTPRequest(
		s.whepURI,
		http.MethodPost,
		strings.NewReader(""),
		sdpHeader,
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	offer, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := pc.SetRemoteDescription(
		webrtc.SessionDescription{
			Type: webrtc.SDPTypeOffer,
			SDP:  string(offer),
		}); err != nil {
		return err
	}

	answerSDP, err := pc.CreateAnswer(nil)
	if err != nil {
		return err
	}

	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answerSDP); err != nil {
		return err
	}
	<-gatherComplete

	answer := pc.LocalDescription().SDP
	_, err = doHTTPRequest( //nolint response is ignored
		resp.Header.Get("location"),
		http.MethodPost,
		strings.NewReader(answer),
		sdpHeader,
	)
	if err != nil {
		return err
	}

	return nil
}

func (s *Stream) startIngestor() {
	for p := range s.rtpIngest {
		// control.go L:292 - control requests thumbnails to be sent on nth tick
		if len(s.requestThumbnail) > 0 {
			fmt.Println("keyframe requested")
			keyframe := s.keyframer.KeyFrame(p.Clone())
			if keyframe != nil {
				fmt.Println("got keyframe:", len(keyframe))
				select {
				// do a non-blocking send of 10 keyframes
				case s.lastThumbnail <- keyframe:
					s.keyframer.Reset()
					fmt.Println("sent keyframe:", len(s.lastThumbnail))
				default:
					// when the send blocks - the channel is full
					// empty the requestThumbnail channel for the next tick of request for thumbnails
					<-s.requestThumbnail
					fmt.Println("reset request thumbnail")
					s.keyframer.Reset()
					break
				}
			}
		}
	}
	s.log.Debug("ending rtp ingestor")
}
