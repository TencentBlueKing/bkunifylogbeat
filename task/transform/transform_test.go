// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.

package transform

import (
	stdjson "encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/tests"
)

var benchmarkIndexSink int

func extractionConfig(dataID int, pattern string, deduplicate bool) map[string]interface{} {
	fieldExtraction := map[string]interface{}{"pattern": pattern}
	if deduplicate {
		fieldExtraction["deduplication"] = map[string]interface{}{
			"enabled":        true,
			"window":         "5s",
			"max_keys":       1024,
			"max_total_keys": 2048,
		}
	}
	return map[string]interface{}{"dataid": dataID, "field_extraction": fieldExtraction}
}

func newPipeline(t testing.TB, dataID int, pattern string, deduplicate bool) *Pipeline {
	t.Helper()
	taskConfig, err := config.CreateTaskConfig(extractionConfig(dataID, pattern, deduplicate))
	require.NoError(t, err)
	return NewPipeline(taskConfig)
}

func TestPipelineProjectsOrderedJSONWithoutMutatingInput(t *testing.T) {
	pipeline := newPipeline(t, 999991001, `trace=(?P<traceID>[^;]+);proc=(?P<ProcName>.+)`, false)
	data := tests.MockLogEvent("/logs/proc-a.log", `trace=12"34;proc=a\\b`)
	data.Event.Fields["untouched"] = "value"

	outcome := pipeline.Apply(data)
	require.NotNil(t, outcome.Data)
	assert.NotSame(t, data, outcome.Data)
	assert.Equal(t, `trace=12"34;proc=a\\b`, data.Event.Fields["data"])
	assert.Equal(t, `{"traceID":"12\"34","ProcName":"a\\\\b"}`, outcome.Data.Event.Fields["data"])
	assert.Equal(t, "value", outcome.Data.Event.Fields["untouched"])
	assert.Equal(t, data.GetState().Source, outcome.Data.GetState().Source)
}

func TestPipelineDropsExtractionFailureAndDuplicate(t *testing.T) {
	pipeline := newPipeline(t, 999991002, `"traceID":(?P<traceID>\d+)`, true)

	failed := pipeline.Apply(tests.MockLogEvent("/logs/proc-a.log", `no trace here`))
	assert.Nil(t, failed.Data)
	assert.EqualValues(t, 1, failed.ExtractFailed)

	first := pipeline.Apply(tests.MockLogEvent("/logs/proc-a.log", `{"traceID":123}`))
	require.NotNil(t, first.Data)
	assert.Equal(t, `{"traceID":"123"}`, first.Data.Event.Fields["data"])

	duplicate := pipeline.Apply(tests.MockLogEvent("/logs/proc-a.log", `prefix "traceID":123 suffix`))
	assert.Nil(t, duplicate.Data)
	assert.EqualValues(t, 1, duplicate.DedupDropped)

	otherSource := pipeline.Apply(tests.MockLogEvent("/logs/proc-b.log", `{"traceID":123}`))
	require.NotNil(t, otherSource.Data)
}

func TestPipelineTreatsMissingOrNonStringDataAsExtractionFailure(t *testing.T) {
	pipeline := newPipeline(t, 999991005, `trace=(?P<traceID>\d+)`, false)
	missing := tests.MockLogEvent("/logs/proc-a.log", "placeholder")
	delete(missing.Event.Fields, "data")
	nonString := tests.MockLogEvent("/logs/proc-a.log", "placeholder")
	nonString.Event.Fields["data"] = 123

	missingOutcome := pipeline.Apply(missing)
	assert.Nil(t, missingOutcome.Data)
	assert.EqualValues(t, 1, missingOutcome.ExtractFailed)
	nonStringOutcome := pipeline.Apply(nonString)
	assert.Nil(t, nonStringOutcome.Data)
	assert.EqualValues(t, 1, nonStringOutcome.ExtractFailed)
}

func TestPipelineTransformsBatchAndCountsEachDroppedLine(t *testing.T) {
	pipeline := newPipeline(t, 999991003, `trace=(?P<traceID>\d+)`, true)
	data := tests.MockLogEvent("/logs/proc-a.log", "")
	data.Event.Texts = []string{"trace=1", "trace=1 duplicate", "no trace", "trace=2"}

	outcome := pipeline.Apply(data)
	require.NotNil(t, outcome.Data)
	assert.Equal(t, []string{`{"traceID":"1"}`, `{"traceID":"2"}`}, outcome.Data.Event.Texts)
	assert.Equal(t, []string{"trace=1", "trace=1 duplicate", "no trace", "trace=2"}, data.Event.Texts)
	assert.EqualValues(t, 1, outcome.ExtractFailed)
	assert.EqualValues(t, 1, outcome.DedupDropped)
}

