package fs

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/h264reader"
	"github.com/sirupsen/logrus"
)

type FSSource struct {
	log     logrus.FieldLogger
	config  FSSourceConfig
	control *control.Control
}

type FSSourceConfig struct {
	// Listen address of the FS server in the ip:port format
	Address   string
	VideoFile string `mapstructure:"video_file"`
	AudioFile string `mapstructure:"audio_file"`
}

func New(config FSSourceConfig) *FSSource {
	return &FSSource{
		config: config,
	}
}

func (s *FSSource) SetControl(ctrl *control.Control) {
	s.control = ctrl
}

func (s *FSSource) SetLogger(log logrus.FieldLogger) {
	s.log = log
}

func (s *FSSource) Listen(ctx context.Context) {
	s.log.Infof("Reading from FS for video=%s and audio=%s", s.config.VideoFile, s.config.AudioFile)

	// Assert that we have an audio or video file
	_, err := os.Stat(s.config.VideoFile)
	haveVideoFile := !os.IsNotExist(err)

	_, err = os.Stat(s.config.AudioFile)
	haveAudioFile := !os.IsNotExist(err)

	if !haveAudioFile && !haveVideoFile {
		panic("Could not find files")
	}

	videoTrack, videoTrackErr := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion")
	if videoTrackErr != nil {
		panic(videoTrackErr)
	}

	s.control.StartStream(1234)
	s.control.AddTrack(1234, videoTrack, webrtc.MimeTypeH264)

	go func() {
		// Open a H264 file and start reading using our IVFReader
		file, h264Err := os.Open(s.config.VideoFile)
		if h264Err != nil {
			panic(h264Err)
		}

		h264, h264Err := h264reader.NewReader(file)
		if h264Err != nil {
			panic(h264Err)
		}

		h264FrameDuration := time.Millisecond * 33

		// Send our video file frame at a time. Pace our sending so we send it at the same speed it should be played back as.
		// This isn't required since the video is timestamped, but we will such much higher loss if we send all at once.
		//
		// It is important to use a time.Ticker instead of time.Sleep because
		// * avoids accumulating skew, just calling time.Sleep didn't compensate for the time spent parsing the data
		// * works around latency issues with Sleep (see https://github.com/golang/go/issues/44343)
		ticker := time.NewTicker(h264FrameDuration)
		for ; true; <-ticker.C {
			nal, h264Err := h264.NextNAL()
			if h264Err == io.EOF {
				s.log.Info("All video frames parsed and sent")
				os.Exit(0)
			}
			if h264Err != nil {
				panic(h264Err)
			}

			if h264Err = videoTrack.WriteSample(media.Sample{Data: nal.Data, Duration: h264FrameDuration}); h264Err != nil {
				panic(h264Err)
			}
		}
	}()
}
