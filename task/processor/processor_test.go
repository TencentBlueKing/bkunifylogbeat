// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.

package processor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	"github.com/elastic/beats/filebeat/util"
	libbeatlogp "github.com/elastic/beats/libbeat/logp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/task/formatter"
	"github.com/TencentBlueKing/bkunifylogbeat/tests"
)

func init() {
	logp.SetLogger(libbeatlogp.L())
}

func processorTaskConfig(dataID int, deduplicate bool) map[string]interface{} {
	fieldExtraction := map[string]interface{}{
		"pattern": `trace=(?P<traceID>\d+);proc=(?P<proc>[a-z]+)`,
	}
	if deduplicate {
		fieldExtraction["deduplication"] = map[string]interface{}{
			"enabled":        true,
			"window":         "5s",
			"max_keys":       8,
			"max_total_keys": 16,
		}
	}
	return map[string]interface{}{"dataid": dataID, "field_extraction": fieldExtraction}
}

func newDirectProcessor(t testing.TB, taskConfig *config.TaskConfig) (*Processors, *base.TaskNode) {
	t.Helper()
	p := &Processors{Node: base.NewEmptyNode(taskConfig.ProcessorID)}
	require.NoError(t, p.MergeProcessorsConfig(taskConfig))
	taskNode := tests.MockTaskNode(taskConfig)
	p.AddTaskNode(&base.Node{ID: "test-output"}, taskNode)
	return p, taskNode
}

func TestProcessorRunsTransformBeforeLibbeatProcessors(t *testing.T) {
	configMap := processorTaskConfig(999992001, false)
	configMap["processors"] = []map[string]interface{}{
		{
			"drop_event": map[string]interface{}{
				"when": map[string]interface{}{
					"equals": map[string]interface{}{"data": `{"proc":"worker","traceID":"123"}`},
				},
			},
		},
	}
	taskConfig, err := config.CreateTaskConfig(configMap)
	require.NoError(t, err)
	p, taskNode := newDirectProcessor(t, taskConfig)
	droppedBefore := taskNode.CrawlerDropped.Get()

	assert.Nil(t, p.process(tests.MockLogEvent("/logs/a.log", "trace=123;proc=worker")))
	assert.Equal(t, droppedBefore+1, taskNode.CrawlerDropped.Get())

	kept := p.process(tests.MockLogEvent("/logs/a.log", "trace=124;proc=worker"))
	require.NotNil(t, kept)
	assert.JSONEq(t, `{"traceID":"124","proc":"worker"}`, kept.Event.Fields["data"].(string))
}

func TestProcessorRecordsTransformMetricsAndDoesNotMutateInput(t *testing.T) {
	taskConfig, err := config.CreateTaskConfig(processorTaskConfig(999992002, true))
	require.NoError(t, err)
	p, taskNode := newDirectProcessor(t, taskConfig)
	extractBefore := taskNode.ExtractFailed.Get()
	dedupBefore := taskNode.DedupDropped.Get()
	droppedBefore := taskNode.CrawlerDropped.Get()

	failed := p.process(tests.MockLogEvent("/logs/a.log", "not matched"))
	assert.Nil(t, failed)
	input := tests.MockLogEvent("/logs/a.log", "trace=123;proc=worker")
	kept := p.process(input)
	require.NotNil(t, kept)
	assert.NotSame(t, input, kept)
	assert.Equal(t, "trace=123;proc=worker", input.Event.Fields["data"])
	duplicate := p.process(tests.MockLogEvent("/logs/a.log", "trace=123;proc=worker"))
	assert.Nil(t, duplicate)

	assert.Equal(t, extractBefore+1, taskNode.ExtractFailed.Get())
	assert.Equal(t, dedupBefore+1, taskNode.DedupDropped.Get())
	assert.Equal(t, droppedBefore+2, taskNode.CrawlerDropped.Get())
}

func TestProcessorDedupStateIsIsolatedByTaskConfigWithinSameDataID(t *testing.T) {
	firstConfigMap := processorTaskConfig(999992007, true)
	firstConfigMap["package_count"] = 10
	secondConfigMap := processorTaskConfig(999992007, true)
	secondConfigMap["package_count"] = 20
	firstConfig, err := config.CreateTaskConfig(firstConfigMap)
	require.NoError(t, err)
	secondConfig, err := config.CreateTaskConfig(secondConfigMap)
	require.NoError(t, err)
	require.NotEqual(t, firstConfig.ProcessorID, secondConfig.ProcessorID)
	first, _ := newDirectProcessor(t, firstConfig)
	second, _ := newDirectProcessor(t, secondConfig)

	line := "trace=123;proc=worker"
	require.NotNil(t, first.process(tests.MockLogEvent("/logs/a.log", line)))
	require.NotNil(t, second.process(tests.MockLogEvent("/logs/a.log", line)))
	firstDuplicate := first.process(tests.MockLogEvent("/logs/a.log", line))
	secondDuplicate := second.process(tests.MockLogEvent("/logs/a.log", line))
	assert.Nil(t, firstDuplicate)
	assert.Nil(t, secondDuplicate)
}

