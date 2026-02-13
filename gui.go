//go:build gui
// +build gui

package main

import (
	"fmt"
	"log"

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

func startGUIOrBlock() {
	log.Println("正在启动 GUI...")
	startEbitenGUI()
}

// Ebiten 游戏实现
type Game struct {
	width  int
	height int
	firstFrame bool
}

func (g *Game) Update() error {
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	state.Lock()
	defer state.Unlock()
	
	// 背景色
	screen.Fill(color.Black)
	
	// 获取实际屏幕尺寸
	w, h := screen.Bounds().Dx(), screen.Bounds().Dy()
	
	// 计算时间
	currentElapsed := time.Since(state.CurrentStartTime).Seconds()
	currentTotal := state.CurrentDuration.Seconds()
	if currentElapsed > currentTotal {
		currentElapsed = currentTotal
	}
	currentRemaining := currentTotal - currentElapsed
	if currentRemaining < 0 { currentRemaining = 0 }

	mesoElapsed := time.Since(state.MesoStartTime).Seconds()
	mesoTotal := state.MesoDuration.Seconds()
	if mesoElapsed > mesoTotal {
		mesoElapsed = mesoTotal
	}
	mesoRemaining := mesoTotal - mesoElapsed
	if mesoRemaining < 0 { mesoRemaining = 0 }

	// 布局逻辑
	padding := 10
	
	rowCount := 1
	if state.InMeso {
		rowCount = 2
	}
	
	availHeight := h - (padding * (rowCount + 1))
	barHeight := availHeight / rowCount
	
	textWidth := 50 
	
	barWidth := w - (padding * 3) - textWidth
	if barWidth < 10 { barWidth = 10 }
	
	// 绘制当前进度
	currentRatio := 0.0
	if currentTotal > 0 {
		currentRatio = currentElapsed / currentTotal
	}
	
	yPos := padding
	drawBar(screen, padding, yPos, barWidth, barHeight, currentRatio, color.RGBA{76, 175, 80, 255})
	
	timeStr := formatTime(currentRemaining)
	textY := yPos + (barHeight/2) + 8
	text.Draw(screen, timeStr, uiFont, padding+barWidth+padding, textY, color.White)

	// 如果在中循环中，绘制中循环进度
	if state.InMeso {
		mesoRatio := 0.0
		if mesoTotal > 0 {
			mesoRatio = mesoElapsed / mesoTotal
		}
		
		yPos = padding + barHeight + padding
		drawBar(screen, padding, yPos, barWidth, barHeight, mesoRatio, color.RGBA{33, 150, 243, 255}) // 蓝色
		
		mesoTimeStr := formatTime(mesoRemaining)
		textY = yPos + (barHeight/2) + 8
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
	
	if err := ebiten.RunGame(&Game{}); err != nil {
		msg := fmt.Sprintf("GUI 错误: %v", err)
		fmt.Println(msg)
		log.Println(msg)
	}
	log.Println("GUI 已退出")
}
