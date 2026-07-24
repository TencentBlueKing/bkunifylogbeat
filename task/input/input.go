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

package input

import (
	"sync"
	"time"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	bkmonitoring "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/monitoring"
	"github.com/elastic/beats/filebeat/channel"
	"github.com/elastic/beats/filebeat/input"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/filebeat/util"
	"github.com/elastic/beats/libbeat/common"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/task/filter"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
)

var (
	inputMaps = map[string]*Input{}
	mtx       sync.RWMutex

	numOfInputTotal = bkmonitoring.NewInt("task_input_total") // 当前全局input的数量

	// input 没有做处理，没有丢弃的可能，所以不上报这个指标
	//inputDroppedTotal = bkmonitoring.NewInt("input_dropped_total")
	inputHandledTotal = bkmonitoring.NewInt("input_handled_total")
)

// ContainerStdoutFields container 标准输出字段
type ContainerStdoutFields struct {
	Log    string `json:"log"`
	Stream string `json:"stream"`
	Time   string `json:"time"`
}

func GetInput(
	taskCfg *config.TaskConfig,
	taskNode *base.TaskNode,
	beatDone chan struct{},
	states []file.State,
) (*Input, error) {

	var (
		ok bool
		in *Input
	)
	func() {
		mtx.RLock()
		defer mtx.RUnlock()
		in, ok = inputMaps[taskCfg.InputID]
	}()

	if ok {
		f, err := filter.GetFilters(taskCfg, taskNode)
		if err != nil {
			return nil, err
		}
		in.AddOutput(f.Node)
		in.AddTaskNode(f.Node, taskNode)
		// 共享 input 每接入一个 Task 都追加一份元数据，日志才能还原完整任务列表。
		utils.RegisterAdaptiveScanInput(in.runner.AdaptiveScanID(), adaptiveScanMetadataFromTask(taskCfg))
		return in, nil
	}

	return NewInput(taskCfg, taskNode, beatDone, states)
}

func NewInput(
	taskCfg *config.TaskConfig,
	taskNode *base.TaskNode,
	beatDone chan struct{},
	states []file.State,
) (*Input, error) {
	var err error
	var in = &Input{
		Node:              base.NewEmptyNode(taskCfg.InputID),
		IsContainerStd:    taskCfg.IsContainerStd,
		IsCRIContainerStd: taskCfg.IsCRIContainerStd,
	}

	f, err := filter.NewFilters(taskCfg, taskNode)
	if err != nil {
		return nil, err
	}
	in.AddOutput(f.Node)
	in.AddTaskNode(f.Node, taskNode)
	go in.Run()

	// input.New 里会发送事件出来，需要先创建好后续的Output，再创建Input
	in.runner, err = input.New(
		taskCfg.RawConfig, ConnectToTask(in), beatDone, states, nil)
	if err != nil {
		return nil, err
	}
	// Runner 创建后才有进程内唯一 AdaptiveScanID，元数据必须以该实例 ID 注册。
	utils.RegisterAdaptiveScanInput(in.runner.AdaptiveScanID(), adaptiveScanMetadataFromTask(taskCfg))

	logp.L.Infof("add input(%s) to global inputMaps", in.ID)
	mtx.Lock()
	defer mtx.Unlock()
	inputMaps[taskCfg.InputID] = in
	numOfInputTotal.Add(1)
	return in, nil
}

// RemoveInput : 移除全局缓存
func RemoveInput(id string) {
	logp.L.Infof("remove input(%s) in global inputMaps", id)
	mtx.Lock()
	defer mtx.Unlock()
	delete(inputMaps, id)
	numOfInputTotal.Add(-1)
}

type Input struct {
	*base.Node

	IsContainerStd    bool
	IsCRIContainerStd bool

	runner   *input.Runner
	runOnce  sync.Once
	stopOnce sync.Once
}

// Start 启动runner
// 创建后则自动启动
func (in *Input) Start() {
	in.runOnce.Do(func() {
		in.runner.Start()
	})
}

func (in *Input) Run() {
	defer close(in.GameOver)
	defer func() {
		RemoveInput(in.ID)
		in.stop()
	}()
	for {
		select {
		case <-in.End:
			return
		case e := <-in.In:
			// 处理速率限制，可在一定层度上限制CPU的使用率
			if utils.IsEnableRateLimiter && utils.GlobalCpuLimiter != nil {
				checkInterval := utils.GlobalCpuLimiter.GetCheckInterval()
				for {
					if utils.GlobalCpuLimiter.Allow() {
						break
					} else {
						time.Sleep(checkInterval)
					}
				}
			}

			base.CrawlerReceived.Add(1)

			data := e.(*util.Data)
			if data.Event.Fields != nil {
				for _, out := range in.GetOuts() {
					select {
					case <-in.End:
						return
					case out <- data:
						inputHandledTotal.Add(1)
						in.ForEachTaskNode(func(tNode *base.TaskNode) {
							tNode.CrawlerReceived.Add(int64(data.Event.Count()))
						})
					}
				}
			} else {
				// 采集进度类事件
				beat.SendEvent(beat.Event{
					Fields:  nil,
					Private: data.GetState(),
				})
				base.CrawlerState.Add(1)
				in.ForEachTaskNode(func(tNode *base.TaskNode) {
					tNode.CrawlerState.Add(1)
				})
			}
		}
	}
}

// stop : 停止runner
// 停用的场景:
//  1. 当outs为空后，自动退出
//  2. 当End的channel被主动关闭后
func (in *Input) stop() {
	in.stopOnce.Do(func() {
		if in.runner == nil {
			return
		}
		runner := in.runner
		go func() {
			runner.Stop() // 防止卡主reload的流程，这里改为异步，不等待input结束
			// 必须等待 Runner 完全停止后再清理，避免在途回调重新创建已删除的控制状态。
			utils.UnregisterAdaptiveScanInput(runner.AdaptiveScanID())
		}()
	})
}

func adaptiveScanMetadataFromTask(taskConfig *config.TaskConfig) utils.AdaptiveScanInputMetadata {
	return utils.AdaptiveScanInputMetadata{
		InputID: taskConfig.InputID,
		TaskID:  taskConfig.ID,
		DataID:  taskConfig.DataID,
		Paths:   append([]string(nil), taskConfig.Paths...),
	}
}

// UnregisterAdaptiveScanTask 从当前 Runner 的诊断元数据中移除指定任务，
// 同时保留共享该 Runner 的其他任务。
func (in *Input) UnregisterAdaptiveScanTask(taskConfig *config.TaskConfig) {
	if in.runner == nil || taskConfig == nil {
		return
	}
	utils.UnregisterAdaptiveScanTask(in.runner.AdaptiveScanID(), adaptiveScanMetadataFromTask(taskConfig))
}

// Reload : Input不做reload处理，配置如果有变化，直接删除新建
func (in *Input) Reload() {
	return
}

// ConnectToTask 返回任务实例，用于接收采集事件 OnEvent
func ConnectToTask(in *Input) channel.Connector {
	return func(cfg *common.Config, m *common.MapStrPointer) (channel.Outleter, error) {
		return in, nil
	}
}

// Close 由Filebeat在停止采集插件后调用
func (in *Input) Close() error {
	return nil
}

// Done 返回任务状态channel
func (in *Input) Done() <-chan struct{} {
	return in.End
}

// OnEvent 处理input runner发送的事件
func (in *Input) OnEvent(data *util.Data) bool {
	if data == nil {
		logp.L.Errorf("task get event nil, inputID:%s", in.ID)
		return false
	}

	select {
	case <-in.End:
		return false
	case in.In <- data:
	}

	return true
}
