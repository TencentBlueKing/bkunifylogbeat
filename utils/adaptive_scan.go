// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company. All rights reserved.
// Licensed under the MIT License.

package utils

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
)

const (
	// 单 input 扫描耗时 EWMA 参与控制；聚合占空比 EWMA 仅用于诊断快照。
	adaptiveScanEWMAAlpha = 0.5
	// 占空比落在目标值上下 15% 时不调整，防止 multiplier 在目标附近来回振荡。
	adaptiveScanDeadbandRatio = 0.15
	// 超预算越严重，升档步长越大；恢复时保持保守，避免负载抖动导致周期骤降。
	adaptiveScanNormalGrowthRatio    = 2.0
	adaptiveScanModerateGrowthRatio  = 4.0
	adaptiveScanEmergencyGrowthRatio = 8.0
	adaptiveScanModerateDutyRatio    = 2.0
	adaptiveScanEmergencyDutyRatio   = 8.0
	adaptiveScanRecoveryRatio        = 0.75
	adaptiveScanSearchIterations     = 60
	adaptiveScanInitialMultiple      = 1.0
	// 变更日志按 input 限频，聚合快照则提供固定周期的全局视图。
	adaptiveScanLogMinInterval   = time.Minute
	adaptiveScanSnapshotInterval = 5 * time.Minute
	adaptiveScanSnapshotTopK     = 5
)

// AdaptiveScanSettings 控制单 input 周期计算和全局扫描 CPU 预算。
// ScanCPUPercent 表示扫描耗时占单核时间的百分比。
type AdaptiveScanSettings struct {
	MinScanFrequency time.Duration
	ScanCPUPercent   float64
	ControlInterval  time.Duration
}

type adaptiveScanInputState struct {
	ewmaScanNanos      float64
	lastLoggedInterval time.Duration
	lastLogAt          time.Time
	baseInterval       time.Duration
	localInterval      time.Duration
	requestedInterval  time.Duration
	effectiveInterval  time.Duration
	lastScanDuration   time.Duration
}

// AdaptiveScanIntervalLog 表示一条经过限频的扫描周期变更记录。
type AdaptiveScanIntervalLog struct {
	RunnerID          uint64
	InputID           string
	TaskIDs           []string
	DataIDs           []int
	Paths             []string
	BaseInterval      time.Duration
	RequestedInterval time.Duration
	EffectiveInterval time.Duration
	Multiplier        float64
	ScanDuration      time.Duration
}

// AdaptiveScanIntervalDistribution 是周期快照中固定桶数的扫描周期分布。
type AdaptiveScanIntervalDistribution struct {
	AtMinimum     int
	UpTo1Second   int
	UpTo2Seconds  int
	UpTo5Seconds  int
	Above5Seconds int
	AtBase        int
}

// AdaptiveScanSnapshotInput 表示生效周期与基础周期比值最大的 input 之一。
type AdaptiveScanSnapshotInput struct {
	RunnerID          uint64
	InputID           string
	TaskIDs           []string
	DataIDs           []int
	BaseInterval      time.Duration
	RequestedInterval time.Duration
	EffectiveInterval time.Duration
	ScanDuration      time.Duration
}

// AdaptiveScanSnapshot 表示一条固定大小的控制器聚合快照。
type AdaptiveScanSnapshot struct {
	Inputs     int
	Shortened  int
	Multiplier float64
	ScanDuty   float64
	TargetDuty float64
	// MinimumPossibleDuty 是所有活跃 input 都退回基础周期后的最低聚合扫描占空比。
	MinimumPossibleDuty float64
	// BudgetSaturated 表示基础周期上限使目标预算无法满足。
	BudgetSaturated bool
	Intervals       AdaptiveScanIntervalDistribution
	SlowestInputs   []AdaptiveScanSnapshotInput
}

