package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAudioPreload_EmbeddedFallbackAndExternalOverride(t *testing.T) {
	emptyExternalDir := t.TempDir()

	// 1. 外部目录无文件时，应成功从内置 embed.FS 解码全部三个提示音
	for _, name := range []string{"Sounds/info.mp3", "Sounds/succeed.mp3", "Sounds/warning.mp3"} {
		buf, fromExt, err := loadAndDecodeSound(name, emptyExternalDir)
		if err != nil {
			t.Fatalf("解码内嵌音频 %s 失败: %v", name, err)
		}
		if fromExt {
			t.Errorf("外部目录为空时不应标记为来自外部文件 (%s)", name)
		}
		if buf == nil || buf.Len() == 0 {
			t.Fatalf("解码 %s 得到的内存缓冲为空", name)
		}
	}

	// 2. 外部目录存在合法同名音频时，应优先命中外部文件
	extDir := t.TempDir()
	extSoundsDir := filepath.Join(extDir, "Sounds")
	if err := os.MkdirAll(extSoundsDir, 0755); err != nil {
		t.Fatal(err)
	}
	origBytes, err := os.ReadFile(filepath.Join("Sounds", "warning.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extSoundsDir, "warning.mp3"), origBytes, 0644); err != nil {
		t.Fatal(err)
	}

	bufExt, fromExt, err := loadAndDecodeSound("Sounds/warning.mp3", extDir)
	if err != nil {
		t.Fatalf("加载外部音频失败: %v", err)
	}
	if !fromExt || bufExt == nil || bufExt.Len() == 0 {
		t.Fatalf("期望优先加载外部有效音频 (fromExt=%v)", fromExt)
	}

	// 3. 外部同名文件损坏时，应自动回退内置音频而不失败
	if err := os.WriteFile(filepath.Join(extSoundsDir, "info.mp3"), []byte("invalid-mp3-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	bufFallback, fromExtFallback, err := loadAndDecodeSound("Sounds/info.mp3", extDir)
	if err != nil {
		t.Fatalf("外部文件损坏时期望自动回退内置音频，但返回错误: %v", err)
	}
	if fromExtFallback || bufFallback == nil || bufFallback.Len() == 0 {
		t.Fatalf("回退内置音频结果不符合预期 (fromExt=%v)", fromExtFallback)
	}
}