func TestDigitCaptureFastPathMatchesRegexp(t *testing.T) {
	testCases := []struct {
		pattern string
		texts   []string
	}{
		{pattern: `(?P<id>\d+)`, texts: []string{"abc123def", "abc", "12x34"}},
		{pattern: `trace=(?P<id>\d+)`, texts: []string{"x trace=123 y", "trace=x", "trace=1 trace=2"}},
		{pattern: `^trace=(?P<id>\d+)`, texts: []string{"trace=123", "xtrace=123", "trace=123x"}},
	}
	for _, testCase := range testCases {
		re := regexp.MustCompile(testCase.pattern)
		fast := newDigitCaptureExtractor(testCase.pattern, "id")
		require.NotNil(t, fast, testCase.pattern)
		for _, text := range testCase.texts {
			matches := re.FindStringSubmatch(text)
			wantOK := matches != nil
			want := ""
			if wantOK {
				want = matches[1]
			}
			got, gotOK := fast.extract(text)
			assert.Equal(t, wantOK, gotOK, "pattern=%s text=%s", testCase.pattern, text)
			assert.Equal(t, want, got, "pattern=%s text=%s", testCase.pattern, text)
		}
	}
	assert.Nil(t, newDigitCaptureExtractor(`^trace=(?P<id>\d+)$`, "id"))
	assert.Nil(t, newDigitCaptureExtractor(`x(?P<id>\d+)2`, "id"))
}

func TestOrderedFieldValuesAlwaysProducesValidJSON(t *testing.T) {
	data, err := orderedFieldValues{
		names:  []string{"control", "invalid"},
		values: []string{"line\x00\n\tend", string([]byte{'a', 0xff, 'b'})},
	}.MarshalJSON()
	require.NoError(t, err)
	require.True(t, stdjson.Valid(data), string(data))
	decoded := make(map[string]string)
	require.NoError(t, stdjson.Unmarshal(data, &decoded))
	assert.Equal(t, "line\x00\n\tend", decoded["control"])
	assert.Equal(t, "a�b", decoded["invalid"])
}

func TestExtractionFailsWhenNamedCaptureDidNotParticipate(t *testing.T) {
	pipeline := newPipeline(t, 999991004, `(?P<traceID>\d+)(?:-(?P<proc>[a-z]+))?`, false)
	failed := pipeline.Apply(tests.MockLogEvent("/logs/proc-a.log", "123"))
	assert.Nil(t, failed.Data)
	assert.EqualValues(t, 1, failed.ExtractFailed)
	kept := pipeline.Apply(tests.MockLogEvent("/logs/proc-a.log", "123-worker"))
	require.NotNil(t, kept.Data)
	assert.Equal(t, `{"traceID":"123","proc":"worker"}`, kept.Data.Event.Fields["data"])
}

func TestWindowDeduperRetainsPreviousGenerationAndReportsCapacityEviction(t *testing.T) {
	deduper := newWindowDeduper(&config.Deduplication{Window: 5 * time.Second, MaxKeys: 2, MaxTotalKeys: 4})
	now := time.Unix(100, 0)
	deduper.now = func() time.Time { return now }

	duplicate, _ := deduper.seen("/a.log", []string{"1"})
	assert.False(t, duplicate)
	duplicate, _ = deduper.seen("/a.log", []string{"1"})
	assert.True(t, duplicate)
	now = now.Add(6 * time.Second)
	duplicate, _ = deduper.seen("/a.log", []string{"2"})
	assert.False(t, duplicate)
	duplicate, _ = deduper.seen("/a.log", []string{"1"})
	assert.True(t, duplicate, "previous generation must still deduplicate")
	now = now.Add(6 * time.Second)
	duplicate, _ = deduper.seen("/a.log", []string{"1"})
	assert.False(t, duplicate, "two rotations expire the oldest generation")

	_, _ = deduper.seen("/b.log", []string{"1"})
	_, _ = deduper.seen("/c.log", []string{"1"})
	_, stats := deduper.seen("/d.log", []string{"1"})
	assert.Positive(t, stats.evictedKeys)
	assert.LessOrEqual(t, deduper.totalKeys, deduper.maxTotalKeys)
}

func TestWindowDeduperFailsOpenWhenSingleSourceFillsTotalCap(t *testing.T) {
	deduper := newWindowDeduper(&config.Deduplication{Window: 5 * time.Second, MaxKeys: 8, MaxTotalKeys: 2})
	now := time.Unix(100, 0)
	deduper.now = func() time.Time { return now }
	_, _ = deduper.seen("/a.log", []string{"1"})
	_, _ = deduper.seen("/a.log", []string{"2"})
	duplicate, stats := deduper.seen("/a.log", []string{"3"})
	assert.False(t, duplicate)
	assert.EqualValues(t, 2, stats.evictedKeys)
	assert.EqualValues(t, 1, stats.failOpen)
	assert.LessOrEqual(t, deduper.totalKeys, deduper.maxTotalKeys)
}

