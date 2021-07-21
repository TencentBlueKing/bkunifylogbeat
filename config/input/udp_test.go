package input

import (
	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/docker/docker/pkg/testutil/assert"
	"github.com/elastic/beats/libbeat/common"
	"testing"
)

func mockUdpTaskConfig(vars map[string]interface{}) (*cfg.TaskConfig, error) {
	rawConfig, err := common.NewConfigFrom(vars)
	if err != nil {
		return nil, err
	}
	config, err := cfg.NewTaskConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func TestUdpNewTaskConfig(t *testing.T) {
	var err error
	var config *cfg.TaskConfig

	vars := map[string]interface{}{
		"dataid": "999990001",
		"host":   "localhost:8080",
		"type":   "udp",
	}

	// 创建任务配置
	config, err = mockUdpTaskConfig(vars)
	if err != nil {
		assert.NilError(t, err)
		return
	}

	// 日志采集默认配置
	taskConfig := map[string]interface{}{}
	config.RawConfig.Unpack(taskConfig)
	assert.Equal(t, taskConfig["host"].(string), "localhost:8080")
	assert.Equal(t, taskConfig["max_message_size"].(string), "128Kib")

	//case 1: 变更close_inactive
	vars["max_message_size"] = "10Kib"

	config, err = mockUdpTaskConfig(vars)
	if err != nil {
		assert.NilError(t, err)
		return
	}
	taskConfig = map[string]interface{}{}
	config.RawConfig.Unpack(taskConfig)
	assert.Equal(t, taskConfig["host"].(string), "localhost:8080")
	assert.Equal(t, taskConfig["max_message_size"].(string), "10Kib")
}
