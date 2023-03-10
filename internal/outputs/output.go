package output

import (
	"context"
	"fmt"

	"github.com/Glimesh/waveguide/config"
	"github.com/Glimesh/waveguide/internal/outputs/hls"
	"github.com/Glimesh/waveguide/internal/outputs/whep"
	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/sirupsen/logrus"
)

type Outputs []control.Output

func New(cfg config.Config, ctrl *control.Control, logger *logrus.Logger) (Outputs, error) {
	sources := cfg.Output.Sources

	outputs := make(Outputs, 0, len(sources))

	for _, src := range sources {
		var output control.Output

		switch src.Type {
		case "hls":
			output = hls.New(src.Address)
		case "whep":
			if src.HTTPS {
				output = whep.New(
					src.Address,
					src.Server,
					whep.WithHTTPS(src.HTTPSHostname, src.HTTPSCert, src.HTTPSKey),
				)
			} else {
				output = whep.New(src.Address, src.Server)
			}
		default:
			return nil, fmt.Errorf("unsupported output source type %s", src.Type)
		}
		output.SetControl(ctrl)
		output.SetLogger(logger.WithFields(logrus.Fields{"output": src.Type}))
		outputs = append(outputs, output)
	}

	return outputs, nil
}

func (out Outputs) Start(ctx context.Context) {
	for i := range out {
		output := out[i]
		go output.Listen(ctx)
	}
}
