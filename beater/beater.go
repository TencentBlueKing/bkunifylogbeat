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

// beaterCreator
// 1. 初始化配置、存储、监控
// 2. 发送事件

package beater

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/output/gse"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/utils/host"
	"github.com/elastic/beats/libbeat/cfgfile"
	"github.com/elastic/beats/libbeat/common"
	"github.com/pkg/errors"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	_ "github.com/TencentBlueKing/bkunifylogbeat/include" // 加载 Filebeat Input插件及配置优化模块
	"github.com/TencentBlueKing/bkunifylogbeat/registrar"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
)

var Registrar *registrar.Registrar

const beatName = "bkunifylogbeat"

// LogBeat  package cadvisor
type LogBeat struct {
	Name    string
	done    chan struct{}
	manager *Manager
	config  cfg.Config

	hostIDWatcher host.Watcher

	isReload     bool
	lastTaskHash string
}

// New create cadvisor beat
func New(rawConfig *beat.Config) (*LogBeat, error) {
	var bt = &LogBeat{
		done:     make(chan struct{}),
		isReload: false,
	}

	err := bt.ParseConfig(rawConfig)
	if err != nil {
		return nil, errors.Wrap(err, "error reading configuration file")
	}

	mgr, err := NewManager(bt.config, bt.done)
	if nil != err {
		logp.L.Errorf("can not create manager object , %v", err)
		return nil, err
	}
	bt.manager = mgr
	return bt, nil
}

func (bt *LogBeat) ParseConfig(rawConfig *beat.Config) error {
	config, err := cfg.Parse(rawConfig)
	if err != nil {
		return errors.Wrap(err, "error reading configuration file")
	}
	logp.L.Infof("config: %+v", config)
	bt.config = config

	err = bt.initHostIDWatcher()
	if err != nil {
		return fmt.Errorf("init hostid failed, error:%s", err)
	}
	return nil
}

// PublishEvent ISender interface
func (bt *LogBeat) PublishEvent(event beat.MapStr) bool {
	return beat.Send(event)
}

func (bt *LogBeat) windowsReload() {
	if !beat.IsContainerMode() {
		return
	}

	var modTime time.Time
	checkFunc := func() error {
		fileInfo, err := os.Stat(bt.config.WindowsReloadPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}

		if fileInfo.ModTime() != modTime {
			select {
			case beat.ReloadChan <- true:
			default:
			}
		}
		modTime = fileInfo.ModTime()
		return nil
	}

	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := checkFunc(); err != nil {
				logp.L.Errorf("failed to check windows reload path: %s", err)
			}
		}
	}
}

// Run beater interface
func (bt *LogBeat) Run() error {
	logp.L.Infof("logbeat is running! Hit CTRL-C to stop it.")

	// load last states
	var err error
	Registrar, err = registrar.New(bt.config.Registry)
	if err != nil {
		return err
	}
	err = Registrar.Start()
	if err != nil {
		return fmt.Errorf("could not start registrar: %v", err)
	}
	defer Registrar.Stop()

	if err := bt.manager.Start(); err != nil {
		logp.L.Error("failed to start manager ")
	}

	if runtime.GOOS == "windows" {
		go bt.windowsReload()
	}

	reloadTicker := time.NewTicker(10 * time.Second)
	diffTaskTicker := time.NewTicker(60 * time.Second)
	defer diffTaskTicker.Stop()
	defer reloadTicker.Stop()
	for {
		select {
		// 处理采集器框架发送的重加载配置信号
		case <-reloadTicker.C:
			if bt.isReload {
				bt.isReload = false
				config := beat.GetConfig()
				if bt.config.CheckDiff {
					config, err = GetRawConfig()
					if err != nil {
						logp.L.Error(err)
					}
				}
				if config != nil {
					bt.Reload(config)
				}
			}
		// 处理采集器主配置是否变更，变更则发送重加载信号
		case <-diffTaskTicker.C:
			if bt.config.CheckDiff {
				if err = bt.checkDiffReload(); err != nil {
					logp.L.Error(err)
				}
			}
		case <-beat.ReloadChan:
			bt.isReload = true
		// 处理采集器框架发送的结束采集器的信号（常由SIGINT引起），关闭采集器
		case <-beat.Done:
			bt.Stop()
			return nil
		}
	}
	logp.L.Info("Shutting down.")
	return nil
}

// Stop beater interface
func (bt *LogBeat) Stop() {
	bt.manager.Stop()
	close(bt.done)
}

// GetRawConfig Get raw main config
func GetRawConfig() (*common.Config, error) {
	rawConfig, err := cfgfile.Load("", nil)
	if err != nil {
		return nil, err
	}

	if !rawConfig.HasField(beatName) {
		return nil, errors.New("no beat name field found")
	}

	beatConfig, err := rawConfig.Child(beatName, -1)
	if err != nil {
		return nil, err
	}

	return beatConfig, nil
}

// Main config diff check
func (bt *LogBeat) checkDiffReload() error {
	beatConfig, err := GetRawConfig()
	if err != nil {
		return err
	}
	config, err := cfg.Parse(beatConfig)
	if err != nil {
		return err
	}
	b, err := json.Marshal(config)
	if err != nil {
		return err
	}

	currentTaskHash := utils.Md5(string(b))
	if len(bt.lastTaskHash) == 0 {
		bt.lastTaskHash = currentTaskHash
	}
	if bt.lastTaskHash != currentTaskHash {
		bt.lastTaskHash = currentTaskHash
		bt.Reload(beatConfig)
		logp.L.Info("Reload main config task.")
	}

	return nil
}

// Close cadvisor storage interface
func (bt *LogBeat) Close() error {
	return nil
}

// Reload beater interface
func (bt *LogBeat) Reload(c *beat.Config) {
	logp.L.Infof("reload with: %v", c)
	err := bt.ParseConfig(c)
	if err != nil {
		logp.L.Errorf("parse config error, %v", err)
	}

	bt.manager.Reload(bt.config)
}

// initHostIDWatcher 监听cmdb下发host id文件
func (bt *LogBeat) initHostIDWatcher() error {
	var err error
	if bt.hostIDWatcher != nil {
		err = bt.hostIDWatcher.Reload(context.Background(), bt.config.HostIDPath, bt.config.CmdbLevelMaxLength, bt.config.MustHostIDExist)
		if err != nil {
			logp.L.Warnf("reload watch host id failed,error:%s", err.Error())
			// 不影响其他位置的reload
			return nil
		}
		return nil
	}

	// 将watcher初始化并启动
	hostConfig := host.Config{
		HostIDPath:         bt.config.HostIDPath,
		CMDBLevelMaxLength: bt.config.CmdbLevelMaxLength,
		IgnoreCmdbLevel:    bt.config.IgnoreCmdbLevel,
		MustHostIDExist:    bt.config.MustHostIDExist,
	}
	bt.hostIDWatcher = host.NewWatcher(context.Background(), hostConfig)
	err = bt.hostIDWatcher.Start()
	if err != nil {
		logp.L.Warnf("start watch host id failed,filepath:%s,cmdb max length:%d,error:%s", bt.config.HostIDPath, bt.config.CmdbLevelMaxLength, err)
		return err
	}
	gse.RegisterHostWatcher(bt.hostIDWatcher)
	logp.L.Infof("register hostid to gse success.")

	return nil
}
