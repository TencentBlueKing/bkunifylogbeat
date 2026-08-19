// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.
//
// License for bkunifylogbeat 蓝鲸日志采集器:
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

package filter

import (
	"strings"
	"sync"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	bkmonitoring "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/monitoring"
	"github.com/elastic/beats/filebeat/util"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/task/processor"
)

var (
	filterMaps = map[string]*Filters{}
	mtx        sync.RWMutex

	numOfFilterTotal = bkmonitoring.NewInt("num_filter_total") // 当前全局filter的数量

	filterDroppedTotal = bkmonitoring.NewInt("filter_dropped_total") // 被过滤的总数
	filterHandledTotal = bkmonitoring.NewInt("filter_handled_total") // 被处理的总数
)

type filterBranchEntry struct {
	processorID string
	taskConfig  *config.TaskConfig
}

type Filters struct {
	*base.Node

	Delimiter      string
	filterMaxIndex int

	taskConfigMaps map[string]*config.TaskConfig
	branchSnapshot []filterBranchEntry
	configMutex    sync.RWMutex
}

// GetFilters get filter
func GetFilters(taskCfg *config.TaskConfig, taskNode *base.TaskNode) (*Filters, error) {
	var (
		ok  bool
		fil *Filters
	)

	func() {
		mtx.RLock()
		defer mtx.RUnlock()
		fil, ok = filterMaps[taskCfg.FilterID]
	}()

	if ok {
		p, err := processor.GetProcessors(taskCfg, taskNode)
		if err != nil {
			return nil, err
		}

		fil.MergeFilterConfig(taskCfg)
		fil.AddOutput(p.Node)
		fil.AddTaskNode(p.Node, taskNode)
		return fil, nil
	}
	return NewFilters(taskCfg, taskNode)
}

func NewFilters(taskCfg *config.TaskConfig, taskNode *base.TaskNode) (*Filters, error) {
	var err error
	var fil = &Filters{
		Node:      base.NewEmptyNode(taskCfg.FilterID),
		Delimiter: taskCfg.Delimiter,

		taskConfigMaps: map[string]*config.TaskConfig{},
	}
	fil.MergeFilterConfig(taskCfg)

	p, err := processor.NewProcessors(taskCfg, taskNode)
	if err != nil {
		return nil, err
	}
	fil.AddOutput(p.Node)
	fil.AddTaskNode(p.Node, taskNode)

	go fil.Run()

	logp.L.Infof("add filter(%s) to global filterMaps", fil.ID)
	mtx.Lock()
	defer mtx.Unlock()
	filterMaps[taskCfg.FilterID] = fil
	numOfFilterTotal.Add(1)
	return fil, nil
}

// RemoveFilter : 移除全局缓存
func RemoveFilter(id string) {
	logp.L.Infof("remove filter(%s) in global filterMaps", id)
	mtx.Lock()
	defer mtx.Unlock()
	delete(filterMaps, id)
	numOfFilterTotal.Add(-1)
}

func (f *Filters) MergeFilterConfig(taskCfg *config.TaskConfig) {
	f.configMutex.Lock()
	defer f.configMutex.Unlock()

	if taskCfg.HasFilter {
		for _, filConfig := range taskCfg.Filters {
			if len(filConfig.Conditions) != 0 {
				maxIndex := filConfig.Conditions[len(filConfig.Conditions)-1].Index
				if f.filterMaxIndex < maxIndex {
					f.filterMaxIndex = maxIndex
				}
			}
		}
	}
	f.OutsLock.RLock()
	_, outputExists := f.Outs[taskCfg.ProcessorID]
	f.OutsLock.RUnlock()
	if _, ok := f.taskConfigMaps[taskCfg.ProcessorID]; !ok || !outputExists {
		f.taskConfigMaps[taskCfg.ProcessorID] = taskCfg
	}
	f.branchSnapshot = make([]filterBranchEntry, 0, len(f.taskConfigMaps))
	for processorID, taskConfig := range f.taskConfigMaps {
		f.branchSnapshot = append(f.branchSnapshot, filterBranchEntry{processorID: processorID, taskConfig: taskConfig})
	}
}

func (f *Filters) Run() {
	defer close(f.GameOver)
	defer RemoveFilter(f.ID)
	for {
		select {
		case <-f.End:
			// node is done
			return
		case e := <-f.In:
			data := e.(*util.Data)
			if data.Event.HasTexts() {
				f.batchFilter(data)
			} else {
				f.singleFilter(data)
			}
		}
	}
}

