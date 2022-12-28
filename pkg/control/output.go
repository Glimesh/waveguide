package control

import (
	"context"

	"github.com/sirupsen/logrus"
)

type Output interface {
	SetControl(ctrl *Control)
	SetLogger(log logrus.FieldLogger)

	Listen(ctx context.Context)
}