// AdaptiveScanController 计算每个 input 的扫描周期，并维护限制聚合扫描开销的全局乘数。
type AdaptiveScanController struct {
	minScanFrequency time.Duration
	controlInterval  time.Duration
	targetDuty       float64

	// mu 只保护各 Runner 的局部 EWMA、最终周期和日志状态。
	mu     sync.Mutex
	inputs map[uint64]*adaptiveScanInputState

	// 热路径与 governor 通过原子值交换累计耗时、全局 multiplier 和最近占空比，
	// 避免每个 Runner 在计算周期时争用 governor 的采样锁。
	totalScanNanos  atomic.Int64
	multiplierBits  atomic.Uint64
	lastDutyBits    atomic.Uint64
	minimumDutyBits atomic.Uint64
	budgetSaturated atomic.Bool

	// sampleMu 保护相邻采样点及占空比 EWMA，仅由 governor 周期更新。
	sampleMu        sync.Mutex
	lastSampleAt    time.Time
	lastSampleTotal int64
	smoothedDuty    float64
	hasSmoothedDuty bool

	// lifecycleMu 保证 Start/Stop 幂等，并避免重复创建或关闭后台 goroutine。
	lifecycleMu sync.Mutex
	running     bool
	done        chan struct{}
	wg          sync.WaitGroup

	now               func() time.Time
	logIntervalChange func(AdaptiveScanIntervalLog)
	snapshotInterval  time.Duration
	logSnapshot       func(AdaptiveScanSnapshot)
	logInputSnapshots func([]AdaptiveScanIntervalLog)
}

// NewAdaptiveScanController 创建自适应扫描控制器。
func NewAdaptiveScanController(settings AdaptiveScanSettings) (*AdaptiveScanController, error) {
	if settings.MinScanFrequency <= 0 {
		return nil, fmt.Errorf("min scan frequency must be greater than 0")
	}
	if settings.ScanCPUPercent <= 0 || settings.ScanCPUPercent > 100 ||
		math.IsNaN(settings.ScanCPUPercent) || math.IsInf(settings.ScanCPUPercent, 0) {
		return nil, fmt.Errorf("scan CPU percent must be greater than 0 and no more than 100")
	}
	if settings.ControlInterval <= 0 {
		return nil, fmt.Errorf("control interval must be greater than 0")
	}

	controller := &AdaptiveScanController{
		minScanFrequency:  settings.MinScanFrequency,
		controlInterval:   settings.ControlInterval,
		targetDuty:        settings.ScanCPUPercent / 100,
		inputs:            make(map[uint64]*adaptiveScanInputState),
		now:               time.Now,
		logIntervalChange: logAdaptiveScanIntervalChange,
		snapshotInterval:  adaptiveScanSnapshotInterval,
		logSnapshot:       logAdaptiveScanSnapshot,
		logInputSnapshots: logAdaptiveScanInputSnapshots,
	}
	controller.setMultiplier(adaptiveScanInitialMultiple)
	return controller, nil
}

// NextInterval 记录最新扫描开销并计算候选周期。
// 配置周期始终是上限，因此开启自适应后不会比原配置扫描得更慢。
func (c *AdaptiveScanController) NextInterval(inputID uint64, base, scanDuration time.Duration) time.Duration {
	if base <= 0 || scanDuration <= 0 {
		return base
	}

	// 累计值供 governor 按采样窗口计算所有 Runner 的聚合扫描占空比。
	c.totalScanNanos.Add(int64(scanDuration))

	c.mu.Lock()
	state, ok := c.inputs[inputID]
	if !ok {
		state = &adaptiveScanInputState{ewmaScanNanos: float64(scanDuration)}
		c.inputs[inputID] = state
	} else {
		state.ewmaScanNanos = adaptiveScanEWMAAlpha*float64(scanDuration) +
			(1-adaptiveScanEWMAAlpha)*state.ewmaScanNanos
	}
	ewmaScanNanos := state.ewmaScanNanos
	minimum := c.minScanFrequency
	if base < minimum {
		minimum = base
	}

	// 第一层局部控制：local = EWMA(scanDuration) / targetDuty。
	// 例如目标占空比为 5%，一次扫描耗时 50ms，则局部周期应约为 1s。
	localNanos := ewmaScanNanos / c.targetDuty
	local := base
	if localNanos < float64(base) {
		local = clampDuration(time.Duration(localNanos), minimum, base)
	}
	// governor 使用各 input 的稳态成本模型求解全局 multiplier；
	// base/local 决定该 input 退回原配置周期前仍有多少调节空间。
	state.baseInterval = base
	state.localInterval = local
	c.mu.Unlock()

	// 第二层全局控制：所有 input 的局部周期统一乘以 governor 的 multiplier。
	// 最终仍限制在 [minimum, base]，不会突破最小周期，也不会比原配置扫描得更慢。
	effective := float64(local) * c.Multiplier()
	var interval time.Duration
	if effective >= float64(base) {
		interval = base
	} else {
		interval = clampDuration(time.Duration(effective), minimum, base)
	}

	return interval
}

