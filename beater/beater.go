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
	"sync"
	"sync/atomic"
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

	// stopOnce 保证任务与 done 通道只停止/关闭一次，重复调用 Stop 安全。
	stopOnce sync.Once
	// doneOnce 保护 done：正常 Stop 与收尾预算到期后的强制 abort 都可能关闭它。
	doneOnce sync.Once
	// managerStopOnce 只启动一次 Manager.Stop；managerStopped 在其返回后关闭。
	// 单独用 sync.Once 启动 goroutine 而不是让 Do 执行阻塞操作，避免第一次
	// Stop 卡住时后续调用者一起永久等待。
	managerStopOnce sync.Once
	managerStopped  chan struct{}
	// aborted 标记预览收尾已走强制退出路径。此后 Stop 不再同步等待 Manager，
	// 因为那次等待已经在 abort 中有界完成过。
	aborted atomic.Bool
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
	Registrar, err = registrar.New(bt.config.Registry, bt.config.FileIdentifier)
	if err != nil {
		return err
	}
	err = Registrar.Start()
	if err != nil {
		return fmt.Errorf("could not start registrar: %v", err)
	}
	defer Registrar.Stop()

	if err := bt.manager.Start(); err != nil {
		logp.L.Errorf("failed to start manager, error: %v", err)
		// Manager.Start 可能已经启动了部分任务，返回错误前先停任务；
		// Registrar 的 defer 随后执行，最后才由 main 释放框架/Storage。
		bt.Stop()
		return fmt.Errorf("could not start manager: %w", err)
	}
	previewDone := bt.manager.PreviewDone()
	previewInputDone := bt.manager.PreviewInputDone()

	// The fixed shutdown budget is shared by the normal completion path and the
	// signal path. It starts on the first of: finite input completion, SIGTERM.
	// A nil previewDone keeps every preview branch below disabled.
	var previewBudget *previewFinalizeBudget
	var previewDeadline <-chan time.Time
	if previewDone != nil {
		previewBudget = newPreviewFinalizeBudget(previewFinalizeTimeout)
		defer previewBudget.stop()
		if previewInputDone != nil {
			// The input may have completed before the loop was entered.
			select {
			case <-previewInputDone:
				previewInputDone = nil
				previewDeadline = previewBudget.start()
			default:
			}
		}
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
		// 有限输入已完成、排空标记即将注入。输出背压可能让标记永远到不了
		// Task，因此收尾预算必须从这一刻开始计时，而不是等预览完成信号。
		case <-previewInputDone:
			previewInputDone = nil
			previewDeadline = previewBudget.start()
		// 收尾预算到期：先断开 publisher 解除 Publish 阻塞，再有界等待
		// Manager.Stop，保证 Run 一定返回。
		case <-previewDeadline:
			return bt.abortPreview(errPreviewFinalizeTimeout)
		// The finite Kafka preview finished on its own: the producer has
		// stopped, the drain marker has traversed the Task pipeline and the
		// Sender cache has been flushed.
		case <-previewDone:
			return bt.finishPreview(bt.manager.PreviewErr(), previewBudget)
		// 处理采集器框架发送的结束采集器的信号（常由SIGINT引起），关闭采集器
		case <-beat.Done:
			if previewDone != nil {
				previewBudget.start()
				return bt.finishPreviewAfterSignal(previewBudget)
			}
			bt.Stop()
			return nil
		}
	}
	logp.L.Info("Shutting down.")
	return nil
}

// finishPreview waits for every published event to be acknowledged and for the
// console batch to be flushed, then stops the Task/Manager. Registrar.Stop is
// still owned by Run's defer; storage is released by main's beat.Stop() after
// Registrar.Stop returns.
//
// The ACK wait and the final Manager.Stop are bounded: when the shared
// finalization budget expires the preview aborts instead of calling Stop
// synchronously, because a blocked publisher keeps Manager.Stop inside
// Task.Stop forever.
func (bt *LogBeat) finishPreview(err error, budget *previewFinalizeBudget) error {
	if budget == nil {
		budget = newPreviewFinalizeBudget(previewFinalizeTimeout)
		defer budget.stop()
	}
	budget.start()

	if budget.expired() {
		if err == nil {
			err = errPreviewFinalizeTimeout
		}
		return bt.abortPreview(err)
	}

	if waitErr := waitPublishIdle(budget.remaining()); waitErr != nil {
		if err == nil {
			err = waitErr
		}
		if budget.expired() {
			// The ACK/console drain did not finish inside the shared window:
			// force the publisher release instead of risking an unbounded
			// Manager.Stop.
			return bt.abortPreview(err)
		}
	}

	if !bt.stopManagerBounded(previewStopTimeout) {
		logp.L.Errorf("preview shutdown: manager did not stop within %s after drain", previewStopTimeout)
		if err == nil {
			err = fmt.Errorf("manager did not stop within %s after preview drain", previewStopTimeout)
		}
	}
	bt.closeDone()
	return err
}

