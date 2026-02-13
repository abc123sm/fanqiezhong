//go:build !gui
// +build !gui

package main

import (
	"log"
)

func startGUIOrBlock() {
	log.Println("运行在终端模式（阻塞中）")
	select {}
}
