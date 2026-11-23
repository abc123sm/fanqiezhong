//go:build !gui
// +build !gui

package main

import (
	"log"
)

func startGUIOrBlock(engine *TimerEngine) {
	log.Println("运行在无窗口/终端模式（等待退出信号）")
	<-engine.Context().Done()
}
