package input

import (
	"fmt"
	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/elastic/beats/filebeat/util"
	"strconv"
)

func init() {
	config := beat.MapStr{
		"type":      "otlp",
		"endpoint":  "0.0.0.0:4317",
		"transport": "tcp",
	}

	err := cfg.Register("otlp", func(rawConfig *beat.Config) (*beat.Config, error) {
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
	err = utils.RegisterCacheIdentifier("otlp", func(event *util.Data, dataId string) string {
		dataid, err := event.Event.GetValue("dataid")
		if err == nil {
			dataId = strconv.FormatInt(dataid.(int64), 10)
		}
		address, err := event.Event.GetValue("trace_id")
		if err != nil {
			address = ""
		}
		address = utils.GetHostName(address.(string))
		return fmt.Sprintf("%s-%s", dataId, address)
	})
	if err != nil {
		panic(err)
	}
}
