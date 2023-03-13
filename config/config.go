package config

import (
	"github.com/kkyr/fig"
)

type InputSource struct {
	Type string `fig:"type" validate:"required"`

	Address string `fig:"address" validate:"required"`

	// fs, whip
	VideoFile string `fig:"video_file"`
	AudioFile string `fig:"audio_file"`

	// janus
	ChannelID int `fig:"channel_id"`
}

type OutputSource struct {
	Type string `fig:"type" validate:"required"`

	Address string `fig:"address" validate:"required"`

	// whep

	Server        string `fig:"server"`
	HTTPS         bool
	HTTPSHostname string `fig:"https_hostname"`
	HTTPSCert     string `fig:"https_cert"`
	HTTPSKey      string `fig:"https_key"`
}

type Config struct {
	Input struct {
		Sources []InputSource `fig:"sources"`
	}

	Output struct {
		Sources []OutputSource `fig:"sources"`
	}

	Service struct {
		Type string `fig:"type" validate:"required"`

		Endpoint     string `fig:"endpoint"`
		ClientID     string `fig:"client_id"`
		ClientSecret string `fig:"client_secret"`
	}

	Orchestrator struct {
		Type string `fig:"type" validate:"required"`

		// rt orchestrator

		Endpoint     string `fig:"endpoint"`
		Key          string `fig:"key"`
		WHEPEndpoint string `fig:"whep_endpoint"`

		// ftl orchestrator

		Address    string `fig:"address"`
		RegionCode string `fig:"region_code"`
	}

	Control struct {
		Service      string `fig:"service"`
		Orchestrator string `fig:"orchestrator"`
		LogLevel     string `fig:"log_level" default:"info"`

		Address        string `fig:"http_address"`
		HTTPServerType string `fig:"http_server_type"`
		HTTPSHostname  string `mapstructure:"https_hostname"`
		HTTPSCert      string `mapstructure:"https_cert"`
		HTTPSKey       string `mapstructure:"https_key"`
	}
}

func Load() (Config, error) {
	var cfg Config

	if err := fig.Load(&cfg, fig.File("config.toml"), fig.Dirs(".")); err != nil {
		return cfg, nil
	}

	return cfg, nil
}