// ObserveApplied 接收 Beats 归一化后的最终扫描周期。
// 日志与快照只使用 applied，确保观测值和 Runner 实际等待时间一致。
func (c *AdaptiveScanController) ObserveApplied(
	inputID uint64,
	base, requested, applied, scanDuration time.Duration,
) {
	c.mu.Lock()
	state := c.inputs[inputID]
	if state == nil {
		c.mu.Unlock()
		return
	}
	state.baseInterval = base
	state.requestedInterval = requested
	state.effectiveInterval = applied
	state.lastScanDuration = scanDuration
	c.mu.Unlock()

	c.maybeLogIntervalChange(inputID, base, requested, applied, scanDuration)
}

// TotalScanDuration 返回累计实测扫描耗时。
func (c *AdaptiveScanController) TotalScanDuration() time.Duration {
	return time.Duration(c.totalScanNanos.Load())
}

func (c *AdaptiveScanController) removeInput(inputID uint64) {
	// Runner 完全停止后删除局部 EWMA，避免长期 Reload 积累已经失效的实例状态。
	c.mu.Lock()
	delete(c.inputs, inputID)
	c.mu.Unlock()
}

// Multiplier 返回当前全局扫描周期乘数。
func (c *AdaptiveScanController) Multiplier() float64 {
	return math.Float64frombits(c.multiplierBits.Load())
}

// Start 启动聚合扫描占空比的周期采样。
func (c *AdaptiveScanController) Start() {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.running {
		return
	}

	c.sampleMu.Lock()
	c.lastSampleAt = time.Now()
	c.lastSampleTotal = c.totalScanNanos.Load()
	c.smoothedDuty = 0
	c.hasSmoothedDuty = false
	c.lastDutyBits.Store(math.Float64bits(0))
	c.minimumDutyBits.Store(math.Float64bits(0))
	c.budgetSaturated.Store(false)
	c.sampleMu.Unlock()

	c.done = make(chan struct{})
	c.running = true
	c.wg.Add(1)
	go c.runGovernor(c.done)
}

// Stop 停止聚合扫描占空比的周期采样。
func (c *AdaptiveScanController) Stop() {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if !c.running {
		return
	}

	close(c.done)
	c.running = false
	c.wg.Wait()
}

func (c *AdaptiveScanController) runGovernor(done <-chan struct{}) {
	defer c.wg.Done()
	// control ticker 调整全局 multiplier；snapshot ticker 只负责诊断输出，
	// 两者分离可避免日志周期影响控制收敛速度。
	ticker := time.NewTicker(c.controlInterval)
	defer ticker.Stop()
	snapshotTicker := time.NewTicker(c.snapshotInterval)
	defer snapshotTicker.Stop()

	for {
		select {
		case <-done:
			return
		case now := <-ticker.C:
			c.observeSample(now, c.totalScanNanos.Load())
		case <-snapshotTicker.C:
			c.logSnapshot(c.snapshot())
			c.logInputSnapshots(c.inputSnapshots())
		}
	}
}

func (c *AdaptiveScanController) observeSample(now time.Time, totalNanos int64) {
	c.sampleMu.Lock()
	// 累计值倒退表示采样基线已失效，只重置基线，不用异常窗口更新 multiplier。
	if c.lastSampleAt.IsZero() || totalNanos < c.lastSampleTotal {
		c.lastSampleAt = now
		c.lastSampleTotal = totalNanos
		c.sampleMu.Unlock()
		return
	}

	elapsed := now.Sub(c.lastSampleAt)
	deltaNanos := totalNanos - c.lastSampleTotal
	c.lastSampleAt = now
	c.lastSampleTotal = totalNanos

	if elapsed <= 0 {
		c.sampleMu.Unlock()
		return
	}
	// 聚合占空比 = 采样窗口内所有扫描耗时增量 / 实际墙钟时间。
	duty := float64(deltaNanos) / float64(elapsed)
	if c.hasSmoothedDuty {
		c.smoothedDuty = adaptiveScanEWMAAlpha*duty +
			(1-adaptiveScanEWMAAlpha)*c.smoothedDuty
	} else {
		c.smoothedDuty = duty
		c.hasSmoothedDuty = true
	}
	smoothedDuty := c.smoothedDuty
	c.sampleMu.Unlock()

	c.lastDutyBits.Store(math.Float64bits(smoothedDuty))
	c.updateMultiplier()
}