func TestProcessorTransformsBatchWithNilFields(t *testing.T) {
	taskConfig, err := config.CreateTaskConfig(processorTaskConfig(999992003, true))
	require.NoError(t, err)
	p, taskNode := newDirectProcessor(t, taskConfig)
	data := tests.MockLogEvent("/logs/a.log", "")
	data.Event.Texts = []string{"trace=1;proc=a", "invalid", "trace=1;proc=a", "trace=2;proc=b"}

	processed := p.process(data)
	require.NotNil(t, processed)
	assert.Equal(t, []string{
		`{"proc":"a","traceID":"1"}`,
		`{"proc":"b","traceID":"2"}`,
	}, processed.Event.Texts)
	assert.EqualValues(t, 1, taskNode.ExtractFailed.Get())
	assert.EqualValues(t, 1, taskNode.DedupDropped.Get())
}

func TestProcessorRecordsDedupCapacityObservability(t *testing.T) {
	configMap := processorTaskConfig(999992006, true)
	fieldExtraction := configMap["field_extraction"].(map[string]interface{})
	deduplication := fieldExtraction["deduplication"].(map[string]interface{})
	deduplication["max_total_keys"] = 2
	taskConfig, err := config.CreateTaskConfig(configMap)
	require.NoError(t, err)
	p, taskNode := newDirectProcessor(t, taskConfig)
	evictedBefore := taskNode.DedupEvictedKeys.Get()
	failOpenBefore := taskNode.DedupFailOpen.Get()

	require.NotNil(t, p.process(tests.MockLogEvent("/logs/a.log", "trace=1;proc=a")))
	require.NotNil(t, p.process(tests.MockLogEvent("/logs/a.log", "trace=2;proc=a")))
	require.NotNil(t, p.process(tests.MockLogEvent("/logs/a.log", "trace=3;proc=a")))

	assert.Equal(t, evictedBefore+2, taskNode.DedupEvictedKeys.Get())
	assert.Equal(t, failOpenBefore+1, taskNode.DedupFailOpen.Get())
}

func TestProcessorOutputFeedsV2FormatterItemsData(t *testing.T) {
	taskConfig, err := config.CreateTaskConfig(processorTaskConfig(999992004, false))
	require.NoError(t, err)
	p, _ := newDirectProcessor(t, taskConfig)
	processed := p.process(tests.MockLogEvent("/logs/a.log", "trace=123;proc=worker"))
	require.NotNil(t, processed)

	factory, err := formatter.FindFormatterFactory("v2")
	require.NoError(t, err)
	v2, err := factory(taskConfig)
	require.NoError(t, err)
	formatted := v2.Format([]*util.Data{processed})
	items := formatted["items"].([]beat.MapStr)
	require.Len(t, items, 1)
	assert.JSONEq(t, `{"traceID":"123","proc":"worker"}`, items[0]["data"].(string))
}

func TestProcessorPassesStateEventThroughTransformPipeline(t *testing.T) {
	taskConfig, err := config.CreateTaskConfig(processorTaskConfig(999992005, true))
	require.NoError(t, err)
	p, _ := newDirectProcessor(t, taskConfig)
	state := tests.MockLogEvent("/logs/a.log", "")

	assert.Same(t, state, p.process(state))
}

func TestProcessorDropsWholeBatchWithoutSyntheticState(t *testing.T) {
	taskConfig, err := config.CreateTaskConfig(processorTaskConfig(999992008, true))
	require.NoError(t, err)
	p, _ := newDirectProcessor(t, taskConfig)
	data := tests.MockLogEvent("/logs/a.log", "")
	data.Event.Texts = []string{"invalid", "also invalid"}

	processed := p.process(data)
	assert.Nil(t, processed)
}

