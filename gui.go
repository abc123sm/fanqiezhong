//go:build gui
// +build gui

package main

import (
	"fmt"
	"log"
	"sync/atomic"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"image/color"
	"time"
)

var (
	uiFont font.Face
)

// 缓存的计算结果 - 避免每帧重复计算
type cachedValues struct {
	currentElapsed   float64
	currentRemaining float64
	mesoElapsed      float64
	mesoRemaining    float64
	inMeso           bool
	width            int
	height           int
}

var currentCache cachedValues

func startGUIOrBlock() {
	log.Println("正在启动 GUI...")
	startEbitenGUI()
}

// Ebiten 游戏实现
type Game struct {
	width      int
	height     int
	firstFrame bool
}

func (g *Game) Update() error {
	// 每秒更新一次缓存值
	now := time.Now().UnixNano()

	// 读取原子变量
	cStart := atomic.LoadInt64(&currentStartNano)
	cDur := atomic.LoadInt64(&currentDuration)
	mStart := atomic.LoadInt64(&mesoStartNano)
	mDur := atomic.LoadInt64(&mesoDuration)
	inMesoFlag := atomic.LoadInt32(&inMeso) == 1

	// 计算当前进度
	currentElapsed := float64(now-cStart) / 1e9
	currentTotal := float64(cDur) / 1e9
	if currentElapsed > currentTotal {
		currentElapsed = currentTotal
	}
	currentRemaining := currentTotal - currentElapsed
	if currentRemaining < 0 {
		currentRemaining = 0
	}

	// 计算中循环进度
	mesoElapsed := float64(now-mStart) / 1e9
	mesoTotal := float64(mDur) / 1e9
	if mesoElapsed > mesoTotal {
		mesoElapsed = mesoTotal
	}
	mesoRemaining := mesoTotal - mesoElapsed
	if mesoRemaining < 0 {
		mesoRemaining = 0
	}

	// 更新缓存
	currentCache = cachedValues{
		currentElapsed:   currentElapsed,
		currentRemaining: currentRemaining,
		mesoElapsed:      mesoElapsed,
		mesoRemaining:    mesoRemaining,
		inMeso:           inMesoFlag,
		width:            g.width,
		height:           g.height,
	}

	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	// 背景色
	screen.Fill(color.Black)

	// 获取实际屏幕尺寸
	w, h := screen.Bounds().Dx(), screen.Bounds().Dy()

	// 使用缓存值（无需锁）
	cache := currentCache

	// 布局逻辑
	padding := 10

	rowCount := 1
	if cache.inMeso {
		rowCount = 2
	}

	availHeight := h - (padding * (rowCount + 1))
	barHeight := availHeight / rowCount

	textWidth := 50

	barWidth := w - (padding * 3) - textWidth
	if barWidth < 10 {
		barWidth = 10
	}

	// 绘制当前进度
	currentRatio := 0.0
	cTotal := cache.currentElapsed + cache.currentRemaining
	if cTotal > 0 {
		currentRatio = cache.currentElapsed / cTotal
	}

	yPos := padding
	drawBar(screen, padding, yPos, barWidth, barHeight, currentRatio, color.RGBA{76, 175, 80, 255})

	timeStr := formatTime(cache.currentRemaining)
	textY := yPos + (barHeight / 2) + 8
	text.Draw(screen, timeStr, uiFont, padding+barWidth+padding, textY, color.White)

	// 如果在中循环中，绘制中循环进度
	if cache.inMeso {
		mesoRatio := 0.0
		mTotal := cache.mesoElapsed + cache.mesoRemaining
		if mTotal > 0 {
			mesoRatio = cache.mesoElapsed / mTotal
		}

		yPos = padding + barHeight + padding
		drawBar(screen, padding, yPos, barWidth, barHeight, mesoRatio, color.RGBA{33, 150, 243, 255}) // 蓝色

		mesoTimeStr := formatTime(cache.mesoRemaining)
		textY = yPos + (barHeight / 2) + 8
		text.Draw(screen, mesoTimeStr, uiFont, padding+barWidth+padding, textY, color.White)
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	g.width = outsideWidth
	g.height = outsideHeight
	return outsideWidth, outsideHeight
}

var pixelImage *ebiten.Image

func init() {
	pixelImage = ebiten.NewImage(1, 1)
	pixelImage.Fill(color.White)
}

func drawBar(screen *ebiten.Image, x, y, width, height int, ratio float64, c color.Color) {
	bgOpts := &ebiten.DrawImageOptions{}
	bgOpts.GeoM.Scale(float64(width), float64(height))
	bgOpts.GeoM.Translate(float64(x), float64(y))
	bgOpts.ColorM.Scale(0.2, 0.2, 0.2, 1) // 深灰色背景
	screen.DrawImage(pixelImage, bgOpts)

	fgWidth := float64(width) * ratio
	if fgWidth > 0 {
		fgOpts := &ebiten.DrawImageOptions{}
		fgOpts.GeoM.Scale(fgWidth, float64(height))
		fgOpts.GeoM.Translate(float64(x), float64(y))
		r, g, b, a := c.RGBA()
		fgOpts.ColorM.Scale(float64(r)/65535, float64(g)/65535, float64(b)/65535, float64(a)/65535)
		screen.DrawImage(pixelImage, fgOpts)
	}
}

func formatTime(seconds float64) string {
	sec := int(seconds)
	m := sec / 60
	s := sec % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}

func startEbitenGUI() {
	tt, err := opentype.Parse(goregular.TTF)
	if err != nil {
		msg := fmt.Sprintf("字体错误: %v", err)
		fmt.Println(msg)
		log.Println(msg)
		return
	}
	const dpi = 72
	uiFont, err = opentype.NewFace(tt, &opentype.FaceOptions{
		Size:    20,
		DPI:     dpi,
		Hinting: font.HintingFull,
	})
	if err != nil {
		msg := fmt.Sprintf("创建字体失败: %v", err)
		fmt.Println(msg)
		log.Println(msg)
		return
	}

	ebiten.SetWindowSize(200, 80)
	ebiten.SetWindowTitle("番茄钟状态")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetTPS(1) // 设置每秒更新1帧 - 大幅降低CPU占用

	if err := ebiten.RunGame(&Game{}); err != nil {
		msg := fmt.Sprintf("GUI 错误: %v", err)
		fmt.Println(msg)
		log.Println(msg)
	}
	log.Println("GUI 已退出")
}
