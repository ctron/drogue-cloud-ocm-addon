package config

import "github.com/kelseyhightower/envconfig"

type DeviceSynchronizerConfig struct {
	Application string `required:"true" envconfig:"DROGUE__APPLICATION"`

	API struct {
		URL string `required:"true" envconfig:"DROGUE__API__URL"`
	}
	MQTT struct {
		IntegrationUrl string `required:"true" envconfig:"DROGUE__MQTT_INTEGRATION__URL"`
		Group          string `default:"drogue-cloud-ocm" envconfig:"DROGUE__MQTT_INTEGRATION__GROUP"`
	}

	Credentials struct {
		Username string `envconfig:"DROGUE__USERNAME"`
		Token    string `envconfig:"DROGUE__TOKEN"`
	}
}

func LoadConfig() (*DeviceSynchronizerConfig, error) {
	var config DeviceSynchronizerConfig
	if err := envconfig.Process("", &config); err != nil {
		return nil, err
	}

	return &config, nil
}
