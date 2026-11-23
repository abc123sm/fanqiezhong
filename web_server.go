//go:build web
// +build web

package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"
)

//go:embed web/*
var webFS embed.FS

func startWebServerIfNeeded(engine *TimerEngine) {
	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: newWebMux(engine),
	}

	go func() {
		<-engine.Context().Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	go func() {
		fmt.Printf("OBS 浏览器源地址: http://%s/\n", addr)
		fmt.Printf("控制面板地址:     http://%s/control\n", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Web 服务器启动失败: %v\n", err)
		}
	}()
}

// newWebMux 构造独立的 HTTP 路由处理器（剥离 web/ 前缀并挂载状态、SSE 与控制端点）
func newWebMux(engine *TimerEngine) http.Handler {
	subFS, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(fmt.Sprintf("加载内嵌 web 目录失败: %v", err))
	}

	mux := http.NewServeMux()
	fileServer := http.FileServer(http.FS(subFS))

	mux.Handle("/", fileServer)
	mux.HandleFunc("/control", func(w http.ResponseWriter, r *http.Request) {
		data, readErr := fs.ReadFile(subFS, "control.html")
		if readErr != nil {
			http.Error(w, "控制页面不存在", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(engine.Status())
	})

	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "不支持流式推送", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		subCh, unsub := engine.Subscribe()
		defer unsub()

		writeEvent := func(view StatusView) error {
			payload, encErr := json.Marshal(view)
			if encErr != nil {
				return encErr
			}
			if _, writeErr := fmt.Fprintf(w, "data: %s\n\n", payload); writeErr != nil {
				return writeErr
			}
			flusher.Flush()
			return nil
		}

		// 连接建立立即发送当前实时状态快照
		if err := writeEvent(engine.Status()); err != nil {
			return
		}

		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-engine.Context().Done():
				return
			case <-subCh:
				// 发送最新实时状态（包含精确已过时间）
				if err := writeEvent(engine.Status()); err != nil {
					return
				}
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})

	mux.HandleFunc("/api/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "仅允许 POST 请求", http.StatusMethodNotAllowed)
			return
		}
		engine.Reset()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/api/quit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "仅允许 POST 请求", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		engine.Quit()
	})

	return mux
}