func BenchmarkProcessorDataPath(b *testing.B) {
	prefix := strings.Repeat("p", 180) + `"traceID":`
	suffix := strings.Repeat("s", 180)
	line := prefix + `14778626958416894315` + suffix
	data := tests.MockLogEvent("/logs/proc-a.log", line)

	b.Run("normal", func(b *testing.B) {
		taskConfig, err := config.CreateTaskConfig(map[string]interface{}{"dataid": 999992101})
		require.NoError(b, err)
		p, _ := newDirectProcessor(b, taskConfig)
		b.ReportAllocs()
		b.SetBytes(int64(len(line)))
		for i := 0; i < b.N; i++ {
			if p.process(data) == nil {
				b.Fatal("normal event was dropped")
			}
		}
	})

	b.Run("field_extraction", func(b *testing.B) {
		configMap := processorTaskConfig(999992102, false)
		configMap["field_extraction"].(map[string]interface{})["pattern"] = `"traceID":(?P<traceID>\d+)`
		taskConfig, err := config.CreateTaskConfig(configMap)
		require.NoError(b, err)
		p, _ := newDirectProcessor(b, taskConfig)
		b.ReportAllocs()
		b.SetBytes(int64(len(line)))
		for i := 0; i < b.N; i++ {
			if p.process(data) == nil {
				b.Fatal("extracted event was dropped")
			}
		}
	})

	b.Run("field_extraction_and_dedup_miss", func(b *testing.B) {
		const corpusSize = 3 * config.DefaultDeduplicationMaxKeys
		lines := make([]string, corpusSize)
		for index := range lines {
			lines[index] = fmt.Sprintf("%s%d%s", prefix, 14778626958416894315+uint64(index), suffix)
		}
		configMap := processorTaskConfig(999992103, true)
		fieldExtraction := configMap["field_extraction"].(map[string]interface{})
		fieldExtraction["pattern"] = `"traceID":(?P<traceID>\d+)`
		deduplication := fieldExtraction["deduplication"].(map[string]interface{})
		deduplication["max_keys"] = config.DefaultDeduplicationMaxKeys
		deduplication["max_total_keys"] = config.DefaultDeduplicationMaxTotalKeys
		taskConfig, err := config.CreateTaskConfig(configMap)
		require.NoError(b, err)
		p, _ := newDirectProcessor(b, taskConfig)
		b.ReportAllocs()
		b.SetBytes(int64(len(lines[0])))
		for i := 0; i < b.N; i++ {
			data.Event.Fields["data"] = lines[i%corpusSize]
			if p.process(data) == nil {
				b.Fatalf("bounded cache corpus should miss: index=%d corpus_index=%d", i, i%corpusSize)
			}
		}
	})

	b.Run("field_extraction_and_dedup_hit", func(b *testing.B) {
		configMap := processorTaskConfig(999992104, true)
		configMap["field_extraction"].(map[string]interface{})["pattern"] = `"traceID":(?P<traceID>\d+)`
		taskConfig, err := config.CreateTaskConfig(configMap)
		require.NoError(b, err)
		p, _ := newDirectProcessor(b, taskConfig)
		data.Event.Fields["data"] = line
		require.NotNil(b, p.process(data))
		b.ReportAllocs()
		b.SetBytes(int64(len(line)))
		for i := 0; i < b.N; i++ {
			if p.process(data) != nil {
				b.Fatal("duplicate event was not dropped")
			}
		}
	})
}

func BenchmarkProcessorNodeDataPath(b *testing.B) {
	prefix := strings.Repeat("p", 180) + `"traceID":`
	suffix := strings.Repeat("s", 180)
	line := prefix + `14778626958416894315` + suffix

	benchmarkNode := func(b *testing.B, taskConfig *config.TaskConfig, lines []string) {
		p, _ := newDirectProcessor(b, taskConfig)
		output := base.NewEmptyNode("benchmark-output")
		p.AddOutput(output)
		go p.Run()
		b.Cleanup(func() {
			p.CloseOnce.Do(func() { close(p.End) })
			p.WaitUntilGameOver()
		})
		data := tests.MockLogEvent("/logs/proc-a.log", lines[0])
		b.ReportAllocs()
		b.SetBytes(int64(len(lines[0])))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			data.Event.Fields["data"] = lines[i%len(lines)]
			p.In <- data
			if (<-output.In).(*util.Data) == nil {
				b.Fatal("event was dropped")
			}
		}
	}

	b.Run("normal", func(b *testing.B) {
		taskConfig, err := config.CreateTaskConfig(map[string]interface{}{"dataid": 999992111})
		require.NoError(b, err)
		benchmarkNode(b, taskConfig, []string{line})
	})

	b.Run("field_extraction", func(b *testing.B) {
		configMap := processorTaskConfig(999992112, false)
		configMap["field_extraction"].(map[string]interface{})["pattern"] = `"traceID":(?P<traceID>\d+)`
		taskConfig, err := config.CreateTaskConfig(configMap)
		require.NoError(b, err)
		benchmarkNode(b, taskConfig, []string{line})
	})

	b.Run("field_extraction_and_dedup_miss", func(b *testing.B) {
		const corpusSize = 3 * config.DefaultDeduplicationMaxKeys
		lines := make([]string, corpusSize)
		for index := range lines {
			lines[index] = fmt.Sprintf("%s%d%s", prefix, 14778626958416894315+uint64(index), suffix)
		}
		configMap := processorTaskConfig(999992113, true)
		fieldExtraction := configMap["field_extraction"].(map[string]interface{})
		fieldExtraction["pattern"] = `"traceID":(?P<traceID>\d+)`
		deduplication := fieldExtraction["deduplication"].(map[string]interface{})
		deduplication["max_keys"] = config.DefaultDeduplicationMaxKeys
		deduplication["max_total_keys"] = config.DefaultDeduplicationMaxTotalKeys
		taskConfig, err := config.CreateTaskConfig(configMap)
		require.NoError(b, err)
		benchmarkNode(b, taskConfig, lines)
	})
}
