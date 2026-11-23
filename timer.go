package main

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// Phase 表示番茄钟当前所处的阶段
type Phase string

const (
	PhaseFocus     Phase = "focus"
	PhaseMicroRest Phase = "micro_rest"
	PhaseMesoRest  Phase = "meso_rest"
	PhaseMacroRest Phase = "macro_rest"
)

// TimerSnapshot 保存不可变的阶段计时状态（内部时间携带单调时钟）
type TimerSnapshot struct {
	Seq             uint64
	Phase           Phase
	CurrentStart    time.Time
	CurrentDuration time.Duration
	InMeso          bool
	MesoStart       time.Time
	MesoDuration    time.Duration
}

// StatusView 是对外暴露（GUI 与 Web JSON/SSE）的标准化进度视图
type StatusView struct {
	Seq            uint64  `json:"seq"`
	Phase          Phase   `json:"phase"`
	CurrentTotal   float64 `json:"current_total"`
	CurrentElapsed float64 `json:"current_elapsed"`
	InMeso         bool    `json:"in_meso"`
	MesoTotal      float64 `json:"meso_total"`
	MesoElapsed    float64 `json:"meso_elapsed"`
}

// Progress 基于单调时钟差值计算指定时刻的精确进度，并钳制在合法区间内
func (s *TimerSnapshot) Progress(now time.Time) StatusView {
	if s == nil {
		return StatusView{Phase: PhaseFocus}
	}

	cTotal := s.CurrentDuration.Seconds()
	if cTotal < 0 {
		cTotal = 0
	}
	cElapsed := now.Sub(s.CurrentStart).Seconds()
	if cElapsed < 0 {
		cElapsed = 0
	} else if cElapsed > cTotal {
		cElapsed = cTotal
	}

	var mTotal, mElapsed float64
	if s.InMeso {
		mTotal = s.MesoDuration.Seconds()
		if mTotal < 0 {
			mTotal = 0
		}
		mElapsed = now.Sub(s.MesoStart).Seconds()
		if mElapsed < 0 {
			mElapsed = 0
		} else if mElapsed > mTotal {
			mElapsed = mTotal
		}
	}

	return StatusView{
		Seq:            s.Seq,
		Phase:          s.Phase,
		CurrentTotal:   cTotal,
		CurrentElapsed: cElapsed,
		InMeso:         s.InMeso,
		MesoTotal:      mTotal,
		MesoElapsed:    mElapsed,
	}
}

// cycleTimings 内部归一化后的时间参数（支持生产秒/分配置与测试毫秒注入）
type cycleTimings struct {
	microBase   time.Duration
	microOffset time.Duration
	microRest   time.Duration
	mesoTarget  time.Duration
	mesoRest    time.Duration
	mesoCount   int
	macroRest   time.Duration
}

func timingsFromConfig(cfg Config) cycleTimings {
	return cycleTimings{
		microBase:   time.Duration(cfg.MicroBaseS) * time.Second,
		microOffset: time.Duration(cfg.MicroOffsetS) * time.Second,
		microRest:   time.Duration(cfg.MicroRestS) * time.Second,
		mesoTarget:  time.Duration(cfg.MesoDurationM) * time.Minute,
		mesoRest:    time.Duration(cfg.MesoRestM) * time.Minute,
		mesoCount:   cfg.MesoCount,
		macroRest:   time.Duration(cfg.MacroRestM) * time.Minute,
	}
}

// planMesoSchedule 根据配置生成一组随机小循环专注时长
func planMesoSchedule(cfg Config) []time.Duration {
	return planScheduleFromTimings(timingsFromConfig(cfg))
}

func planScheduleFromTimings(t cycleTimings) []time.Duration {
	minDur := t.microBase - t.microOffset
	maxDur := t.microBase + t.microOffset
	if minDur <= 0 {
		minDur = time.Second
	}
	if maxDur < minDur {
		maxDur = minDur
	}

	var durations []time.Duration
	var currentTotal time.Duration

	// 判断是否按整秒粒度随机（保持生产配置下秒级整洁）
	wholeSeconds := (minDur%time.Second == 0) && (maxDur%time.Second == 0)

	for {
		var d time.Duration
		if maxDur == minDur {
			d = minDur
		} else if wholeSeconds {
			minSec := int64(minDur / time.Second)
			maxSec := int64(maxDur / time.Second)
			d = time.Duration(minSec+rand.Int64N(maxSec-minSec+1)) * time.Second
		} else {
			span := int64(maxDur - minDur)
			d = minDur + time.Duration(rand.Int64N(span+1))
		}

		durations = append(durations, d)
		currentTotal += d
		if currentTotal >= t.mesoTarget {
			break
		}

		currentTotal += t.microRest
		if currentTotal >= t.mesoTarget {
			break
		}
	}

	return durations
}

