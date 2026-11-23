package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPlanMesoSchedule_BoundsAndConvergence(t *testing.T) {
	cfg := Config{
		MicroBaseS:    120,
		MicroOffsetS:  30,
		MicroRestS:    10,
		MesoDurationM: 25,
		MesoRestM:     5,
		MesoCount:     3,
		MacroRestM:    30,
		Host:          "127.0.0.1",
		Port:          8080,
	}

	target := time.Duration(cfg.MesoDurationM) * time.Minute
	minDur := time.Duration(cfg.MicroBaseS-cfg.MicroOffsetS) * time.Second
	maxDur := time.Duration(cfg.MicroBaseS+cfg.MicroOffsetS) * time.Second
	restDur := time.Duration(cfg.MicroRestS) * time.Second

	for round := 0; round < 50; round++ {
		schedule := planMesoSchedule(cfg)
		if len(schedule) == 0 {
			t.Fatal("排期结果不能为空")
		}

		var totalWithRest time.Duration
		for i, d := range schedule {
			if d < minDur || d > maxDur {
				t.Fatalf("第 %d 个小循环时长 %v 超出允许范围 [%v, %v]", i, d, minDur, maxDur)
			}
			// 在到达最后一个元素之前，累计时长必然严格小于目标时长
			if i < len(schedule)-1 {
				totalWithRest += d
				if totalWithRest >= target {
					t.Fatalf("在第 %d 个元素后已达到目标 %v (累计 %v)，不应继续生成后续元素", i, target, totalWithRest)
				}
				totalWithRest += restDur
				if totalWithRest >= target {
					t.Fatalf("在第 %d 个休息后已达到目标 %v (累计 %v)，不应继续生成后续元素", i, target, totalWithRest)
				}
			} else {
				totalWithRest += d
			}
		}

		// 加上最后一个元素（或前序加休息）至少逼近目标：totalWithRest + restDur >= target
		if totalWithRest+restDur < target {
			t.Fatalf("总时长 %v (即使加一次休息) 未达到目标 %v", totalWithRest, target)
		}
	}
}

func TestTimerSnapshot_MonotonicProgressClamping(t *testing.T) {
	start := time.Now()
	snap := &TimerSnapshot{
		Seq:             3,
		Phase:           PhaseMicroRest,
		CurrentStart:    start,
		CurrentDuration: 10 * time.Second,
		InMeso:          true,
		MesoStart:       start.Add(-120 * time.Second),
		MesoDuration:    300 * time.Second,
	}

	// 正常中途读取
	viewMid := snap.Progress(start.Add(4 * time.Second))
	if viewMid.Seq != 3 || viewMid.Phase != PhaseMicroRest {
		t.Fatalf("快照元数据不匹配: %+v", viewMid)
	}
	if viewMid.CurrentElapsed != 4.0 || viewMid.CurrentTotal != 10.0 {
		t.Errorf("当前阶段进度计算错误: got %v/%v", viewMid.CurrentElapsed, viewMid.CurrentTotal)
	}
	if viewMid.MesoElapsed != 124.0 || viewMid.MesoTotal != 300.0 || !viewMid.InMeso {
		t.Errorf("中循环进度计算错误: got %v/%v (inMeso=%v)", viewMid.MesoElapsed, viewMid.MesoTotal, viewMid.InMeso)
	}

	// 超时钳制（不得超过总时长）
	viewOver := snap.Progress(start.Add(25 * time.Second))
	if viewOver.CurrentElapsed != 10.0 {
		t.Errorf("超出时长后应钳制在 10.0，实际得到 %v", viewOver.CurrentElapsed)
	}

	// 负时间钳制（不得小于 0）
	viewBefore := snap.Progress(start.Add(-5 * time.Second))
	if viewBefore.CurrentElapsed != 0 {
		t.Errorf("早于起点时应钳制在 0，实际得到 %v", viewBefore.CurrentElapsed)
	}
}

