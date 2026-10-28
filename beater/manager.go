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

package beater

import (
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	bkmonitoring "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/monitoring"
	filebeatinput "github.com/elastic/beats/filebeat/input"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/libbeat/monitoring"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/registrar"
	"github.com/TencentBlueKing/bkunifylogbeat/task"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
)

var (
	taskActive  = bkmonitoring.NewInt("manager_active", monitoring.Gauge)
	taskStarted = bkmonitoring.NewInt("manager_start")
	taskStop    = bkmonitoring.NewInt("manager_stop")
	taskReload  = bkmonitoring.NewInt("manager_reload")
	taskError   = bkmonitoring.NewInt("manager_error")
)

// Manager 任务管理
// 1. 读取采集任务
// 2. 管理任务生命周期：创建、删除
type Manager struct {
	tasks    map[string]*task.Task
	config   cfg.Config
	wg       sync.WaitGroup
	beatDone chan struct{}

	// stopOnce 保证 Manager.Stop 只执行一次；-T 预览的错误退出路径可能由
	// LogBeat.Stop 与入口清理重复触发。
	stopOnce sync.Once

	// isTestMode 用于 -T 预览：任务启动失败时向上返回错误，避免入口静默等待。
	// 普通模式仍保留逐任务容错语义。
	isTestMode func() bool
	// startTaskFunc 仅供测试注入任务启动错误，生产路径为 nil。
	startTaskFunc func(*cfg.TaskConfig, []file.State) error

	// -T direct-read preview coordination. previewDone is created only after
	// the single preview task has been validated and managed mode is enabled.
	// previewInputDone is closed when the finite input stopped producing, before
	// the drain marker is injected, so the shutdown budget covers the marker
	// traversal itself.
	preview              bool
	previewTaskID        string
	previewDone          chan struct{}
	previewInputDone     chan struct{}
	previewErr           error
	previewMu            sync.Mutex
	previewOnce          sync.Once
	previewInputOnce     sync.Once
	enableManagedPreview func() error

	// 自适应扫描控制器由 Manager 独占管理；配置锁保证并发 Reload 时只会有一条替换链路。
	adaptiveScan             *utils.AdaptiveScanController
	adaptiveScanConfig       cfg.AdaptiveScanConfig
	adaptiveScanMu           sync.Mutex
	setAdaptiveScanHooksFunc func(filebeatinput.AdaptiveScanHooks)
}

// testModeEnabled 通过 libgse 注册的 -T 标志判断当前是否为预览模式。
// 不新增 libgse API，避免在依赖版本发布前扩大跨模块接口。
func testModeEnabled() bool {
	testModeFlag := flag.Lookup("T")
	return testModeFlag != nil && testModeFlag.Value.String() == "true"
}

// NewManager create new manager
func NewManager(config cfg.Config, beatDone chan struct{}) (*Manager, error) {
	m := &Manager{
		config:                   config,
		beatDone:                 beatDone,
		tasks:                    make(map[string]*task.Task),
		setAdaptiveScanHooksFunc: filebeatinput.SetAdaptiveScanHooks,
		isTestMode:               testModeEnabled,
		enableManagedPreview:     beat.EnableManagedPreview,
	}

	return m, nil
}

// Start Run start and link modules
func (m *Manager) Start() error {
	var err error
	logp.L.Info("start manager")

	utils.SetResourceLimit(m.config.MaxCpuLimit, m.config.CpuCheckTimes)
	if err = m.configureAdaptiveScan(m.config.AdaptiveScan); err != nil {
		return err
	}

	// Task
	lastStates := registrar.ResetStates(Registrar.GetStates())
	var tasks map[string]*cfg.TaskConfig
	if m.testMode() && hasEnabledDirectReadIntent(m.config) {
		tasks, err = cfg.GetTasksStrict(m.config)
		if err != nil {
			return fmt.Errorf("loading preview tasks: %w", err)
		}
	} else {
		tasks = cfg.GetTasks(m.config)
	}
	if m.testMode() && len(tasks) == 0 {
		logp.L.Warnf("-T loaded no tasks; waiting for an external shutdown")
	}
	if err = m.preparePreview(tasks); err != nil {
		return err
	}
	startTask := m.startTask
	if m.startTaskFunc != nil {
		startTask = m.startTaskFunc
	}
	for taskID, taskConfig := range tasks {
		err = startTask(taskConfig, lastStates)
		if err != nil {
			logp.L.Errorf("error creating task, taskID=>%s, err=>%v", taskID, err)
			// -T 预览必须把无效任务配置暴露给入口，由入口非零退出；
			// 普通模式保留既有的逐任务容错语义。
			if m.isTestMode != nil && m.isTestMode() {
				return fmt.Errorf("failed to start task %s: %w", taskID, err)
			}
			continue
		}
		taskStarted.Add(1)
	}

	return nil
}