func TestWindowDeduperFramesMultipleValues(t *testing.T) {
	deduper := newWindowDeduper(&config.Deduplication{Window: 5 * time.Second, MaxKeys: 8, MaxTotalKeys: 16})
	now := time.Unix(100, 0)
	deduper.now = func() time.Time { return now }
	first, _ := deduper.seen("/a.log", []string{"ab", "c"})
	second, _ := deduper.seen("/a.log", []string{"a", "bc"})
	third, _ := deduper.seen("/a.log", []string{"ab", "c"})
	assert.False(t, first)
	assert.False(t, second)
	assert.True(t, third)
}

func TestWindowDeduperHighCardinalityCorpusMissesAfterCapacityRotation(t *testing.T) {
	const corpusSize = 3 * config.DefaultDeduplicationMaxKeys
	deduper := newWindowDeduper(&config.Deduplication{
		Window:       time.Hour,
		MaxKeys:      config.DefaultDeduplicationMaxKeys,
		MaxTotalKeys: config.DefaultDeduplicationMaxTotalKeys,
	})
	now := time.Unix(100, 0)
	deduper.now = func() time.Time { return now }
	for index := 0; index < 100000; index++ {
		duplicate, _ := deduper.seen("/logs/a.log", []string{strconv.Itoa(index % corpusSize)})
		assert.False(t, duplicate, "index=%d corpus_index=%d", index, index%corpusSize)
		if duplicate {
			break
		}
	}
}

func BenchmarkTransformPipeline(b *testing.B) {
	prefix := strings.Repeat("p", 180) + `"traceID":`
	suffix := strings.Repeat("s", 180)
	line := prefix + `14778626958416894315` + suffix
	data := tests.MockLogEvent("/logs/proc-a.log", line)

	b.Run("normal_passthrough", func(b *testing.B) {
		var pipeline *Pipeline
		b.ReportAllocs()
		b.SetBytes(int64(len(line)))
		for i := 0; i < b.N; i++ {
			if pipeline.Apply(data).Data == nil {
				b.Fatal("line was dropped")
			}
		}
	})

	b.Run("field_extraction", func(b *testing.B) {
		pipeline := newPipeline(b, 999991101, `"traceID":(?P<traceID>\d+)`, false)
		b.ReportAllocs()
		b.SetBytes(int64(len(line)))
		for i := 0; i < b.N; i++ {
			if pipeline.Apply(data).Data == nil {
				b.Fatal("line was dropped")
			}
		}
	})

	b.Run("field_extraction_and_dedup_miss", func(b *testing.B) {
		const corpusSize = 4096
		lines := make([]string, corpusSize)
		for index := range lines {
			lines[index] = fmt.Sprintf("%s%d%s", prefix, 14778626958416894315+uint64(index), suffix)
		}
		pipeline := newPipeline(b, 999991102, `"traceID":(?P<traceID>\d+)`, true)
		b.ReportAllocs()
		b.SetBytes(int64(len(lines[0])))
		for i := 0; i < b.N; i++ {
			data.Event.Fields["data"] = lines[i&(corpusSize-1)]
			if pipeline.Apply(data).Data == nil {
				b.Fatal("bounded cache corpus should miss")
			}
		}
	})

	b.Run("field_extraction_and_dedup_hit", func(b *testing.B) {
		pipeline := newPipeline(b, 999991103, `"traceID":(?P<traceID>\d+)`, true)
		require.NotNil(b, pipeline.Apply(data).Data)
		b.ReportAllocs()
		b.SetBytes(int64(len(line)))
		for i := 0; i < b.N; i++ {
			if pipeline.Apply(data).DedupDropped != 1 {
				b.Fatal("line should hit dedup cache")
			}
		}
	})

	b.Run("literal_scan_baseline", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(line)))
		for i := 0; i < b.N; i++ {
			benchmarkIndexSink = strings.Index(line, `"traceID":`)
		}
	})
}

func BenchmarkDedupCacheDefaultCapacityFootprint(b *testing.B) {
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	deduper := newWindowDeduper(&config.Deduplication{
		Window:       config.DefaultDeduplicationWindow,
		MaxKeys:      config.DefaultDeduplicationMaxKeys,
		MaxTotalKeys: config.DefaultDeduplicationMaxTotalKeys,
	})
	sources := []string{
		"/logs/proc-a.log", "/logs/proc-b.log", "/logs/proc-c.log", "/logs/proc-d.log",
		"/logs/proc-e.log", "/logs/proc-f.log", "/logs/proc-g.log", "/logs/proc-h.log",
	}
	for index := 0; index < config.DefaultDeduplicationMaxTotalKeys; index++ {
		duplicate, _ := deduper.seen(sources[index&(len(sources)-1)], []string{strconv.Itoa(index)})
		if duplicate {
			b.Fatal("unique key was deduplicated")
		}
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(deduper)
	retained := after.HeapAlloc - before.HeapAlloc
	b.ReportMetric(float64(retained), "retained-B")
	b.ReportMetric(float64(retained)/float64(config.DefaultDeduplicationMaxTotalKeys), "retained-B/key")
}