// singleFilter 常规单行处理模式
func (f *Filters) singleFilter(data *util.Data) {
	event := &data.Event

	text, ok := event.Fields["data"].(string)
	if !ok {
		for _, out := range f.GetOuts() {
			select {
			case <-f.End:
				logp.L.Infof("node filter(%s) is done", f.ID)
				return
			case out <- data:
				filterHandledTotal.Add(1)
			}
		}
		return
	}

	branches, filterMaxIndex := f.getBranches()
	if f.Delimiter == "" {
		for _, out := range f.GetOuts() {
			select {
			case <-f.End:
				logp.L.Infof("node filter(%s) is done", f.ID)
				return
			case out <- data:
				filterHandledTotal.Add(1)
			}
		}
		return
	}

	var words []string
	if f.Delimiter != "" {
		// index为N时，数组切分最少需要分成N+1段
		words = strings.SplitN(text, f.Delimiter, filterMaxIndex+1)
		for i := range words {
			words[i] = strings.TrimSpace(words[i])
		}
	}

	for _, entry := range branches {
		processorID := entry.processorID
		out, ok := f.getOutput(processorID)
		if !ok {
			continue
		}
		matched := f.Handle(words, text, entry.taskConfig)
		if !matched {
			f.recordFilterDropped(processorID, 1)
			continue
		}

		select {
		case <-f.End:
			logp.L.Infof("node filter(%s) is done", f.ID)
			return
		case out <- data:
			filterHandledTotal.Add(1)
		}
	}
}

// batchFilter 针对多行文本的批量处理
func (f *Filters) batchFilter(data *util.Data) {
	texts := data.Event.GetTexts()
	branches, filterMaxIndex := f.getBranches()
	if f.Delimiter == "" {
		for _, out := range f.GetOuts() {
			select {
			case <-f.End:
				logp.L.Infof("node filter(%s) is done", f.ID)
				return
			case out <- data:
				filterHandledTotal.Add(int64(len(texts)))
			}
		}
		return
	}

	var wordsInTexts [][]string
	if f.Delimiter != "" {
		wordsInTexts = make([][]string, 0, len(texts))
		for _, text := range texts {
			wordsInTexts = append(wordsInTexts, strings.SplitN(text, f.Delimiter, filterMaxIndex+1))
		}
	}

	for _, entry := range branches {
		processorID := entry.processorID
		out, ok := f.getOutput(processorID)
		if !ok {
			continue
		}

		matchedTexts := make([]string, 0, len(texts))
		var filterDropped int64

		for idx := range texts {
			var words []string
			if wordsInTexts != nil {
				words = wordsInTexts[idx]
			}
			text := texts[idx]
			matched := f.Handle(words, text, entry.taskConfig)
			if !matched {
				filterDropped++
				continue
			}
			matchedTexts = append(matchedTexts, text)
		}

		f.recordFilterDropped(processorID, filterDropped)

		if len(matchedTexts) > 0 {
			// 复制一个新的事件出来，只修改 Texts 字段
			event := data.GetEvent()
			event.Texts = matchedTexts
			taskData := &util.Data{Event: event}
			taskData.SetState(data.GetState())

			select {
			case <-f.End:
				logp.L.Infof("node filter(%s) is done", f.ID)
				return
			case out <- taskData:
				filterHandledTotal.Add(int64(len(matchedTexts)))
			}
		}
	}
}

func (f *Filters) getBranches() ([]filterBranchEntry, int) {
	f.configMutex.RLock()
	defer f.configMutex.RUnlock()
	return f.branchSnapshot, f.filterMaxIndex
}

func (f *Filters) getOutput(processorID string) (chan interface{}, bool) {
	f.OutsLock.RLock()
	defer f.OutsLock.RUnlock()
	out, ok := f.Outs[processorID]
	return out, ok
}

func (f *Filters) recordFilterDropped(processorID string, count int64) {
	if count == 0 {
		return
	}

	filterDroppedTotal.Add(count)

	f.ForEachTaskNodeBy(processorID, func(tNode *base.TaskNode) {
		base.CrawlerDropped.Add(count)
		tNode.CrawlerDropped.Add(count)
	})
}

// Handle 过滤数据
func (f *Filters) Handle(words []string, text string, taskConfig *config.TaskConfig) bool {
	if !taskConfig.HasFilter {
		return true
	}

	for _, filterConfig := range taskConfig.Filters {
		access := true
		for _, condition := range filterConfig.Conditions {
			matcher := condition.GetMatcher()
			if matcher == nil {
				access = false
				break
			}

			// 匹配第n列，如果n小于等于0，则变更为整个字符串匹配操作
			if condition.Index <= 0 {
				if !matcher(text) {
					access = false
					break
				} else {
					continue
				}
			}

			// 分隔符过滤
			if len(words) < condition.Index {
				access = false
				break
			}
			if !matcher(words[condition.Index-1]) {
				access = false
				break
			}
		}
		if access {
			return true
		}
	}
	return false
}
