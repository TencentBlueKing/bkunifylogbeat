// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company. All rights reserved.
// Licensed under the MIT License.

package input

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
)

func TestAdaptiveScanMetadataFromTask(t *testing.T) {
	taskConfig := &config.TaskConfig{
		ID:      "task-a",
		InputID: "input-a",
		DataID:  1001,
		Paths:   []string{"/var/log/app.log"},
	}

	assert.Equal(t, utils.AdaptiveScanInputMetadata{
		InputID: "input-a",
		TaskID:  "task-a",
		DataID:  1001,
		Paths:   []string{"/var/log/app.log"},
	}, adaptiveScanMetadataFromTask(taskConfig))
}
