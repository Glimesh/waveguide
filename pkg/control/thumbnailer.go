package control

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
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
			s.log.Debug("exiting on track")
		LOOP:
			for {
				select {
				case <-s.lastThumbnail:
				default:
					s.log.Debug("thumbnail channel drained")
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
					s.log.Debug("on track stop signal")
					close(s.rtpIngest)
					return
				default:
					pkt, _, readErr := track.ReadRTP()
					if readErr != nil {
						// terminate the ingestor is input stream is EOF
						if errors.Is(readErr, io.EOF) {
							s.log.Debugf("read: %v", readErr)
							close(s.rtpIngest)
							return
						}
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

func (s *Stream) startIngestor() {
	done := make(chan struct{}, 1)

	go func() {
		for {
			s.log.Debug("waiting for thumbnail request signal")
			select {
			case <-s.requestThumbnail:
			case <-done:
				s.log.Debug("stopping thumbnailer")
				return
			}
			s.log.Debug("thumbnail request received")

			for len(s.thumbnailReceiver) > 0 {
				<-s.thumbnailReceiver
			}
			s.log.Debug("thumbnail buffer drained")

			var pkt *rtp.Packet

			t := time.Now()
		LOOP:
			for {
				select {
				case pkt = <-s.thumbnailReceiver:
				case <-done:
					s.log.Debug("stopping thumbnail receiver")
					return
				}

				select {
				case <-done:
					s.log.Debug("stopping thumbnailer")
					return
				default:
					// use a deadline of 10 seconds to retrieve a keyframe
					if time.Since(t) > time.Second*10 {
						s.log.Warn("keyframe not available")
						break LOOP
					}
					keyframe := s.keyframer.NewKeyframe(pkt)
					if keyframe != nil {
						s.log.Info("got keyframe")
						s.lastThumbnail <- keyframe
						s.log.Debug("sent keyframe")
						// reset and sleep after sending one keyframe
						s.keyframer.Reset()
						break LOOP
					}
				}
			}
		}
	}()

	for p := range s.rtpIngest {
		select {
		case s.thumbnailReceiver <- p:
		default:
		}
	}
	s.log.Debug("closed ingestor listener")

	done <- struct{}{}
	s.log.Debug("ending rtp ingestor")
}
