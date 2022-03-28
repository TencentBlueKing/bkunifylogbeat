package input

import (
	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/task/filter"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
	"github.com/elastic/beats/filebeat/channel"
	"github.com/elastic/beats/filebeat/input"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/filebeat/util"
	"github.com/elastic/beats/libbeat/common"
	"runtime"
	"sync"
	"time"
)

var (
	isEnableRateLimiter bool            // 是否开启速率限制
	cpuLimiter          *utils.CPULimit // CPU使用率限制

	inputMaps = map[string]*Input{}
	mtx       sync.RWMutex
)

// SetResourceLimit 在一定程度上限制CPU使用
func SetResourceLimit(maxCpuLimit, checkTimes int) {
	numCPU := runtime.NumCPU()
	// 在docker富容器中 并且 开启了速率限制
	if utils.IsInDocker() {
		logp.L.Infof("enable rate limit. because numOfCpu(%d) && isInDocker(%v)",
			numCPU, true,
		)

		if maxCpuLimit > 0 && maxCpuLimit <= numCPU*100 {
			cpuLimiter = utils.NewCPULimit(maxCpuLimit, checkTimes)
			isEnableRateLimiter = true
		} else {
			logp.L.Infof("disable rate limit. because cpu limit config(%d) is not valid",
				maxCpuLimit,
			)
		}
	} else {
		logp.L.Infof("disable rate limit. because numOfCpu(%d) && isInDocker(%v), "+
			"cpu limit config(%d)",
			numCPU, false, maxCpuLimit,
		)
		if cpuLimiter != nil {
			cpuLimiter.Stop()
		}
		isEnableRateLimiter = false
	}
}

func GetInput(
	taskCfg *config.TaskConfig,
	leafNode *base.Node,
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
		f, err := filter.GetFilters(taskCfg, leafNode)
		if err != nil {
			return nil, err
		}
		in.AddOutput(f.Node)
		return in, nil
	}

	return NewInput(taskCfg, leafNode, beatDone, states)
}

func NewInput(
	taskCfg *config.TaskConfig,
	leafNode *base.Node,
	beatDone chan struct{},
	states []file.State,
) (*Input, error) {
	var err error
	var in = &Input{Node: base.NewEmptyNode(taskCfg.InputID)}

	f, err := filter.NewFilters(taskCfg, leafNode)
	if err != nil {
		return nil, err
	}
	in.AddOutput(f.Node)
	go in.Run()

	// input.New 里会发送事件出来，需要先创建好后续的Output，再创建Input
	in.runner, err = input.New(
		taskCfg.RawConfig, ConnectToTask(in), beatDone, states, nil)
	if err != nil {
		return nil, err
	}

	logp.L.Infof("add input(%s) to global inputMaps", in.ID)
	mtx.Lock()
	defer mtx.Unlock()
	inputMaps[taskCfg.InputID] = in
	return in, err
}

// RemoveInput : 移除全局缓存
func RemoveInput(id string) {
	logp.L.Infof("remove input(%s) in global inputMaps", id)
	mtx.Lock()
	defer mtx.Unlock()
	delete(inputMaps, id)
}

type Input struct {
	*base.Node

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
			if isEnableRateLimiter && cpuLimiter != nil {
				checkInterval := cpuLimiter.GetCheckInterval()
				for {
					if cpuLimiter.Allow() {
						break
					} else {
						time.Sleep(checkInterval)
					}
				}
			}

			data := e.(*util.Data)
			if data.Event.Fields != nil {
				for _, out := range in.Outs {
					select {
					case <-in.End:
						return
					case out <- data:
					}
				}
			}
		}
	}
}

// stop : 停止runner
// 停用的场景:
//   1. 当outs为空后，自动退出
//   2. 当End的channel并主动关闭后
func (in *Input) stop() {
	in.stopOnce.Do(func() {
		in.runner.Stop()
	})
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

	//接收到的事件
	//task.crawlerReceived.Add(1)
	//crawlerReceived.Add(1)

	//if event.Fields == nil {
	//	//采集进度类事件
	//	//task.crawlerState.Add(1)
	//	//crawlerState.Add(1)
	//} else {
	//
	//
	//	for _, out := range in.Outs {
	//		select {
	//		case <- in.End:
	//			in.stop()
	//			return false
	//		case out <- data:
	//		}
	//	}

	//event = task.filters.Run(event)
	//if event != nil {
	//	event = task.processors.Run(event)
	//}
	//
	//if event != nil {
	//	//正常事件
	//	//task.crawlerSendTotal.Add(1)
	//	//crawlerSendTotal.Add(1)
	//	data.Event = *event
	//} else {
	//	//需要丢弃的事件
	//	data.Event.Fields = nil
	//	//task.crawlerDropped.Add(1)
	//	//crawlerDropped.Add(1)
	//}
	//}
	return true
	//return task.sender.OnEvent(data)
}