// finishPreviewAfterSignal runs the same order as a normal completion, but
// first asks the Kafka input to stop and waits for its drain marker. It returns
// a non-nil error because an externally terminated preview is not a successful
// sample.
func (bt *LogBeat) finishPreviewAfterSignal(budget *previewFinalizeBudget) error {
	if budget == nil {
		budget = newPreviewFinalizeBudget(previewFinalizeTimeout)
		defer budget.stop()
	}
	bt.manager.StopPreviewInputs()

	select {
	case <-bt.manager.PreviewDone():
	case <-budget.start():
		return bt.abortPreview(errors.New("preview did not drain after shutdown signal"))
	}

	err := bt.manager.PreviewErr()
	if err == nil {
		err = errors.New("preview canceled by shutdown signal")
	}
	return bt.finishPreview(err, budget)
}

// abortPreview forcibly tears the preview down when the shared shutdown budget
// expires. The publisher client must be released first: a Task or Sender
// goroutine blocked inside Publish on a full output queue is only released by
// closing the client, and stopping the Task before that would wait forever.
// The remaining Manager.Stop wait is bounded, and Run always returns so
// Registrar.Stop and main's beat.Stop can release the Storage.
func (bt *LogBeat) abortPreview(cause error) error {
	if cause == nil {
		cause = errPreviewFinalizeTimeout
	}
	logp.L.Errorf("preview shutdown aborted, releasing publisher: %v", cause)

	// 1. Disconnect the publisher. The libgse API is idempotent, does not stop
	// the reloader, does not release Storage and does not close Done.
	bt.aborted.Store(true)
	stopPublish()

	// 2. Never call bt.Stop synchronously here: Manager.Stop joins the Task
	// goroutines and may still block on a stuck output worker.
	if !bt.stopManagerBounded(previewStopTimeout) {
		logp.L.Errorf("preview abort: manager did not stop within %s", previewStopTimeout)
	}

	// 3. Release Task loops waiting on done. Registrar.Stop (Run defer) and
	// beat.Stop (main) complete the remaining cleanup.
	bt.closeDone()
	return fmt.Errorf("preview aborted: %w", cause)
}

// startManagerStop launches Manager.Stop at most once and returns a channel that
// is closed when it returned. It returns immediately, so callers can bound the
// wait; a stuck first stop must not make later callers block inside sync.Once.
func (bt *LogBeat) startManagerStop() <-chan struct{} {
	bt.managerStopOnce.Do(func() {
		stopped := make(chan struct{})
		bt.managerStopped = stopped
		go func() {
			defer close(stopped)
			if bt.manager != nil {
				_ = bt.manager.Stop()
			}
		}()
	})
	return bt.managerStopped
}

func (bt *LogBeat) stopManagerBounded(timeout time.Duration) bool {
	select {
	case <-bt.startManagerStop():
		return true
	case <-time.After(timeout):
		return false
	}
}

func (bt *LogBeat) closeDone() {
	if bt.done == nil {
		return
	}
	bt.doneOnce.Do(func() {
		close(bt.done)
	})
}

// Stop beater interface
// 幂等：重复调用不会重复停止 Manager，也不会重复 close(done)；
// manager/done 为空（例如测试中直接构造 LogBeat）时同样安全。
func (bt *LogBeat) Stop() {
	bt.stopOnce.Do(func() {
		if !bt.aborted.Load() {
			// Wait for Manager.Stop as before. startManagerStop returns the
			// shared channel immediately, so repeated Stop calls stay safe.
			<-bt.startManagerStop()
		}
		bt.closeDone()
	})
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
	if bt.manager != nil && bt.manager.preview {
		logp.L.Info("ignoring reload during managed preview")
		return
	}
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
