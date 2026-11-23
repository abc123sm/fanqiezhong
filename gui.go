//go:build gui
// +build gui

package main

import (
	"bytes"
	"fmt"
	"image/color"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/gofont/gomono"
)

func startGUIOrBlock(engine *TimerEngine) {
	log.Println("正在启动 GUI...")
	startEbitenGUI(engine)
	// 窗口关闭后通知全局引擎优雅退出
	engine.Quit()
}

// Game 实现 Ebiten 窗口渲染（支持 HiDPI 缩放与无字化阶段色彩区分）
type Game struct {
	engine      *TimerEngine
	fontSource  *text.GoTextFaceSource
	view        StatusView
	deviceScale float64
	rKeyHeld    bool
}

func (g *Game) Update() error {
	select {
	case <-g.engine.Context().Done():
		return ebiten.Termination
	default:
	}

	// 支持窗口聚焦时按 R 键从 0 重新开始（边沿触发防连发）
	if ebiten.IsKeyPressed(ebiten.KeyR) {
		if !g.rKeyHeld {
			g.rKeyHeld = true
			g.engine.Reset()
		}
	} else {
		g.rKeyHeld = false
	}

	g.view = g.engine.Status()
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(color.Black)

	bounds := screen.Bounds()
	w := float32(bounds.Dx())
	h := float32(bounds.Dy())

	scale := g.deviceScale
	if scale <= 0 {
		scale = 1.0
	}
	scale32 := float32(scale)

	padding := 10.0 * scale32
	rowCount := float32(1)
	if g.view.InMeso {
		rowCount = 2
	}

	availHeight := h - padding*(rowCount+1)
	if availHeight < 8*scale32 {
		availHeight = 8 * scale32
	}
	barHeight := availHeight / rowCount

	fontSize := 18.0 * scale
	face := &text.GoTextFace{
		Source: g.fontSource,
		Size:   fontSize,
	}

	// 测量 "00:00" 等宽文本宽度，动态适配任意窗口缩放比例
	sampleTextW, _ := text.Measure("00:00", face, 0)
	textAreaW := float32(sampleTextW) + 4.0*scale32

	barWidth := w - (padding * 3) - textAreaW
	if barWidth < 10*scale32 {
		barWidth = 10 * scale32
	}

	// 第一行：当前阶段进度条与倒计时（按阶段着色，无多余文字）
	curRatio := float32(0)
	if g.view.CurrentTotal > 0 {
		curRatio = float32(g.view.CurrentElapsed / g.view.CurrentTotal)
	}
	curRemaining := g.view.CurrentTotal - g.view.CurrentElapsed
	if curRemaining < 0 {
		curRemaining = 0
	}

	yPos := padding
	drawBar(screen, padding, yPos, barWidth, barHeight, curRatio, phaseColor(g.view.Phase))
	drawTimeRightAligned(screen, formatTime(curRemaining), face, float64(w-padding), float64(yPos+barHeight/2))

	// 第二行：中循环整体进度条与倒计时（仅在中循环内展示）
	if g.view.InMeso {
		mesoRatio := float32(0)
		if g.view.MesoTotal > 0 {
			mesoRatio = float32(g.view.MesoElapsed / g.view.MesoTotal)
		}
		mesoRemaining := g.view.MesoTotal - g.view.MesoElapsed
		if mesoRemaining < 0 {
			mesoRemaining = 0
		}

		yPos = padding + barHeight + padding
		drawBar(screen, padding, yPos, barWidth, barHeight, mesoRatio, color.RGBA{33, 150, 243, 255})
		drawTimeRightAligned(screen, formatTime(mesoRemaining), face, float64(w-padding), float64(yPos+barHeight/2))
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return outsideWidth, outsideHeight
}

// LayoutF 结合显示器真实 DPI 缩放因子返回物理像素分辨率，解决 Windows 高分屏文字与边缘发虚问题
func (g *Game) LayoutF(outsideWidth, outsideHeight float64) (float64, float64) {
	scale := 1.0
	if m := ebiten.Monitor(); m != nil {
		if s := m.DeviceScaleFactor(); s > 0 {
			scale = s
		}
	}
	g.deviceScale = scale
	return outsideWidth * scale, outsideHeight * scale
}

func phaseColor(p Phase) color.RGBA {
	switch p {
	case PhaseMicroRest:
		return color.RGBA{255, 179, 0, 255} // 琥珀黄 #FFB300
	case PhaseMesoRest:
		return color.RGBA{38, 198, 218, 255} // 清爽青 #26C6DA
	case PhaseMacroRest:
		return color.RGBA{171, 71, 188, 255} // 紫罗兰 #AB47BC
	default:
		return color.RGBA{76, 175, 80, 255} // 专注绿 #4CAF50
	}
}

func drawBar(screen *ebiten.Image, x, y, width, height, ratio float32, c color.RGBA) {
	if ratio < 0 {
		ratio = 0
	} else if ratio > 1 {
		ratio = 1
	}

	vector.FillRect(screen, x, y, width, height, color.RGBA{51, 51, 51, 255}, false)

	fgWidth := width * ratio
	if fgWidth > 0 {
		vector.FillRect(screen, x, y, fgWidth, height, c, false)
	}
}

func drawTimeRightAligned(screen *ebiten.Image, str string, face text.Face, rightX, centerY float64) {
	opts := &text.DrawOptions{}
	opts.GeoM.Translate(rightX, centerY)
	opts.PrimaryAlign = text.AlignEnd
	opts.SecondaryAlign = text.AlignCenter
	opts.ColorScale.ScaleWithColor(color.White)
	text.Draw(screen, str, face, opts)
}

func formatTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	sec := int(seconds)
	m := sec / 60
	s := sec % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}

func startEbitenGUI(engine *TimerEngine) {
	source, err := text.NewGoTextFaceSource(bytes.NewReader(gomono.TTF))
	if err != nil {
		msg := fmt.Sprintf("加载内置等宽字体失败: %v", err)
		fmt.Println(msg)
		log.Println(msg)
		return
	}

	ebiten.SetWindowSize(220, 80)
	ebiten.SetWindowTitle("番茄钟状态")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetTPS(1) // 保持每秒 1 次逻辑更新，维持极低 CPU/GPU 占用

	game := &Game{
		engine:      engine,
		fontSource:  source,
		deviceScale: 1.0,
	}

	if err := ebiten.RunGame(game); err != nil {
		msg := fmt.Sprintf("GUI 错误: %v", err)
		fmt.Println(msg)
		log.Println(msg)
	}
	log.Println("GUI 已退出")
}
