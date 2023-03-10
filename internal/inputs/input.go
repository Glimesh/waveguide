package input

import (
	"context"
	"fmt"

	"github.com/Glimesh/waveguide/config"
	"github.com/Glimesh/waveguide/internal/inputs/fs"
	"github.com/Glimesh/waveguide/internal/inputs/ftl"
	"github.com/Glimesh/waveguide/internal/inputs/janus"
	"github.com/Glimesh/waveguide/internal/inputs/rtmp"
	"github.com/Glimesh/waveguide/internal/inputs/whip"
	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/sirupsen/logrus"
)

type Inputs []control.Input

func New(cfg config.Config, ctrl *control.Control, logger *logrus.Logger) (Inputs, error) {
	sources := cfg.Input.Sources

	inputs := make(Inputs, 0, len(sources))

	for _, src := range sources {
		var input control.Input

		switch src.Type {
		case "fs":
			input = fs.New(src.Address, src.VideoFile, src.AudioFile)
		case "janus":
			input = janus.New(src.Address, src.ChannelID)
		case "rtmp":
			input = rtmp.New(src.Address)
		case "ftl":
			input = ftl.New(src.Address)
		case "whip":
			input = whip.New(src.Address, src.VideoFile, src.AudioFile)
		default:
			return nil, fmt.Errorf("unsupported input source type %s", src.Type)
		}
		input.SetControl(ctrl)
		input.SetLogger(logger.WithFields(logrus.Fields{"input": src.Type}))
		inputs = append(inputs, input)
	}

	return inputs, nil
}

func (in Inputs) Start(ctx context.Context) {
	for i := range in {
		input := in[i]
		go input.Listen(ctx)
	}
}
