package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndValidateConfig_ValidAndDefaults(t *testing.T) {
	raw := `{
		"小循环基础时间秒": 120,
		"小循环随机偏移秒": 30,
		"小循环休息时间秒": 10,
		"中循环总时间分": 25,
		"中循环休息时间分": 5,
		"中循环组数": 3,
		"大循环休息时间分": 30
	}`

	cfg, err := parseConfig(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("期望解析成功，实际报错: %v", err)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("期望默认监听地址为 127.0.0.1，实际为 %q", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Errorf("期望默认端口为 8080，实际为 %d", cfg.Port)
	}
}

func TestParseAndValidateConfig_CustomHostPort(t *testing.T) {
	raw := `{
		"小循环基础时间秒": 60,
		"小循环随机偏移秒": 0,
		"小循环休息时间秒": 0,
		"中循环总时间分": 10,
		"中循环休息时间分": 0,
		"中循环组数": 1,
		"大循环休息时间分": 0,
		"监听地址": "0.0.0.0",
		"端口": 9090
	}`

	cfg, err := parseConfig(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("期望边界合法配置解析成功，实际报错: %v", err)
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != 9090 {
		t.Errorf("期望 0.0.0.0:9090，实际为 %s:%d", cfg.Host, cfg.Port)
	}
}

func TestConfigValidate_InvalidCases(t *testing.T) {
	baseValid := Config{
		MicroBaseS:    120,
		MicroOffsetS:  30,
		MicroRestS:    10,
		MesoDurationM: 25,
		MesoRestM:     5,
		MesoCount:     3,
		MacroRestM:    30,
		Host:          "127.0.0.1",
		Port:          8080,
	}

	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"基础时间为0", func(c *Config) { c.MicroBaseS = 0 }},
		{"基础时间为负", func(c *Config) { c.MicroBaseS = -10 }},
		{"随机偏移为负", func(c *Config) { c.MicroOffsetS = -1 }},
		{"随机偏移等于基础时间导致最小值为0", func(c *Config) { c.MicroOffsetS = 120 }},
		{"随机偏移大于基础时间导致最小值为负", func(c *Config) { c.MicroOffsetS = 150 }},
		{"小循环休息为负", func(c *Config) { c.MicroRestS = -5 }},
		{"中循环总时长为0", func(c *Config) { c.MesoDurationM = 0 }},
		{"中循环休息为负", func(c *Config) { c.MesoRestM = -1 }},
		{"中循环组数为0", func(c *Config) { c.MesoCount = 0 }},
		{"大循环休息为负", func(c *Config) { c.MacroRestM = -1 }},
		{"端口越界过小", func(c *Config) { c.Port = -1 }},
		{"端口越界过大", func(c *Config) { c.Port = 70000 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseValid
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Errorf("用例 %q 期望返回校验错误，但返回了 nil", tc.name)
			}
		})
	}
}

func TestResolveResourcePath(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(targetFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// 当首选目录存在该文件时，应命中首选目录
	got := resolveResourcePathWithDirs("config.json", tmpDir, ".")
	if got != targetFile {
		t.Errorf("期望命中首选目录 %q，实际得到 %q", targetFile, got)
	}

	// 当首选目录不存在时，应回退到备用目录路径
	fallbackDir := t.TempDir()
	fallbackFile := filepath.Join(fallbackDir, "custom.json")
	if err := os.WriteFile(fallbackFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	gotFallback := resolveResourcePathWithDirs("custom.json", tmpDir, fallbackDir)
	if gotFallback != fallbackFile {
		t.Errorf("期望回退命中 %q，实际得到 %q", fallbackFile, gotFallback)
	}
}