// Stop Close manager when program quit
func (m *Manager) Stop() error {
	m.stopOnce.Do(func() {
		// 先卸载 hooks，再停止 Task，避免 Runner 退出期间继续调用即将销毁的控制器。
		_ = m.configureAdaptiveScan(cfg.AdaptiveScanConfig{Enabled: false})
		for _, t := range m.tasks {
			t.Stop()
			m.wg.Done()
		}
		m.wg.Wait()
		taskStop.Sub(1)
	})
	return nil
}

// Reload diff config, create, remove, update jobs
func (m *Manager) Reload(config cfg.Config) {
	if m.preview {
		logp.L.Info("ignoring reload during managed preview")
		return
	}
	logp.L.Infof("[Reload]update config, current tasks=>%d", len(m.tasks))

	utils.SetResourceLimit(config.MaxCpuLimit, config.CpuCheckTimes)
	if err := m.configureAdaptiveScan(config.AdaptiveScan); err != nil {
		logp.L.Errorf("configure adaptive scan failed: %v", err)
	}

	lastStates := registrar.ResetStates(Registrar.GetStates())
	newTasks := cfg.GetTasks(config)

	reloadTasks := make(map[string]*cfg.TaskConfig)
	removeTasks := make(map[string]*cfg.TaskConfig)
	addTasks := make(map[string]*cfg.TaskConfig)

	//step 1: 生成原来的任务清单
	for taskID, taskInst := range m.tasks {
		removeTasks[taskID] = taskInst.Config
	}

	//step 2: 根据新配置找出有变动的任务列表
	for taskID, taskConfig := range newTasks {
		if originTaskConfig, ok := removeTasks[taskID]; ok {
			if originTaskConfig.Same(taskConfig) {
				logp.L.Debugf("logbeat", "ignore secondary config file: %s, for already exists", taskID)
				delete(removeTasks, taskID)
				reloadTasks[taskID] = taskConfig
				continue
			} else {
				logp.L.Infof("load modified secondary config file: %s", taskID)
				addTasks[taskID] = taskConfig
			}
		} else {
			logp.L.Infof("load new secondary config file: %s", taskID)
			addTasks[taskID] = taskConfig
		}
	}

	logp.L.Infof("[Reload]originTasks=>%d, removeTasks=>%d, addTasks=>%d",
		len(newTasks), len(removeTasks), len(addTasks))

	//step 3：清理任务信息
	var err error
	var isReloadRegistrar bool

	if len(removeTasks) > 0 {
		isReloadRegistrar = true
		for taskID, _ := range removeTasks {
			err = m.removeTask(taskID)
			if err != nil {
				logp.L.Errorf("remove task fail, taskID=>%s, err=>%v", taskID, err)
			}
			taskStop.Add(1)
		}
	}

	time.Sleep(3 * time.Second) // 增加等待时间，确保任务删除后，相关资源已经完整释放

	//step 4：新增的任务需要启动采集、存量任务需要重新加载配置
	if len(addTasks) > 0 {
		for _, taskConfig := range addTasks {
			err = m.startTask(taskConfig, lastStates)
			if err != nil {
				logp.L.Errorf("start task fail, taskID=>%s, err=>%v", taskConfig.ID, err)
			}
			taskReload.Add(1)
		}
	}

	if isReloadRegistrar {
		if len(reloadTasks) > 0 {
			for taskID, _ := range reloadTasks {
				err = m.reloadTask(taskID)
				if err != nil {
					logp.L.Errorf("reload task fail, taskID=>%s, err=>%v", taskID, err)
				}
			}
		}
	}

	//step 5: 重设beats配置
	m.config = config
}

