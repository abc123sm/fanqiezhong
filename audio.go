package main

import (
	"embed"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
)

//go:embed Sounds/*.mp3
var embeddedSoundsFS embed.FS

const targetSampleRate beep.SampleRate = 44100

var defaultSoundNames = []string{
	"Sounds/info.mp3",
	"Sounds/succeed.mp3",
	"Sounds/warning.mp3",
}

// AudioSystem 管理扬声器单次初始化与内存预解码音频缓冲池
type AudioSystem struct {
	initOnce      sync.Once
	speakerReady  atomic.Bool
	mu            sync.RWMutex
	buffers       map[string]*beep.Buffer
}

func NewAudioSystem() *AudioSystem {
	return &AudioSystem{
		buffers: make(map[string]*beep.Buffer, len(defaultSoundNames)),
	}
}

// InitAsync 在后台协程安全初始化扬声器并将提示音预解码至内存，避免阻塞启动与计时
func (a *AudioSystem) InitAsync() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("音频初始化崩溃: %v\n", r)
			}
		}()

		a.ensureSpeaker()

		for _, name := range defaultSoundNames {
			buf, fromExt, err := loadAndDecodeSound(name, "")
			if err != nil {
				log.Printf("预加载音频失败 %s: %v\n", name, err)
				continue
			}
			a.mu.Lock()
			a.buffers[filepath.ToSlash(name)] = buf
			a.mu.Unlock()
			if fromExt {
				log.Printf("已加载外部自定义音效: %s\n", name)
			}
		}
		log.Println("音频缓冲预加载完成")
	}()
}

func (a *AudioSystem) ensureSpeaker() {
	a.initOnce.Do(func() {
		err := speaker.Init(targetSampleRate, targetSampleRate.N(time.Second/10))
		if err != nil {
			fmt.Printf("音频设备初始化警告: %v\n", err)
			return
		}
		a.speakerReady.Store(true)
	})
}

// Play 从内存缓冲异步非阻塞播放指定音效（零磁盘 I/O，不阻塞计时循环）
func (a *AudioSystem) Play(name string) {
	a.ensureSpeaker()
	if !a.speakerReady.Load() {
		return
	}

	key := filepath.ToSlash(name)
	a.mu.RLock()
	buf, ok := a.buffers[key]
	a.mu.RUnlock()

	if !ok {
		loaded, _, err := loadAndDecodeSound(key, "")
		if err != nil {
			log.Printf("播放时加载音频失败 %s: %v\n", key, err)
			return
		}
		a.mu.Lock()
		a.buffers[key] = loaded
		a.mu.Unlock()
		buf = loaded
	}

	streamer := buf.Streamer(0, buf.Len())
	speaker.Play(streamer)
}

// loadAndDecodeSound 优先从外部目录加载并解码 MP3，若不存在或损坏则自动回退到内置 embed.FS
func loadAndDecodeSound(relPath string, externalBaseDir string) (*beep.Buffer, bool, error) {
	slashPath := filepath.ToSlash(relPath)

	var extCandidate string
	if externalBaseDir != "" {
		extCandidate = filepath.Join(externalBaseDir, filepath.FromSlash(slashPath))
	} else {
		extCandidate = resolveResourcePath(slashPath)
	}

	if f, err := os.Open(extCandidate); err == nil {
		buf, decErr := decodeMP3ReaderToBuffer(f)
		_ = f.Close()
		if decErr == nil {
			return buf, true, nil
		}
		log.Printf("外部音频 %s 解码失败 (%v)，回退使用内置音效\n", extCandidate, decErr)
	}

	ef, err := embeddedSoundsFS.Open(slashPath)
	if err != nil {
		return nil, false, fmt.Errorf("打开内置音频 %s 失败: %w", slashPath, err)
	}
	defer ef.Close()

	buf, err := decodeMP3ReaderToBuffer(ef)
	if err != nil {
		return nil, false, fmt.Errorf("解码内置音频 %s 失败: %w", slashPath, err)
	}
	return buf, false, nil
}

func decodeMP3ReaderToBuffer(rc io.ReadCloser) (*beep.Buffer, error) {
	streamer, format, err := mp3.Decode(rc)
	if err != nil {
		return nil, err
	}
	defer streamer.Close()

	targetFormat := beep.Format{
		SampleRate:  targetSampleRate,
		NumChannels: format.NumChannels,
		Precision:   format.Precision,
	}

	var s beep.Streamer = streamer
	if format.SampleRate != targetSampleRate {
		s = beep.Resample(4, format.SampleRate, targetSampleRate, streamer)
	}

	buf := beep.NewBuffer(targetFormat)
	buf.Append(s)
	return buf, nil
}
