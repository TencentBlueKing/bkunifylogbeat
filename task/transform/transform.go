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
	"regexp/syntax"
	"strings"
	"time"
	"unicode/utf8"

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

// Pipeline applies field extraction, ordered JSON projection, and optional
// bounded-window deduplication before libbeat processors run.
type Pipeline struct {
	extractor *fieldExtractor
	values    []string
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

	data, err := orderedFieldValues{names: p.extractor.names, values: p.values}.MarshalJSON()
	if err != nil {
		return "", extractFailed, stats
	}
	return string(data), kept, stats
}

// orderedFieldValues emits an object in capture-group order without allocating
// a map for every input line.
type orderedFieldValues struct {
	names  []string
	values []string
}

func (f orderedFieldValues) MarshalJSON() ([]byte, error) {
	capacity := 2
	for index := range f.names {
		capacity += len(f.names[index]) + len(f.values[index]) + 6
	}

	data := make([]byte, 0, capacity)
	data = append(data, '{')
	for index := range f.names {
		if index > 0 {
			data = append(data, ',')
		}
		data = appendJSONString(data, f.names[index])
		data = append(data, ':')
		data = appendJSONString(data, f.values[index])
	}
	data = append(data, '}')
	return data, nil
}

func appendJSONString(data []byte, value string) []byte {
	const hex = "0123456789abcdef"

	data = append(data, '"')
	start := 0
	for index := 0; index < len(value); {
		character := value[index]
		if character >= utf8.RuneSelf {
			_, size := utf8.DecodeRuneInString(value[index:])
			if size > 1 {
				index += size
				continue
			}
			data = append(data, value[start:index]...)
			data = append(data, `\ufffd`...)
			index++
			start = index
			continue
		}

		var escaped byte
		switch character {
		case '\\', '"':
			escaped = character
		case '\b':
			escaped = 'b'
		case '\f':
			escaped = 'f'
		case '\n':
			escaped = 'n'
		case '\r':
			escaped = 'r'
		case '\t':
			escaped = 't'
		default:
			if character >= 0x20 {
				index++
				continue
			}
		}

		data = append(data, value[start:index]...)
		if escaped != 0 {
			data = append(data, '\\', escaped)
		} else {
			data = append(data, '\\', 'u', '0', '0', hex[character>>4], hex[character&0x0f])
		}
		index++
		start = index
	}
	data = append(data, value[start:]...)
	data = append(data, '"')
	return data
}

type fieldExtractor struct {
	re             *regexp.Regexp
	names          []string
	captureIndexes []int
	fastDigits     *digitCaptureExtractor
}

func newFieldExtractor(taskConfig *config.TaskConfig) *fieldExtractor {
	extractor := &fieldExtractor{
		re:             taskConfig.FieldExtractionRegexp(),
		names:          taskConfig.FieldExtractionCaptureNames(),
		captureIndexes: taskConfig.FieldExtractionCaptureIndexes(),
	}
	if len(extractor.names) == 1 && extractor.re.NumSubexp() == 1 {
		extractor.fastDigits = newDigitCaptureExtractor(taskConfig.FieldExtraction.Pattern, extractor.names[0])
	}
	return extractor
}

func (e *fieldExtractor) extract(text string, values []string) bool {
	if e.fastDigits != nil {
		value, ok := e.fastDigits.extract(text)
		if !ok {
			return false
		}
		values[0] = value
		return true
	}

	indexes := e.re.FindStringSubmatchIndex(text)
	if indexes == nil {
		return false
	}
	for outputIndex, captureIndex := range e.captureIndexes {
		start := indexes[captureIndex*2]
		end := indexes[captureIndex*2+1]
		if start < 0 || end < 0 {
			return false
		}
		values[outputIndex] = text[start:end]
	}
	return true
}

// digitCaptureExtractor recognizes the hot DFM shape: an optional literal
// prefix followed by one named [0-9]+ capture. Other regular expressions keep
// the general RE2 path above so the optimization cannot change their meaning.
type digitCaptureExtractor struct {
	prefix   string
	anchored bool
}

func newDigitCaptureExtractor(pattern, captureName string) *digitCaptureExtractor {
	expression, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil
	}

	parts := []*syntax.Regexp{expression}
	if expression.Op == syntax.OpConcat {
		parts = expression.Sub
	}

	extractor := &digitCaptureExtractor{}
	foundCapture := false
	for index, part := range parts {
		switch part.Op {
		case syntax.OpBeginText:
			if index != 0 {
				return nil
			}
			extractor.anchored = true
		case syntax.OpEndText:
			return nil
		case syntax.OpLiteral:
			if part.Flags&syntax.FoldCase != 0 {
				return nil
			}
			if foundCapture {
				return nil
			} else {
				extractor.prefix += string(part.Rune)
			}
		case syntax.OpCapture:
			if foundCapture || part.Name != captureName || !isASCIIDigitPlus(part) {
				return nil
			}
			foundCapture = true
		default:
			return nil
		}
	}
	if !foundCapture {
		return nil
	}
	return extractor
}

func isASCIIDigitPlus(capture *syntax.Regexp) bool {
	if len(capture.Sub) != 1 || capture.Sub[0].Op != syntax.OpPlus || len(capture.Sub[0].Sub) != 1 {
		return false
	}
	class := capture.Sub[0].Sub[0]
	return class.Op == syntax.OpCharClass && len(class.Rune) == 2 && class.Rune[0] == '0' && class.Rune[1] == '9'
}

func (e *digitCaptureExtractor) extract(text string) (string, bool) {
	searchFrom := 0
	for searchFrom <= len(text) {
		matchStart := searchFrom
		captureStart := searchFrom
		if e.prefix != "" {
			relative := strings.Index(text[searchFrom:], e.prefix)
			if relative < 0 {
				return "", false
			}
			matchStart = searchFrom + relative
			if e.anchored && matchStart != 0 {
				return "", false
			}
			captureStart = matchStart + len(e.prefix)
		} else if e.anchored {
			if searchFrom != 0 {
				return "", false
			}
			captureStart = 0
		} else {
			for captureStart < len(text) && (text[captureStart] < '0' || text[captureStart] > '9') {
				captureStart++
			}
			matchStart = captureStart
		}

		captureEnd := captureStart
		for captureEnd < len(text) && text[captureEnd] >= '0' && text[captureEnd] <= '9' {
			captureEnd++
		}
		if captureEnd == captureStart {
			if e.anchored || matchStart >= len(text) {
				return "", false
			}
			searchFrom = matchStart + 1
			continue
		}

		return text[captureStart:captureEnd], true
	}
	return "", false
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
