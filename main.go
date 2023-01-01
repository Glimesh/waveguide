package main

import (
	"context"
	"fmt"
	_ "net/http/pprof"
	"os"

	"github.com/Glimesh/waveguide/internal/inputs/fs"
	"github.com/Glimesh/waveguide/internal/inputs/ftl"
	"github.com/Glimesh/waveguide/internal/inputs/janus"
	"github.com/Glimesh/waveguide/internal/inputs/rtmp"
	"github.com/Glimesh/waveguide/internal/outputs/hls"
	"github.com/Glimesh/waveguide/internal/outputs/whep"
	"github.com/Glimesh/waveguide/pkg/control"
	"github.com/Glimesh/waveguide/pkg/orchestrators/dummy_orchestrator"
	"github.com/Glimesh/waveguide/pkg/orchestrators/rt_orchestrator"
	"github.com/Glimesh/waveguide/pkg/services/dummy_service"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {
	log := logrus.New()
	log.Level = logrus.DebugLevel

	hostname, err := os.Hostname()
	if err != nil {
		// How tf
		log.Fatal(err)
	}
	log.Debugf("Server Hostname: %s", hostname)

	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	err = viper.ReadInConfig()
	if err != nil {
		log.Fatal(fmt.Errorf("fatal error config file: %w", err))
	}

	var service control.Service
	switch viper.GetString("control.service") {
	case "dummy":
		service = dummy_service.New(dummy_service.Config{})
	}
	service.SetLogger(log.WithField("service", service.Name()))

	var orchestrator control.Orchestrator
	switch viper.GetString("control.orchestrator") {
	case "dummy":
		orchestrator = dummy_orchestrator.New(dummy_orchestrator.Config{}, hostname)
	case "rt":
		var rtConfig rt_orchestrator.Config
		unmarshalConfig("orchestrator.rtrouter", &rtConfig)
		orchestrator = rt_orchestrator.New(rtConfig, hostname)
	}
	service.SetLogger(log.WithField("orchestrator", service.Name()))

	ctrl := control.New()
	ctrl.SetService(service)
	ctrl.SetOrchestrator(orchestrator)

	ctx := context.Background()
	for inputName := range viper.GetStringMap("input") {
		inputType := viper.GetString(fmt.Sprintf("input.%s.type", inputName))
		configKey := fmt.Sprintf("input.%s", inputName)

		var input control.Input

		switch inputType {
		case "fs":
			var fsConfig fs.FSSourceConfig
			unmarshalConfig(configKey, &fsConfig)
			input = fs.New(fsConfig)
		case "janus":
			var janusConfig janus.JanusSourceConfig
			unmarshalConfig(configKey, &janusConfig)
			input = janus.New(janusConfig)
		case "rtmp":
			var rtmpConfig rtmp.RTMPSourceConfig
			unmarshalConfig(configKey, &rtmpConfig)
			input = rtmp.New(rtmpConfig)
		case "ftl":
			var ftlConfig ftl.FTLSourceConfig
			unmarshalConfig(configKey, &ftlConfig)
			input = ftl.New(ftlConfig)
		default:
			log.Fatalf("could not find input type %s", inputType)
		}
		input.SetControl(ctrl)
		input.SetLogger(log.WithFields(logrus.Fields{"input": inputType}))
		go input.Listen(ctx)
	}

	for outputName := range viper.GetStringMap("output") {
		outputType := viper.Get(fmt.Sprintf("output.%s.type", outputName))
		configKey := fmt.Sprintf("output.%s", outputName)

		var output control.Output

		switch outputType {
		case "hls":
			var hlsConfig hls.HLSConfig
			unmarshalConfig(configKey, &hlsConfig)
			output = hls.New(hlsConfig)
		case "whep":
			var whepConfig whep.WHEPConfig
			unmarshalConfig(configKey, &whepConfig)
			output = whep.New(whepConfig)
		}

		output.SetControl(ctrl)
		output.SetLogger(log.WithFields(logrus.Fields{"output": outputName}))
		go output.Listen(ctx)
	}

	select {}
}

func unmarshalConfig(configKey string, config interface{}) error {
	err := viper.UnmarshalKey(configKey, &config)
	if err != nil {
		return err
	}
	return nil
}