func (c *AdaptiveScanController) setMultiplier(value float64) {
	c.multiplierBits.Store(math.Float64bits(value))
}

type adaptiveScanControlCost struct {
	scanNanos  float64
	localNanos float64
	baseNanos  float64
}

type adaptiveScanControlModel struct {
	inputs            int
	maxMultiplier     float64
	currentDuty       float64
	minimumDuty       float64
	desiredMultiplier float64
}

// controlModel 根据各 input 的扫描耗时 EWMA 和周期边界估算稳态聚合占空比。
// 相比直接使用短采样窗口，模型不会在扫描周期大于 controlInterval 时因空窗口和突发窗口振荡。
func (c *AdaptiveScanController) controlModel(current float64) adaptiveScanControlModel {
	c.mu.Lock()
	costs := make([]adaptiveScanControlCost, 0, len(c.inputs))
	for _, state := range c.inputs {
		if state.ewmaScanNanos <= 0 || state.localInterval <= 0 || state.baseInterval <= 0 {
			continue
		}
		costs = append(costs, adaptiveScanControlCost{
			scanNanos:  state.ewmaScanNanos,
			localNanos: float64(state.localInterval),
			baseNanos:  float64(state.baseInterval),
		})
	}
	c.mu.Unlock()

	model := adaptiveScanControlModel{
		inputs:            len(costs),
		maxMultiplier:     1,
		desiredMultiplier: 1,
	}
	if len(costs) == 0 {
		return model
	}

	for _, cost := range costs {
		ratio := cost.baseNanos / cost.localNanos
		if ratio > model.maxMultiplier {
			model.maxMultiplier = ratio
		}
	}

	dutyAt := func(multiplier float64) float64 {
		var duty float64
		for _, cost := range costs {
			interval := cost.localNanos * multiplier
			if interval > cost.baseNanos {
				interval = cost.baseNanos
			}
			duty += cost.scanNanos / interval
		}
		return duty
	}

	current = clampFloat(current, 1, model.maxMultiplier)
	model.currentDuty = dutyAt(current)
	model.minimumDuty = dutyAt(model.maxMultiplier)
	if dutyAt(1) <= c.targetDuty {
		return model
	}
	if model.minimumDuty > c.targetDuty {
		model.desiredMultiplier = model.maxMultiplier
		return model
	}

	// dutyAt 随 multiplier 单调不增；二分得到满足预算的最小 multiplier，
	// 避免固定上限阻断本可满足的配置，也避免超出所有 input 的实际调节空间。
	low, high := 1.0, model.maxMultiplier
	for range adaptiveScanSearchIterations {
		middle := (low + high) / 2
		if dutyAt(middle) > c.targetDuty {
			low = middle
		} else {
			high = middle
		}
	}
	model.desiredMultiplier = high
	return model
}

func (c *AdaptiveScanController) updateMultiplier() {
	current := c.Multiplier()
	model := c.controlModel(current)
	c.minimumDutyBits.Store(math.Float64bits(model.minimumDuty))
	c.budgetSaturated.Store(model.inputs > 0 && model.minimumDuty > c.targetDuty)
	if model.inputs == 0 {
		c.setMultiplier(adaptiveScanInitialMultiple)
		return
	}

	current = clampFloat(current, 1, model.maxMultiplier)
	// input 删除或局部周期变化可能降低动态上限，应立即收回已经无效的 multiplier。
	if current != c.Multiplier() {
		c.setMultiplier(current)
	}

	// 目标附近保留 15% 死区，避免扫描耗时的小幅波动导致周期频繁变化。
	if model.currentDuty >= c.targetDuty*(1-adaptiveScanDeadbandRatio) &&
		model.currentDuty <= c.targetDuty*(1+adaptiveScanDeadbandRatio) {
		return
	}

	var next float64
	if model.currentDuty > c.targetDuty {
		// 严重超预算时按 8 倍、4 倍、2 倍分级快速升档，但绝不越过模型求得的目标。
		dutyRatio := model.currentDuty / c.targetDuty
		growthRatio := adaptiveScanNormalGrowthRatio
		switch {
		case dutyRatio >= adaptiveScanEmergencyDutyRatio:
			growthRatio = adaptiveScanEmergencyGrowthRatio
		case dutyRatio >= adaptiveScanModerateDutyRatio:
			growthRatio = adaptiveScanModerateGrowthRatio
		}
		next = math.Min(model.desiredMultiplier, current*growthRatio)
	} else {
		// 预算有余量时每轮最多回收 25%，保留原控制器的保守恢复特性。
		next = math.Max(model.desiredMultiplier, current*adaptiveScanRecoveryRatio)
	}

	c.setMultiplier(clampFloat(next, 1, model.maxMultiplier))
}

