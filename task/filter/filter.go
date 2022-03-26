package filter

import (
	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"strings"
)

type Filters struct {
	taskConfig *config.TaskConfig

	filterMaxIndex int
}

// NewFilters 新建一个过滤器
func NewFilters(config *config.TaskConfig) *Filters {
	filters := &Filters{
		taskConfig: config,
	}

	// Filter
	if config.HasFilter {
		for _, f := range config.Filters {
			if len(f.Conditions) != 0 {
				if filters.filterMaxIndex < f.Conditions[len(f.Conditions)-1].Index {
					filters.filterMaxIndex = f.Conditions[len(f.Conditions)-1].Index
				}
			}
		}
	}

	return filters
}

// Run 过滤数据
func (f *Filters) Run(event *beat.Event) *beat.Event {
	if !f.taskConfig.HasFilter {
		return event
	}

	// index为N时，数组切分最少需要分成N+1段
	var text string
	var ok bool
	if text, ok = event.Fields["data"].(string); !ok {
		return event
	}
	words := strings.SplitN(text, f.taskConfig.Delimiter, f.filterMaxIndex+1)

	for _, filterConfig := range f.taskConfig.Filters {
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
