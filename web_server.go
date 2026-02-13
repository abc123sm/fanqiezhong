//go:build web
// +build web

package main

import (
	"fmt"
	"net/http"
	"embed"
	"encoding/json"
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
	state.Lock()
	defer state.Unlock()
	
	currentElapsed := time.Since(state.CurrentStartTime).Seconds()
	if currentElapsed > state.CurrentDuration.Seconds() {
		currentElapsed = state.CurrentDuration.Seconds()
	}
	
	mesoElapsed := time.Since(state.MesoStartTime).Seconds()
	if mesoElapsed > state.MesoDuration.Seconds() {
		mesoElapsed = state.MesoDuration.Seconds()
	}
	
	resp := map[string]interface{}{
		"current_total": state.CurrentDuration.Seconds(),
		"current_elapsed": currentElapsed,
		"in_meso": state.InMeso,
		"meso_total": state.MesoDuration.Seconds(),
		"meso_elapsed": mesoElapsed,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
