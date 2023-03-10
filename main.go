package main

import (
	"context"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/Glimesh/waveguide/config"
	inputs "github.com/Glimesh/waveguide/internal/inputs"
	outputs "github.com/Glimesh/waveguide/internal/outputs"
	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/Glimesh/waveguide/pkg/orchestrator"
	"github.com/Glimesh/waveguide/pkg/service"
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
		log.Fatalf("failed to read config: %w", err)
	}

	// Temporary for debugging
	go func() {
		log.Println(http.ListenAndServe(":6060", nil))
	}()

	level, err := logrus.ParseLevel(cfg.Control.LogLevel)
	if err != nil {
		log.Fatalf("failed to parse log level: %w", err)
	}
	log.SetLevel(level)

	svc := service.New(cfg, log)
	if err := svc.Connect(); err != nil {
		log.Fatalf("failed to connect service: %w", err)
	}

	or := orchestrator.New(cfg, hostname, log)
	if err := or.Connect(); err != nil {
		log.Fatalf("failed to connect orchestrator: %w", err)
	}

	ctrl := control.New(cfg, hostname, svc, or, log)

	ctx := context.Background()

	in, err := inputs.New(cfg, ctrl, log)
	if err != nil {
		log.Fatalf("failed to create inputs: %w", err)
	}
	in.Start(ctx)

	out, err := outputs.New(cfg, ctrl, log)
	if err != nil {
		log.Fatalf("failed to create outputs: %w", err)
	}
	out.Start(ctx)

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Info("Exiting Waveguide and cleaning up")
		ctrl.Shutdown()
		os.Exit(0)
	}()

	ctrl.StartHTTPServer()
}