func TestTimerEngine_PhaseTransitionsAndNonBlockingSound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 构造毫秒级精算参数：
	// 每个中循环目标 50ms，小循环固定 20ms，小休息 10ms =>
	// 小循环1(20ms) + 小休(10ms) + 小循环2(20ms) = 50ms 恰好达到目标（2 个小循环，1 次小休）
	// 共 2 组中循环：第 1 组后进 meso_rest(20ms)，第 2 组后进 macro_rest(25ms)
	timings := cycleTimings{
		microBase:    20 * time.Millisecond,
		microOffset:  0,
		microRest:    10 * time.Millisecond,
		mesoTarget:   50 * time.Millisecond,
		mesoRest:     20 * time.Millisecond,
		mesoCount:    2,
		macroRest:    25 * time.Millisecond,
	}

	var soundMu sync.Mutex
	var sounds []string

	engine := newTimerEngineWithTimings(ctx, timings, func(sound string) {
		soundMu.Lock()
		sounds = append(sounds, sound)
		soundMu.Unlock()
		// 故意模拟慢速阻塞音效（150ms > 整个大循环时长），验证绝不阻塞计时状态机！
		time.Sleep(150 * time.Millisecond)
	})

	subCh, unsub := engine.Subscribe()
	defer unsub()

	go engine.Run()

	// 期望捕获的阶段序列：
	// 第1组中循环: focus -> micro_rest -> focus
	// 第1组中休:   meso_rest
	// 第2组中循环: focus -> micro_rest -> focus
	// 大循环休息:  macro_rest
	// 下一轮开始:  focus
	expectedPhases := []struct {
		phase  Phase
		inMeso bool
	}{
		{PhaseFocus, true},
		{PhaseMicroRest, true},
		{PhaseFocus, true},
		{PhaseMesoRest, false},
		{PhaseFocus, true},
		{PhaseMicroRest, true},
		{PhaseFocus, true},
		{PhaseMacroRest, false},
		{PhaseFocus, true},
	}

	startWall := time.Now()
	for i, exp := range expectedPhases {
		select {
		case view := <-subCh:
			if view.Phase != exp.phase || view.InMeso != exp.inMeso {
				t.Fatalf("步骤 %d: 期望阶段 (%s, inMeso=%v)，实际得到 (%s, inMeso=%v)",
					i, exp.phase, exp.inMeso, view.Phase, view.InMeso)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("等待步骤 %d (%s) 超时", i, exp.phase)
		}
	}

	elapsed := time.Since(startWall)
	// 理论总耗时 = 50ms + 20ms + 50ms + 25ms = 145ms。
	// 如果被 7 次 150ms 音效同步阻塞，耗时将超过 1050ms！
	if elapsed > 400*time.Millisecond {
		t.Fatalf("计时循环疑似被音效阻塞！总耗时高达 %v（理论约 145ms）", elapsed)
	}
}

func TestTimerEngine_ResetFromZeroAndQuit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 设置较长的小休息时长(200ms)，mesoTarget=300ms > 30ms+200ms，必然生成至少 2 个小循环并进入 micro_rest
	timings := cycleTimings{
		microBase:   30 * time.Millisecond,
		microOffset: 0,
		microRest:   200 * time.Millisecond,
		mesoTarget:  300 * time.Millisecond,
		mesoRest:    200 * time.Millisecond,
		mesoCount:   2,
		macroRest:   200 * time.Millisecond,
	}

	engine := newTimerEngineWithTimings(ctx, timings, nil)
	subCh, unsub := engine.Subscribe()
	defer unsub()

	go engine.Run()

	// 1. 收到初始 focus
	v1 := <-subCh
	if v1.Phase != PhaseFocus || v1.Seq != 1 {
		t.Fatalf("期望初始状态为 focus (seq=1)，实际为 %+v", v1)
	}

	// 2. 等待进入 micro_rest (约 30ms 后)
	v2 := <-subCh
	if v2.Phase != PhaseMicroRest || v2.Seq != 2 {
		t.Fatalf("期望第2状态为 micro_rest (seq=2)，实际为 %+v", v2)
	}

	// 3. 在 200ms 的 micro_rest 刚开始时立即触发 Reset()，必须立刻（<30ms）跳回全新大循环的 focus
	resetTime := time.Now()
	engine.Reset()

	select {
	case v3 := <-subCh:
		if time.Since(resetTime) > 50*time.Millisecond {
			t.Fatalf("Reset 响应过慢: %v", time.Since(resetTime))
		}
		if v3.Phase != PhaseFocus || !v3.InMeso || v3.Seq != 3 {
			t.Fatalf("Reset 后期望立即回到全新 focus (seq=3, inMeso=true)，实际为 %+v", v3)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Reset 后未及时收到新的 focus 状态广播")
	}

	// 4. 调用 Quit() 必须干净退出引擎主循环
	engine.Quit()
	select {
	case <-engine.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("调用 Quit() 后引擎未能及时退出")
	}
}
