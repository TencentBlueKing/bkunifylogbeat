package utils

import (
	"fmt"
	"github.com/elastic/beats/filebeat/util"
)

//默认配置
type FactoryCacheIdentifier = func(event *util.Data, dataId string) string

var registryCacheIdentifier = make(map[string]FactoryCacheIdentifier)

// Register 用于处理采集任务配置兼容
func RegisterCacheIdentifier(name string, factory FactoryCacheIdentifier) error {
	if name == "" {
		return fmt.Errorf("error registering input cache identifier factory: name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("error registering input cache identifier factory'%v': config cannot be empty", name)
	}
	if _, exists := registryCacheIdentifier[name]; exists {
		return fmt.Errorf("error registering input cache identifier factory'%v': already registered", name)
	}

	registryCacheIdentifier[name] = factory
	return nil
}

//HashRawConfig: 获取配置hash值
func CacheIdentifier(inputType string, event *util.Data, dataId string) string {
	f, exist := registryCacheIdentifier[inputType]
	if exist {
		return f(event, dataId)
	}
	return event.GetState().Source
}
