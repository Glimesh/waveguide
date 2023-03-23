package control

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pion/rtp"
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
	done := make(chan struct{}, 1)

	go func() {
		timer := time.NewTimer(time.Second * 5)
		defer timer.Stop()

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

			for {
				select {
				case pkt = <-s.thumbnailReceiver:
				case <-done:
					s.log.Debug("stopping thumbnail receiver")
					return
				}

				select {
				// stop the loop so the receive signals are not overlapped
				case <-timer.C:
					s.log.Debug("keyframer timed out")
					s.log.Warn("thumbnail not available")
					break
				case <-done:
					s.log.Debug("stopping thumbnailer")
					return
				default:
					keyframe := s.keyframer.NewKeyframe(pkt)
					if keyframe != nil {
						s.log.Info("got keyframe")
						s.lastThumbnail <- keyframe
						s.log.Debug("sent keyframe")
						// reset and sleep after sending one keyframe
						s.keyframer.Reset()
						break
					}
				}
			}
		}
	}()

	for p := range s.rtpIngest {
		// control.go L:292 - control requests thumbnails to be sent on nth tick
		select {
		case s.thumbnailReceiver <- p:
		default:
		}
	}

	done <- struct{}{}
	s.log.Debug("ending rtp ingestor")
}

// func (s *Stream) startIngestorCond() {
// 	done := false
// 	// keyframe listener
// 	go func() {
// 		s.log.Debug("thumbnail listener started")
// 		for {
// 			s.cond.L.Lock()
// 			for !s.thumbnailRequested {
// 				s.log.Debug("waiting for keyframe request")
// 				s.cond.Wait()
// 			}
// 			s.cond.L.Unlock()
// 			if done {
// 				break
// 			}

// 			// buf := make([]*rtp.Packet, 0)
// 			// for i := 0; i < 100; i++ {
// 			// 	pkt := <-s.thumbnailReceiver
// 			// 	s.log.Debug("received packet ", i)
// 			// 	buf = append(buf, pkt.Clone())
// 			// }
// 			// s.log.Debug("filled rtp packet buffer")
// 			// s.cond.L.Lock()
// 			// s.thumbnailRequested = false
// 			// s.cond.L.Unlock()

// 			pkt := <-s.thumbnailReceiver

// 			keyframe := s.keyframer.NewKeyframe(pkt)
// 			if keyframe != nil {
// 				s.log.Debug("got keyframe")
// 				select {
// 				case s.lastThumbnail <- keyframe:
// 					s.log.Debug("sent keyframe")
// 					s.keyframer.Reset()
// 				default:
// 					s.keyframer.Reset()
// 					// s.log.Debug("thumbnail queue full at ", i)
// 					break
// 				}
// 			}
// 			// for i, pkt := range buf {
// 			// }
// 			// s.keyframer.Reset()
// 		}
// 		s.log.Debug("ending thumbnail listener")
// 	}()

// 	for p := range s.rtpIngest {
// 		select {
// 		case s.thumbnailReceiver <- p:
// 		default:
// 		}
// 	}
// 	done = true

// 	s.log.Debug("ending rtp ingestor")
// }
