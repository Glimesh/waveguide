package service

import (
	"github.com/Glimesh/waveguide/config"
	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/Glimesh/waveguide/pkg/service/dummy"
	"github.com/Glimesh/waveguide/pkg/service/glimesh"
	"github.com/sirupsen/logrus"
)

func New(cfg config.Config, logger *logrus.Logger) control.Service {
	var svc control.Service

	switch cfg.Service.Type {
	case "dummy":
		svc = dummy.New(dummy.Config{})
	case "glimesh":
		svc = glimesh.New(glimesh.Config{
			Endpoint:     cfg.Service.Endpoint,
			ClientID:     cfg.Service.ClientID,
			ClientSecret: cfg.Service.ClientSecret,
		})
	default:
		panic("unsupported service type")
	}

	svc.SetLogger(logger.WithFields(logrus.Fields{
		"orchestrator": svc.Name(),
	}))

	return svc
}
