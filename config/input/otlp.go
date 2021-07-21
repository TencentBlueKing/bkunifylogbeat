package input

import (
	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
)

func init() {
	config := beat.MapStr{
		"type":      "otlp",
		"endpoint":  "0.0.0.0:4317",
		"transport": "tcp",
	}

	err := cfg.Register("udp", func(rawConfig *beat.Config) (*beat.Config, error) {
		var err error
		defaultConfig := beat.MapStr{}
		fields := rawConfig.GetFields()
		for key, value := range config {
			isExists := false
			for _, field := range fields {
				if key == field {
					isExists = true
					break
				}
			}
			if !isExists {
				defaultConfig[string(key)] = value
			}
		}
		err = rawConfig.Merge(defaultConfig)
		if err != nil {
			return nil, err
		}
		return rawConfig, nil
	})
	if err != nil {
		panic(err)
	}

}
