package formatter

import (
	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
	"github.com/elastic/beats/filebeat/util"
)

func GetDataIdForFormatter(event *util.Data, config *config.TaskConfig) int {
	dataId := config.DataID
	dataid, err := event.Event.GetValue("dataid")
	if err == nil {
		return int(dataid.(int64))
	}
	return dataId
}

func GetFilenameForFormatter(event *util.Data, taskConfig *config.TaskConfig) string {
	state := event.GetState()
	filename := state.Source
	if taskConfig.Type == config.UDP_INPUT {
		address, err := event.Event.GetValue("log.source.address")
		if err != nil {
			address = ""
		}
		address = utils.GetHostName(address.(string))
		filename = address.(string)
	}
	return filename
}
