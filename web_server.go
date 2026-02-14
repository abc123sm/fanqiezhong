//go:build web
// +build web

package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

//go:embed web/*
var webFS embed.FS

func startWebServerIfNeeded() {
	addr := fmt.Sprintf("0.0.0.0:%d", config.Port)
	go startWebServer(addr)
}

func startWebServer(addr string) {
	// 使用嵌入的文件系统
	http.Handle("/", http.FileServer(http.FS(webFS)))
	http.HandleFunc("/status", statusHandler)

	fmt.Printf("Web UI 服务器已启动: http://%s\n", addr)
	fmt.Println("你可以将此地址添加为 OBS 的浏览器源。")

	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Printf("Web 服务器启动失败: %v\n", err)
	}
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	// 无锁读取原子变量
	now := time.Now().UnixNano()

	cStart := atomic.LoadInt64(&currentStartNano)
	cDur := atomic.LoadInt64(&currentDuration)
	mStart := atomic.LoadInt64(&mesoStartNano)
	mDur := atomic.LoadInt64(&mesoDuration)
	inMesoFlag := atomic.LoadInt32(&inMeso) == 1

	// 计算时间值
	currentElapsed := float64(now-cStart) / 1e9
	cTotalSec := float64(cDur) / 1e9
	if currentElapsed > cTotalSec {
		currentElapsed = cTotalSec
	}

	mesoElapsed := float64(now-mStart) / 1e9
	mTotalSec := float64(mDur) / 1e9
	if mesoElapsed > mTotalSec {
		mesoElapsed = mTotalSec
	}

	resp := map[string]interface{}{
		"current_total":   cTotalSec,
		"current_elapsed": currentElapsed,
		"in_meso":         inMesoFlag,
		"meso_total":      mTotalSec,
		"meso_elapsed":    mesoElapsed,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
