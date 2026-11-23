//go:build web

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestEngineForWeb(ctx context.Context) *TimerEngine {
	timings := cycleTimings{
		microBase:   500 * time.Millisecond,
		microOffset: 0,
		microRest:   200 * time.Millisecond,
		mesoTarget:  1000 * time.Millisecond,
		mesoRest:    200 * time.Millisecond,
		mesoCount:   2,
		macroRest:   200 * time.Millisecond,
	}
	return newTimerEngineWithTimings(ctx, timings, nil)
}

func TestWebMux_RootServesIndexHTMLDirectly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine := newTestEngineForWeb(ctx)
	go engine.Run()

	srv := httptest.NewServer(newWebMux(engine))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("请求根路径失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("期望 200 OK，实际得到 %d", resp.StatusCode)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	// 验证不再返回包含 <a href="web/"> 的目录列表，而是直接返回番茄钟页面且默认透明背景
	if strings.Contains(body, `<a href="web/">`) {
		t.Fatal("根路径仍然返回了目录列表而非 index.html")
	}
	if !strings.Contains(body, `id="bar-current"`) || !strings.Contains(body, "transparent") {
		t.Fatalf("根路径返回的 HTML 内容不符合预期:\n%s", body)
	}
}

func TestWebMux_ControlPageServed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine := newTestEngineForWeb(ctx)
	go engine.Run()

	srv := httptest.NewServer(newWebMux(engine))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/control")
	if err != nil {
		t.Fatalf("请求 /control 失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("期望 /control 返回 200 OK，实际得到 %d", resp.StatusCode)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)
	if !strings.Contains(body, "/api/reset") || !strings.Contains(body, "/api/quit") {
		t.Fatalf("/control 页面缺少重置或退出控制接口调用")
	}
}

func TestWebMux_SSEPushOnConnectAndReset(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine := newTestEngineForWeb(ctx)
	go engine.Run()

	// 等待引擎产生初始快照
	deadline := time.Now().Add(200 * time.Millisecond)
	for engine.Status().Seq == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	srv := httptest.NewServer(newWebMux(engine))
	defer srv.Close()

	reqCtx, reqCancel := context.WithTimeout(ctx, 2*time.Second)
	defer reqCancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, srv.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("连接 /events 失败: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("期望 Content-Type 为 text/event-stream，实际为 %q", ct)
	}

	reader := bufio.NewReader(resp.Body)
	readNextData := func() StatusView {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("读取 SSE 事件流失败: %v", err)
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data:") {
				payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				var v StatusView
				if err := json.Unmarshal([]byte(payload), &v); err != nil {
					t.Fatalf("解析 SSE JSON 失败: %v (payload=%s)", err, payload)
				}
				return v
			}
		}
	}

	// 1. 刚连接应立即收到首帧状态
	firstView := readNextData()
	if firstView.Phase != PhaseFocus || firstView.Seq == 0 {
		t.Fatalf("SSE 首帧状态异常: %+v", firstView)
	}

	// 2. 调用 POST /api/reset，SSE 流应立即收到 seq 递增的新帧
	resetResp, err := http.Post(srv.URL+"/api/reset", "application/json", nil)
	if err != nil {
		t.Fatalf("调用 /api/reset 失败: %v", err)
	}
	resetResp.Body.Close()
	if resetResp.StatusCode != http.StatusOK {
		t.Fatalf("期望 /api/reset 返回 200，实际返回 %d", resetResp.StatusCode)
	}

	secondView := readNextData()
	if secondView.Seq <= firstView.Seq || secondView.Phase != PhaseFocus {
		t.Fatalf("Reset 后期望收到更大的 seq (首帧=%d)，实际收到 %+v", firstView.Seq, secondView)
	}
}

func TestWebMux_MethodCheckAndQuitAPI(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine := newTestEngineForWeb(ctx)
	go engine.Run()

	srv := httptest.NewServer(newWebMux(engine))
	defer srv.Close()

	// GET 请求 /api/quit 必须被拒绝 (405)
	getResp, err := http.Get(srv.URL + "/api/quit")
	if err != nil {
		t.Fatal(err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("期望 GET /api/quit 返回 405，实际得到 %d", getResp.StatusCode)
	}

	// POST 请求 /api/quit 应返回 200 并使引擎退出
	postResp, err := http.Post(srv.URL+"/api/quit", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusOK {
		t.Fatalf("期望 POST /api/quit 返回 200，实际得到 %d", postResp.StatusCode)
	}

	select {
	case <-engine.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("POST /api/quit 后引擎未在预期时间内安全退出")
	}
}
