package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var config Config

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("严重崩溃: %v\n", r)
		}
	}()

	if err := loadConfig(); err != nil {
		fmt.Printf("加载或校验配置文件失败: %v\n", err)
		time.Sleep(5 * time.Second)
		return
	}

	fmt.Println("番茄钟已启动")
	fmt.Printf("配置: %+v\n", config)

	rootCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	audioSys := NewAudioSystem()
	audioSys.InitAsync()

	engine := NewTimerEngine(rootCtx, config, audioSys.Play)

	startWebServerIfNeeded(engine)

	go engine.Run()

	startGUIOrBlock(engine)

	<-engine.Done()
	fmt.Println("番茄钟已安全退出")
}