// configureAdaptiveScan 以完整 hooks bundle 为单位替换自适应扫描控制器。
// 新控制器会先完成校验和启动；创建失败时直接返回，旧控制器继续工作。
// 替换时依次卸载 hooks、断开清理通知、停止旧控制器，最后安装新控制器，
// 确保 Interval 与 Applied 始终属于同一代配置。
// 已经进入等待的 Runner 不会被 Reload 唤醒；当前等待到期并完成扫描后，
// Runner 才读取新 hooks 计算后续周期，这是自适应配置约定的下一轮生效语义。
func (m *Manager) configureAdaptiveScan(config cfg.AdaptiveScanConfig) error {
	m.adaptiveScanMu.Lock()
	defer m.adaptiveScanMu.Unlock()

	if m.setAdaptiveScanHooksFunc == nil {
		m.setAdaptiveScanHooksFunc = filebeatinput.SetAdaptiveScanHooks
	}
	if config.Enabled && m.adaptiveScan != nil && m.adaptiveScanConfig == config {
		return nil
	}

	var controller *utils.AdaptiveScanController
	if config.Enabled {
		// 先构造新实例，不提前破坏当前可用配置，避免一次无效 Reload 关闭既有能力。
		var err error
		controller, err = utils.NewAdaptiveScanController(utils.AdaptiveScanSettings{
			MinScanFrequency: config.MinScanFrequency,
			ScanCPUPercent:   config.ScanCPUPercent,
			ControlInterval:  config.ControlInterval,
		})
		if err != nil {
			return fmt.Errorf("create adaptive scan controller: %w", err)
		}
		controller.Start()
	}

	// 计算与最终结果通知必须整组卸载，避免 Reload 窗口内出现新旧回调错配。
	m.setAdaptiveScanHooksFunc(filebeatinput.AdaptiveScanHooks{})
	utils.SetActiveAdaptiveScanController(nil)
	if m.adaptiveScan != nil {
		m.adaptiveScan.Stop()
	}
	m.adaptiveScan = controller
	m.adaptiveScanConfig = cfg.AdaptiveScanConfig{}
	if controller != nil {
		m.adaptiveScanConfig = config
		utils.SetActiveAdaptiveScanController(controller)
		// Beats 最终裁决周期后通过 Applied 回传，日志记录的才是 Runner 实际等待周期。
		m.setAdaptiveScanHooksFunc(filebeatinput.AdaptiveScanHooks{
			Interval: controller.NextInterval,
			Applied:  controller.ObserveApplied,
		})
	}
	return nil
}

// startTask 启动任务，调用filebeat.runner开始进行日志采集
func (m *Manager) startTask(config *cfg.TaskConfig, lastStates []file.State) error {
	if _, ok := m.tasks[config.ID]; ok {
		return fmt.Errorf("task with same ID already exists: %s", config.ID)
	}
	if err := m.validateDirectTask(config); err != nil {
		return err
	}
	var err error
	taskInst, err := task.NewTask(config, m.beatDone, lastStates)
	if err != nil {
		logp.L.Errorf("start task err, taskid=>%s err=>%v", config.ID, err)
		taskError.Add(1)
		return err
	}
	if m.preview && config.ID == m.previewTaskID {
		taskInst.SetPreviewCompletion(m.notifyPreviewDone)
		taskInst.SetPreviewInputCompletion(m.notifyPreviewInputDone)
	}
	taskInst.Start()

	m.wg.Add(1)
	m.tasks[config.ID] = taskInst
	taskActive.Add(1)
	return nil
}

// removeTask 移除任务，停止filebeat.runner
func (m *Manager) removeTask(taskID string) error {
	var err error
	if _, ok := m.tasks[taskID]; !ok {
		return fmt.Errorf("task is not exists, taskID=>%s", taskID)
	}
	err = m.tasks[taskID].Stop()
	if err != nil {
		taskError.Add(1)
		return err
	}
	m.wg.Done()
	delete(m.tasks, taskID)
	taskActive.Sub(1)
	return nil
}

// reloadTask 任务重载
func (m *Manager) reloadTask(taskID string) error {
	var err error
	if _, ok := m.tasks[taskID]; !ok {
		return fmt.Errorf("task is not exists, taskID=>%s", taskID)
	}
	err = m.tasks[taskID].Reload()
	if err != nil {
		taskError.Add(1)
		return err
	}
	return nil
}
