package main

import (
	"context"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	config "github.com/Glimesh/waveguide/config"
	inputs "github.com/Glimesh/waveguide/internal/inputs"
	outputs "github.com/Glimesh/waveguide/internal/outputs"
	control "github.com/Glimesh/waveguide/pkg/control"

	"github.com/sirupsen/logrus"
)

func main() {
	log := logrus.New()

	hostname, err := os.Hostname()
	if err != nil {
		// How tf
		log.Fatal(err)
	}
	log.Debugf("Server Hostname: %s", hostname)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to read config: %v", err)
	}

	// Temporary for debugging
	go func() {
		log.Println(http.ListenAndServe(":6060", nil))
	}()

	level, err := logrus.ParseLevel(cfg.Control.LogLevel)
	if err != nil {
		log.Fatalf("failed to parse log level: %v", err)
	}
	log.SetLevel(level)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt, syscall.SIGTERM, syscall.SIGINT,
	)

	ctrl, err := control.New(ctx, cfg, hostname, log)
	if err != nil {
		log.Fatalf("failed to create control: %v", err)
	}

	in, err := inputs.New(cfg, ctrl, log)
	if err != nil {
		log.Fatalf("failed to create inputs: %v", err)
	}
	in.Start(ctx)

	out, err := outputs.New(cfg, ctrl, log)
	if err != nil {
		log.Fatalf("failed to create outputs: %v", err)
	}
	out.Start(ctx)

	go ctrl.StartHTTPServer()

	<-ctx.Done()
	stop()
	log.Info("Exiting Waveguide and cleaning up")
	ctrl.Shutdown()
}
