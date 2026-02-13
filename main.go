package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
)

// Config 保存番茄钟的配置信息
type Config struct {
	MicroBaseS    int `json:"小循环基础时间秒"`
	MicroOffsetS  int `json:"小循环随机偏移秒"`
	MicroRestS    int `json:"小循环休息时间秒"`
	MesoDurationM int `json:"中循环总时间分"`
	MesoRestM     int `json:"中循环休息时间分"`
	MesoCount     int `json:"中循环组数"`
	MacroRestM    int `json:"大循环休息时间分"`
	Port          int `json:"端口"`
}

type GlobalState struct {
	sync.Mutex
	CurrentStartTime time.Time
	CurrentDuration  time.Duration

	MesoStartTime time.Time
	MesoDuration  time.Duration
	InMeso        bool
}

var (
	config        Config
	sampleRate    beep.SampleRate = 44100
	speakerInited bool
	state         GlobalState
)

func main() {
	// 捕获严重崩溃
	defer func() {
		if r := recover(); r != nil {
			log.Printf("严重崩溃: %v\n", r)
		}
	}()

	// 初始化随机数种子
	rand.Seed(time.Now().UnixNano())

	// 加载配置
	if err := loadConfig(); err != nil {
		msg := fmt.Sprintf("加载配置文件失败: %v", err)
		fmt.Println(msg)
		time.Sleep(5 * time.Second)
		return
	}

	if config.Port == 0 {
		config.Port = 8080
	}

	fmt.Println("番茄钟已启动")
	fmt.Printf("配置: %+v\n", config)

	// 在后台协程中初始化音频，避免阻塞主线程
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("音频初始化崩溃: %v", r)
			}
		}()

		err := speaker.Init(sampleRate, sampleRate.N(time.Second/10))
		if err != nil {
			msg := fmt.Sprintf("音频初始化警告: %v", err)
			fmt.Println(msg)
		} else {
			speakerInited = true
			log.Println("音频初始化成功")
		}
	}()

	// 根据构建标签执行条件逻辑

	// 如果包含 'web' 标签，启动 Web 服务器
	startWebServerIfNeeded()

	// 启动核心逻辑循环
	go startTimerLoop()

	// 如果包含 'gui' 标签，启动 GUI，否则阻塞
	startGUIOrBlock()
}

func startTimerLoop() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("计时器循环崩溃: %v", r)
		}
	}()
	log.Println("计时器循环已启动")
	for {
		runMacroCycle()
	}
}

func loadConfig() error {
	file, err := os.Open("config.json")
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	return decoder.Decode(&config)
}

func runMacroCycle() {
	fmt.Println(">>> 开始大循环")
	for i := 0; i < config.MesoCount; i++ {
		isLast := (i == config.MesoCount-1)
		runMesoCycle(i+1, isLast)
	}

	fmt.Println(">>> 大循环结束。")
	playSound("Sounds/info.mp3")

	fmt.Printf(">>> 大循环休息 (%d 分)\n", config.MacroRestM)
	clearMesoTask()
	wait(time.Duration(config.MacroRestM) * time.Minute)

	fmt.Println(">>> 大循环休息结束。")
	playSound("Sounds/succeed.mp3")
}

func runMesoCycle(index int, isLastMeso bool) {
	fmt.Printf("  >> 开始中循环 %d/%d\n", index, config.MesoCount)

	// 规划时间表
	// 目标时间转换为秒
	targetDuration := time.Duration(config.MesoDurationM) * time.Minute
	microDurations := planMesoSchedule(targetDuration)

	// 计算包含休息在内的总时长，用于UI显示
	totalMesoDuration := time.Duration(0)
	for i, d := range microDurations {
		totalMesoDuration += d
		if i < len(microDurations)-1 {
			totalMesoDuration += time.Duration(config.MicroRestS) * time.Second
		}
	}
	setMesoTask(totalMesoDuration)

	fmt.Printf("  >> 计划: %d 个小循环。总时长: %v\n", len(microDurations), targetDuration)

	for i, duration := range microDurations {
		fmt.Printf("    > 小循环 %d/%d: %.0f秒\n", i+1, len(microDurations), duration.Seconds())
		wait(duration)

		fmt.Println("    > 小循环结束。")
		playSound("Sounds/warning.mp3")

		// 如果不是最后一个小循环，进行小休息
		if i < len(microDurations)-1 {
			fmt.Printf("    > 小循环休息 (%d 秒)\n", config.MicroRestS)
			wait(time.Duration(config.MicroRestS) * time.Second)
			fmt.Println("    > 小循环休息结束。")
			playSound("Sounds/succeed.mp3")
		}
	}

	clearMesoTask()

	if !isLastMeso {
		fmt.Println("  >> 中循环结束。")
		playSound("Sounds/info.mp3")

		fmt.Printf("  >> 中循环休息 (%d 分)\n", config.MesoRestM)
		wait(time.Duration(config.MesoRestM) * time.Minute)

		fmt.Println("  >> 中循环休息结束。")
		playSound("Sounds/succeed.mp3")
	} else {
		fmt.Println("  >> 本组最后一个中循环结束。进入大循环休息序列。")
	}
}