// TimerEngine 管理番茄钟状态流转、无锁状态读取、SSE 广播、从 0 重置及安全退出
type TimerEngine struct {
	ctx     context.Context
	cancel  context.CancelFunc
	timings cycleTimings
	onSound func(string)

	snapshot atomic.Pointer[TimerSnapshot]
	seq      atomic.Uint64

	resetCh chan struct{}
	doneCh  chan struct{}

	subMu       sync.Mutex
	nextSubID   uint64
	subscribers map[uint64]chan StatusView
}

// NewTimerEngine 使用生产配置创建计时引擎
func NewTimerEngine(parentCtx context.Context, cfg Config, onSound func(string)) *TimerEngine {
	return newTimerEngineWithTimings(parentCtx, timingsFromConfig(cfg), onSound)
}

func newTimerEngineWithTimings(parentCtx context.Context, timings cycleTimings, onSound func(string)) *TimerEngine {
	ctx, cancel := context.WithCancel(parentCtx)
	e := &TimerEngine{
		ctx:         ctx,
		cancel:      cancel,
		timings:     timings,
		onSound:     onSound,
		resetCh:     make(chan struct{}, 1),
		doneCh:      make(chan struct{}),
		subscribers: make(map[uint64]chan StatusView),
	}
	return e
}

// Status 无锁读取当前时刻的精确进度视图
func (e *TimerEngine) Status() StatusView {
	return e.snapshot.Load().Progress(time.Now())
}

// Snapshot 无锁读取当前原始不可变状态指针
func (e *TimerEngine) Snapshot() *TimerSnapshot {
	return e.snapshot.Load()
}

// Subscribe 订阅阶段切换或重置事件，返回只读通道与取消订阅函数
func (e *TimerEngine) Subscribe() (<-chan StatusView, func()) {
	ch := make(chan StatusView, 16)
	e.subMu.Lock()
	e.nextSubID++
	id := e.nextSubID
	e.subscribers[id] = ch
	e.subMu.Unlock()

	unsubscribe := func() {
		e.subMu.Lock()
		if _, ok := e.subscribers[id]; ok {
			delete(e.subscribers, id)
		}
		e.subMu.Unlock()
	}
	return ch, unsubscribe
}

// Reset 立即中断当前周期，从大循环第 1 组第 1 个小循环 0 秒重新开始
func (e *TimerEngine) Reset() {
	select {
	case e.resetCh <- struct{}{}:
	default:
	}
}

// Quit 触发计时引擎及关联上下文优雅退出
func (e *TimerEngine) Quit() {
	e.cancel()
}

// Done 返回引擎主循环完全退出的信号通道
func (e *TimerEngine) Done() <-chan struct{} {
	return e.doneCh
}

// Context 返回引擎的生命周期上下文
func (e *TimerEngine) Context() context.Context {
	return e.ctx
}

// Run 启动计时主循环（阻塞直到 Quit 或父 Context 取消）
func (e *TimerEngine) Run() {
	defer close(e.doneCh)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("计时器循环崩溃: %v\n", r)
		}
	}()

	log.Println("计时器循环已启动")
	for e.ctx.Err() == nil {
		// 排空上一轮可能残留的合并重置信号
		select {
		case <-e.resetCh:
		default:
		}

		if !e.runMacroCycle() {
			if e.ctx.Err() != nil {
				log.Println("计时器循环已安全退出")
				return
			}
			fmt.Println(">>> 番茄钟已重置，从 0 重新开始大循环")
		}
	}
}

// runMacroCycle 执行完整的大循环；若被 Reset 或 Quit 中断则返回 false
func (e *TimerEngine) runMacroCycle() bool {
	fmt.Println(">>> 开始大循环")
	for i := 0; i < e.timings.mesoCount; i++ {
		isLast := (i == e.timings.mesoCount-1)
		if !e.runMesoCycle(i+1, isLast) {
			return false
		}
	}

	fmt.Println(">>> 大循环结束。")
	e.triggerSound("Sounds/info.mp3")

	fmt.Printf(">>> 大循环休息 (%v)\n", e.timings.macroRest)
	now := time.Now()
	e.publishSnapshot(PhaseMacroRest, now, e.timings.macroRest, false, time.Time{}, 0)
	if !e.waitUntil(now.Add(e.timings.macroRest)) {
		return false
	}

	fmt.Println(">>> 大循环休息结束。")
	e.triggerSound("Sounds/succeed.mp3")
	return true
}

