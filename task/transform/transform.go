// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.
// --------------------------------------------------------------------
// Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
// documentation files (the "Software"), to deal in the Software without restriction, including without limitation
// the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software,
// and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all copies or substantial
// portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT
// LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN
// NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
// WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
// SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

package transform

import (
	"encoding/binary"
	"regexp"
	"time"

	"github.com/bytedance/sonic"
	"github.com/cespare/xxhash/v2"
	"github.com/elastic/beats/filebeat/util"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
)

type result uint8

const (
	kept result = iota
	extractFailed
	deduplicated
)

// Outcome describes one pipeline invocation. Dropped counts are per log line,
// including each item in a batched event.
type Outcome struct {
	Data          *util.Data
	ExtractFailed int64
	DedupDropped  int64
	EvictedKeys   int64
	FailOpen      int64
}

// Pipeline applies field extraction, JSON projection, and optional
// bounded-window deduplication before libbeat processors run.
type Pipeline struct {
	extractor *fieldExtractor
	values    []string
	fields    map[string]string
	deduper   *windowDeduper
}

// NewPipeline creates a transform pipeline when field_extraction is configured.
func NewPipeline(taskConfig *config.TaskConfig) *Pipeline {
	if taskConfig.FieldExtraction == nil {
		return nil
	}

	extractor := newFieldExtractor(taskConfig)
	pipeline := &Pipeline{
		extractor: extractor,
		values:    make([]string, len(extractor.names)),
		fields:    make(map[string]string, len(extractor.names)),
	}
	if deduplication := taskConfig.EnabledDeduplication(); deduplication != nil {
		pipeline.deduper = newWindowDeduper(deduplication)
	}
	return pipeline
}

// Apply returns a cloned Data object when projection succeeds. The input is
// never mutated so a shared Filter output can safely fan out to other branches.
func (p *Pipeline) Apply(data *util.Data) Outcome {
	if p == nil {
		return Outcome{Data: data}
	}

	source := data.GetState().Source
	if data.Event.HasTexts() {
		texts := data.Event.GetTexts()
		transformed := make([]string, 0, len(texts))
		outcome := Outcome{}
		for _, text := range texts {
			projected, transformResult, stats := p.transform(source, text)
			outcome.EvictedKeys += stats.evictedKeys
			outcome.FailOpen += stats.failOpen
			switch transformResult {
			case kept:
				transformed = append(transformed, projected)
			case extractFailed:
				outcome.ExtractFailed++
			case deduplicated:
				outcome.DedupDropped++
			}
		}
		if len(transformed) == 0 {
			return outcome
		}
		event := data.GetEvent()
		event.Texts = transformed
		outcome.Data = &util.Data{Event: event}
		outcome.Data.SetState(data.GetState())
		return outcome
	}
	if data.Event.Fields == nil {
		return Outcome{Data: data}
	}

	text, ok := data.Event.Fields["data"].(string)
	if !ok {
		return Outcome{ExtractFailed: 1}
	}
	projected, transformResult, stats := p.transform(source, text)
	outcome := Outcome{EvictedKeys: stats.evictedKeys, FailOpen: stats.failOpen}
	switch transformResult {
	case extractFailed:
		outcome.ExtractFailed = 1
		return outcome
	case deduplicated:
		outcome.DedupDropped = 1
		return outcome
	}

	event := data.GetEvent()
	event.Fields = event.Fields.Clone()
	event.Fields["data"] = projected
	outcome.Data = &util.Data{Event: event}
	outcome.Data.SetState(data.GetState())
	return outcome
}

func (p *Pipeline) transform(source, text string) (string, result, dedupStats) {
	if !p.extractor.extract(text, p.values) {
		return "", extractFailed, dedupStats{}
	}
	var stats dedupStats
	if p.deduper != nil {
		var duplicate bool
		duplicate, stats = p.deduper.seen(source, p.values)
		if duplicate {
			return "", deduplicated, stats
		}
	}

	for index, name := range p.extractor.names {
		p.fields[name] = p.values[index]
	}
	data, err := sonic.ConfigStd.MarshalToString(p.fields)
	if err != nil {
		return "", extractFailed, stats
	}
	return data, kept, stats
}

type fieldExtractor struct {
	re    *regexp.Regexp
	names []string
}

func newFieldExtractor(taskConfig *config.TaskConfig) *fieldExtractor {
	return &fieldExtractor{
		re:    taskConfig.FieldExtractionRegexp(),
		names: taskConfig.FieldExtractionCaptureNames(),
	}
}

func (e *fieldExtractor) extract(text string, values []string) bool {
	indexes := e.re.FindStringSubmatchIndex(text)
	if indexes == nil {
		return false
	}
	for outputIndex := range e.names {
		captureIndex := outputIndex + 1
		start := indexes[captureIndex*2]
		end := indexes[captureIndex*2+1]
		if start < 0 || end < 0 {
			return false
		}
		values[outputIndex] = text[start:end]
	}
	return true
}

