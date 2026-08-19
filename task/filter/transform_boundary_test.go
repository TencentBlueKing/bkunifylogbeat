// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.

package filter

import (
	"testing"

	"github.com/elastic/beats/filebeat/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/tests"
)

func TestFilterLeavesFieldExtractionForProcessor(t *testing.T) {
	taskConfig, err := config.CreateTaskConfig(map[string]interface{}{
		"dataid": 999993001,
		"field_extraction": map[string]interface{}{
			"pattern": `trace=(?P<traceID>\d+)`,
			"deduplication": map[string]interface{}{
				"enabled": true,
			},
		},
	})
	require.NoError(t, err)
	filter := &Filters{
		Node:           base.NewEmptyNode(taskConfig.FilterID),
		Delimiter:      taskConfig.Delimiter,
		taskConfigMaps: make(map[string]*config.TaskConfig),
	}
	filter.MergeFilterConfig(taskConfig)
	output := make(chan interface{}, 2)
	filter.Outs[taskConfig.ProcessorID] = output

	single := tests.MockLogEvent("/logs/a.log", "trace=123")
	filter.singleFilter(single)
	assert.Same(t, single, (<-output).(*util.Data))
	assert.Equal(t, "trace=123", single.Event.Fields["data"])

	batch := tests.MockLogEvent("/logs/a.log", "")
	batch.Event.Texts = []string{"trace=123", "trace=123"}
	filter.batchFilter(batch)
	assert.Same(t, batch, (<-output).(*util.Data))
	assert.Equal(t, []string{"trace=123", "trace=123"}, batch.Event.Texts)
}