func (c *AdaptiveScanController) maybeLogIntervalChange(
	runnerID uint64,
	base, requested, effective, scanDuration time.Duration,
) {
	metadata, ok := adaptiveScanInputMetadata(runnerID)
	if !ok {
		return
	}

	now := c.now()
	c.mu.Lock()
	state := c.inputs[runnerID]
	if state == nil {
		c.mu.Unlock()
		return
	}
	// 首次仅在周期确实缩短或 Beats 发生兜底时打印；之后至少间隔一分钟，
	// 且只有倍数变化或跨固定周期桶时才打印，兼顾可诊断性与日志量。
	shouldLog := (state.lastLoggedInterval == 0 && (effective < base || requested != effective)) ||
		(state.lastLoggedInterval != 0 &&
			now.Sub(state.lastLogAt) >= adaptiveScanLogMinInterval &&
			isMeaningfulAdaptiveScanIntervalChange(state.lastLoggedInterval, effective, base))
	if shouldLog {
		state.lastLoggedInterval = effective
		state.lastLogAt = now
	}
	c.mu.Unlock()
	if !shouldLog {
		return
	}

	c.logIntervalChange(AdaptiveScanIntervalLog{
		RunnerID:          runnerID,
		InputID:           metadata.InputID,
		TaskIDs:           metadata.TaskIDs,
		DataIDs:           metadata.DataIDs,
		Paths:             metadata.Paths,
		BaseInterval:      base,
		RequestedInterval: requested,
		EffectiveInterval: effective,
		Multiplier:        c.Multiplier(),
		ScanDuration:      scanDuration,
	})
}

func (c *AdaptiveScanController) snapshot() AdaptiveScanSnapshot {
	type candidate struct {
		runnerID uint64
		score    float64
		state    adaptiveScanInputState
	}

	snapshot := AdaptiveScanSnapshot{
		Multiplier:          c.Multiplier(),
		ScanDuty:            math.Float64frombits(c.lastDutyBits.Load()),
		TargetDuty:          c.targetDuty,
		MinimumPossibleDuty: math.Float64frombits(c.minimumDutyBits.Load()),
		BudgetSaturated:     c.budgetSaturated.Load(),
	}
	var candidates []candidate

	c.mu.Lock()
	snapshot.Inputs = len(c.inputs)
	for runnerID, state := range c.inputs {
		if state.baseInterval <= 0 || state.effectiveInterval <= 0 {
			continue
		}
		if state.effectiveInterval < state.baseInterval {
			snapshot.Shortened++
		}

		minimum := c.minScanFrequency
		if state.baseInterval < minimum {
			minimum = state.baseInterval
		}
		switch {
		case state.effectiveInterval >= state.baseInterval:
			snapshot.Intervals.AtBase++
		case state.effectiveInterval <= minimum:
			snapshot.Intervals.AtMinimum++
		case state.effectiveInterval <= time.Second:
			snapshot.Intervals.UpTo1Second++
		case state.effectiveInterval <= 2*time.Second:
			snapshot.Intervals.UpTo2Seconds++
		case state.effectiveInterval <= 5*time.Second:
			snapshot.Intervals.UpTo5Seconds++
		default:
			snapshot.Intervals.Above5Seconds++
		}

		candidates = append(candidates, candidate{
			runnerID: runnerID,
			score:    float64(state.effectiveInterval) / float64(state.baseInterval),
			state:    *state,
		})
	}
	c.mu.Unlock()

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].runnerID < candidates[j].runnerID
		}
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > adaptiveScanSnapshotTopK {
		candidates = candidates[:adaptiveScanSnapshotTopK]
	}

	for _, item := range candidates {
		record := AdaptiveScanSnapshotInput{
			RunnerID:          item.runnerID,
			BaseInterval:      item.state.baseInterval,
			RequestedInterval: item.state.requestedInterval,
			EffectiveInterval: item.state.effectiveInterval,
			ScanDuration:      item.state.lastScanDuration,
		}
		if metadata, ok := adaptiveScanInputMetadata(item.runnerID); ok {
			record.InputID = metadata.InputID
			record.TaskIDs = metadata.TaskIDs
			record.DataIDs = metadata.DataIDs
		}
		snapshot.SlowestInputs = append(snapshot.SlowestInputs, record)
	}
	return snapshot
}

