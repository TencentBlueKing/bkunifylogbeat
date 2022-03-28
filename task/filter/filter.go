package filter

import (
	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/task/processor"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
	"github.com/elastic/beats/filebeat/util"
	"strings"
	"sync"
)

var (
	filterMaps = map[string]*Filters{}
	mtx        sync.RWMutex
)

type Filters struct {
	*base.Node

	Delimiter      string
	filterMaxIndex int

	taskConfigMaps map[string]*config.TaskConfig
}

// GetFilters get filter
func GetFilters(taskCfg *config.TaskConfig, leafNode *base.Node) (*Filters, error) {
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
		p, err := processor.GetProcessors(taskCfg, leafNode)
		if err != nil {
			return nil, err
		}

		fil.MergeFilterConfig(taskCfg)
		fil.AddOutput(p.Node)
		return fil, nil
	}
	return NewFilters(taskCfg, leafNode)
}

func NewFilters(taskCfg *config.TaskConfig, leafNode *base.Node) (*Filters, error) {
	var err error
	var fil = &Filters{
		Node:      base.NewEmptyNode(taskCfg.FilterID),
		Delimiter: taskCfg.Delimiter,

		taskConfigMaps: map[string]*config.TaskConfig{},
	}
	fil.MergeFilterConfig(taskCfg)

	p, err := processor.NewProcessors(taskCfg, leafNode)
	if err != nil {
		return nil, err
	}
	fil.AddOutput(p.Node)

	go fil.Run()

	logp.L.Infof("add filter(%s) to global filterMaps", fil.ID)
	mtx.Lock()
	defer mtx.Unlock()
	filterMaps[taskCfg.FilterID] = fil
	return fil, err
}

// RemoveFilter : 移除全局缓存
func RemoveFilter(id string) {
	logp.L.Infof("remove filter(%s) in global filterMaps", id)
	mtx.Lock()
	defer mtx.Unlock()
	delete(filterMaps, id)
}

func (f *Filters) MergeFilterConfig(taskCfg *config.TaskConfig) {
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
	f.taskConfigMaps[taskCfg.ID] = taskCfg
}

func (f *Filters) Run() {
	defer RemoveFilter(f.ID)
	for {
		select {
		case <-f.End:
			// node is done
			return
		case e := <-f.In:
			data := e.(*util.Data)
			event := &data.Event

			// index为N时，数组切分最少需要分成N+1段
			var text string
			var ok bool
			text, ok = event.Fields["data"].(string)
			if !ok || f.Delimiter == "" {
				for _, out := range f.Outs {
					select {
					case <-f.End:
						logp.L.Infof("node filter(%s) is done", f.ID)
						return
					case out <- data:
					}
				}
				break
			}

			words := strings.SplitN(text, f.Delimiter, f.filterMaxIndex+1)
			for _, taskConfig := range f.taskConfigMaps {
				event := f.Handle(words, text, taskConfig, event)
				if event == nil {
					continue
				}

				out, ok := f.Outs[taskConfig.ProcessorID]
				if ok {
					select {
					case <-f.End:
						logp.L.Infof("node filter(%s) is done", f.ID)
						return
					case out <- data:
					}
				}
			}

		}
	}
}

// Handle 过滤数据
func (f *Filters) Handle(words []string, text string, taskConfig *config.TaskConfig, event *beat.Event) *beat.Event {
	if !taskConfig.HasFilter {
		return event
	}

	for _, filterConfig := range taskConfig.Filters {
		access := true
		for _, condition := range filterConfig.Conditions {
			// 匹配第n列，如果n小于等于0，则变更为整个字符串包含
			if condition.Index <= 0 {
				if !strings.Contains(text, condition.Key) {
					access = false
					break
				} else {
					continue
				}
			}
			operationFunc := getOperation(condition.Op)
			if operationFunc != nil {
				if len(words) < condition.Index {
					access = false
					break
				}
				if !operationFunc(words[condition.Index-1], condition.Key) {
					access = false
					break
				}
			}
		}
		if access {
			return event
		}
	}
	return nil
}