type sourceWindow struct {
	current   map[uint64]struct{}
	previous  map[uint64]struct{}
	rotatedAt time.Time
	lastSeen  time.Time
}

type windowDeduper struct {
	window       time.Duration
	maxKeys      int
	maxTotalKeys int
	sources      map[string]*sourceWindow
	totalKeys    int
	nextSweep    time.Time
	now          func() time.Time
	digest       *xxhash.Digest
	lengthBuffer [8]byte
}

type dedupStats struct {
	evictedKeys int64
	failOpen    int64
}

func newWindowDeduper(deduplication *config.Deduplication) *windowDeduper {
	return &windowDeduper{
		window:       deduplication.Window,
		maxKeys:      deduplication.MaxKeys,
		maxTotalKeys: deduplication.MaxTotalKeys,
		sources:      make(map[string]*sourceWindow),
		now:          time.Now,
		digest:       xxhash.New(),
	}
}

func (d *windowDeduper) seen(source string, values []string) (bool, dedupStats) {
	stats := dedupStats{}
	now := d.now()
	if d.nextSweep.IsZero() || !now.Before(d.nextSweep) {
		d.sweep(now)
		d.nextSweep = now.Add(d.window)
	}

	state := d.sources[source]
	if state == nil {
		state = &sourceWindow{current: make(map[uint64]struct{}), rotatedAt: now, lastSeen: now}
		d.sources[source] = state
	} else {
		d.rotate(state, now)
		state.lastSeen = now
	}

	key := d.hash(source, values)
	if _, ok := state.current[key]; ok {
		return true, stats
	}
	if _, ok := state.previous[key]; ok {
		return true, stats
	}

	if len(state.current) >= d.maxKeys {
		stats.evictedKeys += int64(len(state.previous))
		d.totalKeys -= len(state.previous)
		state.previous = state.current
		state.current = make(map[uint64]struct{})
		state.rotatedAt = now
	}
	if d.totalKeys >= d.maxTotalKeys {
		var capacityStats dedupStats
		state, capacityStats = d.makeRoom(source, state, now)
		stats.evictedKeys += capacityStats.evictedKeys
		stats.failOpen += capacityStats.failOpen
	}

	state.current[key] = struct{}{}
	d.totalKeys++
	return false, stats
}

func (d *windowDeduper) rotate(state *sourceWindow, now time.Time) {
	elapsed := now.Sub(state.rotatedAt)
	if elapsed < d.window {
		return
	}

	if elapsed/d.window < 2 {
		d.totalKeys -= len(state.previous)
		state.previous = state.current
	} else {
		d.totalKeys -= len(state.previous) + len(state.current)
		state.previous = nil
	}
	state.current = make(map[uint64]struct{})
	state.rotatedAt = now
}

func (d *windowDeduper) makeRoom(source string, state *sourceWindow, now time.Time) (*sourceWindow, dedupStats) {
	var oldestSource string
	var oldestTime time.Time
	foundOldest := false
	for candidateSource, candidate := range d.sources {
		if candidateSource == source || len(candidate.current)+len(candidate.previous) == 0 {
			continue
		}
		if !foundOldest || candidate.lastSeen.Before(oldestTime) {
			oldestSource = candidateSource
			oldestTime = candidate.lastSeen
			foundOldest = true
		}
	}
	if foundOldest {
		candidate := d.sources[oldestSource]
		evicted := len(candidate.current) + len(candidate.previous)
		d.totalKeys -= evicted
		delete(d.sources, oldestSource)
		return state, dedupStats{evictedKeys: int64(evicted)}
	}

	if len(state.previous) > 0 {
		evicted := len(state.previous)
		d.totalKeys -= evicted
		state.previous = nil
		return state, dedupStats{evictedKeys: int64(evicted)}
	}

	// A single source can fill a deliberately small task-level cap. Resetting
	// its current generation fails open for duplicates while preserving the cap.
	evicted := len(state.current)
	d.totalKeys -= evicted
	state.current = make(map[uint64]struct{})
	state.rotatedAt = now
	return state, dedupStats{evictedKeys: int64(evicted), failOpen: 1}
}

func (d *windowDeduper) sweep(now time.Time) {
	for source, state := range d.sources {
		d.rotate(state, now)
		inactive := now.Sub(state.lastSeen)
		if inactive >= d.window && inactive/d.window >= 2 && len(state.current)+len(state.previous) == 0 {
			delete(d.sources, source)
		}
	}
}

func (d *windowDeduper) hash(source string, values []string) uint64 {
	d.digest.Reset()
	d.writeFramed(source)
	for _, value := range values {
		d.writeFramed(value)
	}
	return d.digest.Sum64()
}

func (d *windowDeduper) writeFramed(value string) {
	binary.LittleEndian.PutUint64(d.lengthBuffer[:], uint64(len(value)))
	_, _ = d.digest.Write(d.lengthBuffer[:])
	_, _ = d.digest.WriteString(value)
}