func (e *TimerEngine) runMesoCycle(index int, isLastMeso bool) bool {
	fmt.Printf("  >> 开始中循环 %d/%d\n", index, e.timings.mesoCount)

	microDurations := planScheduleFromTimings(e.timings)
	var totalMesoDuration time.Duration
	for i, d := range microDurations {
		totalMesoDuration += d
		if i < len(microDurations)-1 {
			totalMesoDuration += e.timings.microRest
		}
	}

	fmt.Printf("  >> 计划: %d 个小循环。中循环实际总时长: %v\n", len(microDurations), totalMesoDuration)

	mesoStart := time.Now()
	var cumulative time.Duration

	for i, duration := range microDurations {
		fmt.Printf("    > 小循环 %d/%d: %v\n", i+1, len(microDurations), duration)

		stageStart := mesoStart.Add(cumulative)
		e.publishSnapshot(PhaseFocus, stageStart, duration, true, mesoStart, totalMesoDuration)

		cumulative += duration
		if !e.waitUntil(mesoStart.Add(cumulative)) {
			return false
		}

		fmt.Println("    > 小循环结束。")
		e.triggerSound("Sounds/warning.mp3")

		if i < len(microDurations)-1 && e.timings.microRest > 0 {
			fmt.Printf("    > 小循环休息 (%v)\n", e.timings.microRest)

			restStart := mesoStart.Add(cumulative)
			e.publishSnapshot(PhaseMicroRest, restStart, e.timings.microRest, true, mesoStart, totalMesoDuration)

			cumulative += e.timings.microRest
			if !e.waitUntil(mesoStart.Add(cumulative)) {
				return false
			}

			fmt.Println("    > 小循环休息结束。")
			e.triggerSound("Sounds/succeed.mp3")
		}
	}

	if !isLastMeso {
		fmt.Println("  >> 中循环结束。")
		e.triggerSound("Sounds/info.mp3")

		if e.timings.mesoRest > 0 {
			fmt.Printf("  >> 中循环休息 (%v)\n", e.timings.mesoRest)
			restStart := time.Now()
			e.publishSnapshot(PhaseMesoRest, restStart, e.timings.mesoRest, false, time.Time{}, 0)
			if !e.waitUntil(restStart.Add(e.timings.mesoRest)) {
				return false
			}
			fmt.Println("  >> 中循环休息结束。")
			e.triggerSound("Sounds/succeed.mp3")
		}
	} else {
		fmt.Println("  >> 本组最后一个中循环结束。进入大循环休息序列。")
	}

	return true
}

func (e *TimerEngine) waitUntil(target time.Time) bool {
	sleepDur := time.Until(target)
	if sleepDur <= 0 {
		select {
		case <-e.ctx.Done():
			return false
		case <-e.resetCh:
			return false
		default:
			return true
		}
	}

	timer := time.NewTimer(sleepDur)
	defer timer.Stop()

	select {
	case <-e.ctx.Done():
		return false
	case <-e.resetCh:
		return false
	case <-timer.C:
		return true
	}
}

func (e *TimerEngine) publishSnapshot(phase Phase, curStart time.Time, curDur time.Duration, inMeso bool, mesoStart time.Time, mesoDur time.Duration) {
	seq := e.seq.Add(1)
	snap := &TimerSnapshot{
		Seq:             seq,
		Phase:           phase,
		CurrentStart:    curStart,
		CurrentDuration: curDur,
		InMeso:          inMeso,
		MesoStart:       mesoStart,
		MesoDuration:    mesoDur,
	}
	e.snapshot.Store(snap)

	view := snap.Progress(curStart)
	e.subMu.Lock()
	for _, ch := range e.subscribers {
		select {
		case ch <- view:
		default:
			// 若慢速订阅者缓冲已满，丢弃最旧事件以保证最新状态送达且绝不阻塞计时协程
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- view:
			default:
			}
		}
	}
	e.subMu.Unlock()
}

func (e *TimerEngine) triggerSound(name string) {
	if e.onSound != nil {
		go e.onSound(name)
	}
}
