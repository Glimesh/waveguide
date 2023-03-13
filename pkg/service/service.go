package service

import (
	"github.com/Glimesh/waveguide/config"
	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/Glimesh/waveguide/pkg/service/dummy"
	"github.com/Glimesh/waveguide/pkg/service/glimesh"
	"github.com/sirupsen/logrus"
)

func New(cfg config.Config, logger *logrus.Logger) control.Service {
	svcCfg := cfg.Service
	var svc control.Service

	switch svcCfg.Type {
	case "dummy":
		svc = dummy.New(dummy.Config{})
	case "glimesh":
		svc = glimesh.New(svcCfg.Endpoint, svcCfg.ClientID, svcCfg.ClientSecret)
	default:
		panic("unsupported service type")
	}

	svc.SetLogger(logger.WithFields(logrus.Fields{
		"service": svc.Name(),
	}))

	return svc
}