// planMesoSchedule 生成一系列小循环的时长
func planMesoSchedule(targetTotal time.Duration) []time.Duration {
	// 转换为秒进行计算
	targetSec := int(targetTotal.Seconds())
	base := config.MicroBaseS
	offset := config.MicroOffsetS
	rest := config.MicroRestS

	minDur := base - offset
	maxDur := base + offset

	// 在 [目标秒数, 目标秒数+60] 范围内随机选择一个实际目标总时间
	actualTarget := targetSec + rand.Intn(61)

	// 确定可行的小循环个数 N 的范围
	// N * minDur + (N-1)*rest <= actualTarget
	// N * maxDur + (N-1)*rest >= actualTarget

	var validN []int
	// 估算 N ≈ actualTarget / (base + rest)
	estN := actualTarget / (base + rest)

	// 在估算值附近搜索
	for n := estN - 5; n <= estN+5; n++ {
		if n <= 0 {
			continue
		}
		minTotal := n*minDur + (n-1)*rest
		maxTotal := n*maxDur + (n-1)*rest

		if actualTarget >= minTotal && actualTarget <= maxTotal {
			validN = append(validN, n)
		}
	}

	if len(validN) == 0 {
		// 备用方案：使用估算值，强制适配
		validN = append(validN, estN)
	}

	// 从有效选项中随机选择一个 N
	n := validN[rand.Intn(len(validN))]

	// 生成前 N-1 个循环的随机时长
	// 最后一个循环将承担剩余时间
	durations := make([]time.Duration, n)

	// 尝试生成一组有效的时长，使最后一个循环也在范围内
	// 我们会重试几次以获得良好的分布
	bestDurations := make([]time.Duration, n)
	bestDiff := 1000000 // 最小化最后一个循环与有效范围的偏差

	for attempt := 0; attempt < 100; attempt++ {
		currentSum := 0
		for i := 0; i < n-1; i++ {
			// 在 [minDur, maxDur] 范围内完全随机
			d := minDur + rand.Intn(maxDur-minDur+1)
			durations[i] = time.Duration(d) * time.Second
			currentSum += d
		}

		// 计算最后一个循环所需的时长
		// 总时间 = Sum(前 N-1) + 最后一个 + (N-1)*Rest = ActualTarget
		// 最后一个 = ActualTarget - (N-1)*Rest - Sum
		requiredLast := actualTarget - (n-1)*rest - currentSum

		durations[n-1] = time.Duration(requiredLast) * time.Second

		// 检查最后一个是否在范围内
		if requiredLast >= minDur && requiredLast <= maxDur {
			// 找到完美组合
			return durations
		}

		// 如果不完美，记录偏差
		diff := 0
		if requiredLast < minDur {
			diff = minDur - requiredLast
		} else {
			diff = requiredLast - maxDur
		}

		if diff < bestDiff {
			bestDiff = diff
			copy(bestDurations, durations)
		}
	}

	// 如果没有找到完美组合，使用最佳组合（最接近有效范围）
	return bestDurations
}

func playSound(path string) {
	// 在 Windows 上，使用 filepath.FromSlash 确保分隔符正确
	path = filepath.FromSlash(path)

	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("打开音频文件失败 %s: %v\n", path, err)
		return
	}
	defer f.Close()

	streamer, format, err := mp3.Decode(f)
	if err != nil {
		fmt.Printf("解码 mp3 失败 %s: %v\n", path, err)
		return
	}
	defer streamer.Close()

	// 如有必要进行重采样
	var s beep.Streamer = streamer
	if format.SampleRate != sampleRate {
		s = beep.Resample(4, format.SampleRate, sampleRate, streamer)
	}

	if !speakerInited {
		// 尝试初始化（应该已经在 main 中完成，但以防万一）
		speaker.Init(sampleRate, sampleRate.N(time.Second/10))
		speakerInited = true
	}

	done := make(chan bool)
	speaker.Play(beep.Seq(s, beep.Callback(func() {
		done <- true
	})))

	<-done
}

// 状态管理辅助函数
func setCurrentTask(duration time.Duration) {
	state.Lock()
	state.CurrentStartTime = time.Now()
	state.CurrentDuration = duration
	state.Unlock()
}

func setMesoTask(duration time.Duration) {
	state.Lock()
	state.MesoStartTime = time.Now()
	state.MesoDuration = duration
	state.InMeso = true
	state.Unlock()
}

func clearMesoTask() {
	state.Lock()
	state.InMeso = false
	state.Unlock()
}

func wait(duration time.Duration) {
	setCurrentTask(duration)
	time.Sleep(duration)
}
