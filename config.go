package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Config 保存番茄钟的配置信息
type Config struct {
	MicroBaseS    int    `json:"小循环基础时间秒"`
	MicroOffsetS  int    `json:"小循环随机偏移秒"`
	MicroRestS    int    `json:"小循环休息时间秒"`
	MesoDurationM int    `json:"中循环总时间分"`
	MesoRestM     int    `json:"中循环休息时间分"`
	MesoCount     int    `json:"中循环组数"`
	MacroRestM    int    `json:"大循环休息时间分"`
	Host          string `json:"监听地址"`
	Port          int    `json:"端口"`
}

// Validate 对配置参数进行严格边界校验，防止负数随机崩溃或零时长死循环
func (c *Config) Validate() error {
	if c.MicroBaseS <= 0 {
		return fmt.Errorf("小循环基础时间秒必须大于 0（当前: %d）", c.MicroBaseS)
	}
	if c.MicroOffsetS < 0 {
		return fmt.Errorf("小循环随机偏移秒不能为负数（当前: %d）", c.MicroOffsetS)
	}
	if c.MicroBaseS-c.MicroOffsetS <= 0 {
		return fmt.Errorf("小循环基础时间秒(%d)必须严格大于小循环随机偏移秒(%d)，以确保单次专注时长大于 0", c.MicroBaseS, c.MicroOffsetS)
	}
	if c.MicroRestS < 0 {
		return fmt.Errorf("小循环休息时间秒不能为负数（当前: %d）", c.MicroRestS)
	}
	if c.MesoDurationM <= 0 {
		return fmt.Errorf("中循环总时间分必须大于 0（当前: %d）", c.MesoDurationM)
	}
	if c.MesoRestM < 0 {
		return fmt.Errorf("中循环休息时间分不能为负数（当前: %d）", c.MesoRestM)
	}
	if c.MesoCount <= 0 {
		return fmt.Errorf("中循环组数必须大于 0（当前: %d）", c.MesoCount)
	}
	if c.MacroRestM < 0 {
		return fmt.Errorf("大循环休息时间分不能为负数（当前: %d）", c.MacroRestM)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("端口必须在 1 到 65535 之间（当前: %d）", c.Port)
	}
	if strings.TrimSpace(c.Host) == "" {
		return errors.New("监听地址不能为空")
	}
	return nil
}

// parseConfig 从输入流解析配置，填充默认值并执行合法性校验
func parseConfig(r io.Reader) (Config, error) {
	var cfg Config
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(cfg.Host) == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// loadConfig 从外部 config.json 加载并校验配置
func loadConfig() error {
	cfgPath := resolveResourcePath("config.json")
	file, err := os.Open(cfgPath)
	if err != nil {
		return err
	}
	defer file.Close()

	parsed, err := parseConfig(file)
	if err != nil {
		return err
	}
	config = parsed
	return nil
}

// resolveResourcePath 优先查找可执行文件所在目录，不存在则回退到当前工作目录
func resolveResourcePath(rel string) string {
	rel = filepath.FromSlash(rel)
	exePath, err := os.Executable()
	exeDir := ""
	if err == nil {
		exeDir = filepath.Dir(exePath)
	}
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	return resolveResourcePathWithDirs(rel, exeDir, wd)
}

func resolveResourcePathWithDirs(rel, primaryDir, fallbackDir string) string {
	rel = filepath.FromSlash(rel)
	if primaryDir != "" {
		primaryCandidate := filepath.Join(primaryDir, rel)
		if _, err := os.Stat(primaryCandidate); err == nil {
			return primaryCandidate
		}
	}
	if fallbackDir != "" {
		fallbackCandidate := filepath.Join(fallbackDir, rel)
		if _, err := os.Stat(fallbackCandidate); err == nil {
			return fallbackCandidate
		}
	}
	if primaryDir != "" {
		return filepath.Join(primaryDir, rel)
	}
	return rel
}