func (c *AdaptiveScanController) inputSnapshots() []AdaptiveScanIntervalLog {
	type stateSnapshot struct {
		runnerID uint64
		state    adaptiveScanInputState
	}

	c.mu.Lock()
	states := make([]stateSnapshot, 0, len(c.inputs))
	for runnerID, state := range c.inputs {
		states = append(states, stateSnapshot{runnerID: runnerID, state: *state})
	}
	c.mu.Unlock()

	sort.Slice(states, func(i, j int) bool {
		return states[i].runnerID < states[j].runnerID
	})
	records := make([]AdaptiveScanIntervalLog, 0, len(states))
	for _, item := range states {
		record := AdaptiveScanIntervalLog{
			RunnerID:          item.runnerID,
			BaseInterval:      item.state.baseInterval,
			RequestedInterval: item.state.requestedInterval,
			EffectiveInterval: item.state.effectiveInterval,
			Multiplier:        c.Multiplier(),
			ScanDuration:      item.state.lastScanDuration,
		}
		if metadata, ok := adaptiveScanInputMetadata(item.runnerID); ok {
			record.InputID = metadata.InputID
			record.TaskIDs = metadata.TaskIDs
			record.DataIDs = metadata.DataIDs
			record.Paths = metadata.Paths
		}
		records = append(records, record)
	}
	return records
}

func isMeaningfulAdaptiveScanIntervalChange(previous, current, base time.Duration) bool {
	if previous <= 0 || current <= 0 {
		return true
	}
	larger, smaller := previous, current
	if current > previous {
		larger, smaller = current, previous
	}
	if larger >= 2*smaller {
		return true
	}
	return adaptiveScanIntervalBucket(previous, base) != adaptiveScanIntervalBucket(current, base)
}

func adaptiveScanIntervalBucket(interval, base time.Duration) int {
	switch {
	case interval >= base:
		return 5
	case interval <= 500*time.Millisecond:
		return 0
	case interval <= time.Second:
		return 1
	case interval <= 2*time.Second:
		return 2
	case interval <= 5*time.Second:
		return 3
	default:
		return 4
	}
}

func logAdaptiveScanIntervalChange(record AdaptiveScanIntervalLog) {
	logp.L.Infof(
		"adaptive scan interval changed: runner_id=%d input_id=%s task_ids=%v data_ids=%v paths=%v "+
			"base=%s requested_interval=%s effective_interval=%s multiplier=%.3f scan_duration=%s",
		record.RunnerID,
		record.InputID,
		record.TaskIDs,
		record.DataIDs,
		record.Paths,
		record.BaseInterval,
		record.RequestedInterval,
		record.EffectiveInterval,
		record.Multiplier,
		record.ScanDuration,
	)
}

func logAdaptiveScanSnapshot(snapshot AdaptiveScanSnapshot) {
	logp.L.Infof(
		"adaptive scan snapshot: inputs=%d shortened=%d multiplier=%.3f scan_duty=%.4f "+
			"target_duty=%.4f minimum_possible_duty=%.4f budget_saturated=%t "+
			"intervals=%+v slowest_inputs=%+v",
		snapshot.Inputs,
		snapshot.Shortened,
		snapshot.Multiplier,
		snapshot.ScanDuty,
		snapshot.TargetDuty,
		snapshot.MinimumPossibleDuty,
		snapshot.BudgetSaturated,
		snapshot.Intervals,
		snapshot.SlowestInputs,
	)
}

func logAdaptiveScanInputSnapshots(records []AdaptiveScanIntervalLog) {
	for _, record := range records {
		logp.L.Debugf(
			"adaptive scan input snapshot: runner_id=%d input_id=%s task_ids=%v data_ids=%v paths=%v "+
				"base=%s requested_interval=%s effective_interval=%s multiplier=%.3f scan_duration=%s",
			record.RunnerID,
			record.InputID,
			record.TaskIDs,
			record.DataIDs,
			record.Paths,
			record.BaseInterval,
			record.RequestedInterval,
			record.EffectiveInterval,
			record.Multiplier,
			record.ScanDuration,
		)
	}
}

func clampDuration(value, minimum, maximum time.Duration) time.Duration {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func clampFloat(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
