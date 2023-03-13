package orchestrator

import (
	"github.com/Glimesh/waveguide/config"
	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/Glimesh/waveguide/pkg/orchestrator/dummy"
	"github.com/Glimesh/waveguide/pkg/orchestrator/rt"
	"github.com/sirupsen/logrus"
)

func New(cfg config.Config, hostname string, logger *logrus.Logger) control.Orchestrator {
	orCfg := cfg.Orchestrator

	var or control.Orchestrator

	switch cfg.Orchestrator.Type {
	case "dummy":
		or = dummy.New(dummy.Config{}, hostname)
	case "rt":
		or = rt.New(hostname, orCfg.Endpoint, orCfg.Key, orCfg.WHEPEndpoint)
		// case "ftl":
		// TODO: ftl orchestrator
	default:
		panic("unknown orchestrator type")
	}

	or.SetLogger(logger.WithFields(logrus.Fields{
		"orchestrator": or.Name(),
	}))

	return or
}
