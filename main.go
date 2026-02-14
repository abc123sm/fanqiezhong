package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync/atomic"
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

var (
	config        Config
	sampleRate    beep.SampleRate = 44100
	speakerInited int32           // 原子访问: 0=false, 1=true

	// 无锁状态变量 - 使用int64纳秒时间戳
	currentStartNano int64 // Unix纳秒时间戳
	currentDuration  int64 // 纳秒
	mesoStartNano    int64
	mesoDuration     int64
	inMeso           int32 // 0=false, 1=true
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
			atomic.StoreInt32(&speakerInited, 1)
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

	var durations []time.Duration
	currentTotal := 0

	// 循环生成直到总时间达到目标
	for {
		// 在 [minDur, maxDur] 范围内完全随机
		d := minDur + rand.Intn(maxDur-minDur+1)
		durations = append(durations, time.Duration(d)*time.Second)
		currentTotal += d

		// 如果当前累加时间已经 >= 目标，停止
		if currentTotal >= targetSec {
			break
		}

		// 加上休息时间用于下一次判断
		currentTotal += rest

		// 再次检查
		if currentTotal >= targetSec {
			break
		}
	}

	return durations
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

	if atomic.LoadInt32(&speakerInited) == 0 {
		// 尝试初始化（应该已经在 main 中完成，但以防万一）
		speaker.Init(sampleRate, sampleRate.N(time.Second/10))
		atomic.StoreInt32(&speakerInited, 1)
	}

	done := make(chan bool)
	speaker.Play(beep.Seq(s, beep.Callback(func() {
		done <- true
	})))

	<-done
}

// 状态管理辅助函数 - 无锁实现
func setCurrentTask(duration time.Duration) {
	atomic.StoreInt64(&currentStartNano, time.Now().UnixNano())
	atomic.StoreInt64(&currentDuration, int64(duration))
}

func setMesoTask(duration time.Duration) {
	atomic.StoreInt64(&mesoStartNano, time.Now().UnixNano())
	atomic.StoreInt64(&mesoDuration, int64(duration))
	atomic.StoreInt32(&inMeso, 1)
}

func clearMesoTask() {
	atomic.StoreInt32(&inMeso, 0)
}

func wait(duration time.Duration) {
	setCurrentTask(duration)
	time.Sleep(duration)
}
