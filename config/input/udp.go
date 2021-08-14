package input

import (
	"fmt"
	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/elastic/beats/filebeat/util"
	"github.com/elastic/go-ucfg"
	"strconv"
)

var udpUniqueRecord = make(map[string]bool)

func makeUdpProcessors(token string) beat.MapStr {
	return beat.MapStr{
		"processors": []beat.MapStr{
			{
				"decode_json_fields": beat.MapStr{
					"fields": []string{"message"},
					"target": "message_decode",
				},
			},
			{
				"drop_event": beat.MapStr{
					"when": beat.MapStr{
						"not": beat.MapStr{
							"equals": beat.MapStr{
								"message_decode.token": token,
							},
						},
					},
				},
			},
			{
				"drop_fields": beat.MapStr{
					"fields": []string{"message"},
				},
			},
			{
				"rename": beat.MapStr{
					"fields": []beat.MapStr{
						{
							"from": "message_decode.data",
							"to":   "message",
						},
					},
				},
			},
			{
				"rename": beat.MapStr{
					"fields": []beat.MapStr{
						{
							"from": "message_decode.data",
							"to":   "message",
						},
					},
				},
			},
			{
				"drop_fields": beat.MapStr{
					"fields": []string{"message_decode"},
				},
			},
		},
	}
}

func defaultUdpProcessors() beat.MapStr {
	return beat.MapStr{
		"processors": []beat.MapStr{
			{
				"decode_json_fields": beat.MapStr{
					"fields": []string{"message"},
					"target": "message_decode",
				},
			},
			{
				"drop_fields": beat.MapStr{
					"fields": []string{"message"},
				},
			},
			{
				"rename": beat.MapStr{
					"fields": []beat.MapStr{
						{
							"from": "message_decode.dataid",
							"to":   "dataid",
						},
					},
				},
			},
			{
				"rename": beat.MapStr{
					"fields": []beat.MapStr{
						{
							"from": "message_decode.data",
							"to":   "message",
						},
					},
				},
			},
			{
				"drop_fields": beat.MapStr{
					"fields": []string{"message_decode"},
				},
			},
		},
	}
}

const (
	UDP_PLAINTEXT = "plaintext"
	// json格式适用于需要 传入dataid的情况 data 为日志原文 {"data": "xxxx", "dataid": 123}
	UDP_JSON = "json"
)

func init() {
	config := beat.MapStr{
		"type":             "udp",
		"host":             "localhost:8080",
		"max_message_size": "128Kib",
		"encoding":         "utf-8",
		"token":            "",
		// json plaintext
		"input_type": UDP_PLAINTEXT,
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
		// check token
		token, err := rawConfig.String("token", -1)
		if err != nil {
			token = ""
		}
		inputType, err := rawConfig.String("input_type", -1)

		if err != nil {
			return nil, err
		}
		var processorsConfig beat.MapStr

		if inputType == UDP_JSON {
			processorsConfig = defaultUdpProcessors()
		}

		if token != "" {
			processorsConfig = makeUdpProcessors(token)
		}

		rawConfig.Remove("input_type", -1)
		rawConfigOrigin := (*ucfg.Config)(rawConfig)
		err = rawConfigOrigin.Merge(processorsConfig, ucfg.AppendValues)
		if err != nil {
			return nil, err
		}
		return rawConfig, nil
	})
	if err != nil {
		panic(err)
	}

	err = utils.RegisterHash("udp", func(rawConfig *beat.Config) (error, string) {
		var err error
		host, err := rawConfig.String("host", -1)
		if err != nil {
			return err, ""
		}
		return nil, utils.Md5(host)
	})

	if err != nil {
		panic(err)
	}

	err = utils.RegisterCacheIdentifier("udp", func(event *util.Data, dataId string) string {
		dataid, err := event.Event.GetValue("dataid")
		if err == nil {
			dataId = strconv.FormatInt(dataid.(int64), 10)
		}
		address, err := event.Event.GetValue("log.source.address")
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
