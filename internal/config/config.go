// Package config 提供桌面端配置的读写。
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config 桌面端持久化配置。
type Config struct {
	DocsDir   string   `json:"docsDir"`   // Markdown 文档根目录(兼容旧配置, 多目录时为首个)
	DocsDirs  []string `json:"docsDirs"`  // 多文档根目录(空 = 使用 DocsDir)
	Port      int      `json:"port"`      // HTTP 端口
	LAN       bool     `json:"lan"`       // 是否允许局域网访问
	Blacklist []string `json:"blacklist"` // IP 黑名单(精确 IP 或 CIDR)
	Password  string   `json:"password"`  // 只读访问密码(空 = 不启用)
}

// Default 返回默认配置。
func Default() Config {
	return Config{Port: 8080, LAN: true}
}

// GetDocsDirs 返回归一化后的文档目录列表(兼容旧 docsDir 字段)。
func (c Config) GetDocsDirs() []string {
	var dirs []string
	for _, d := range c.DocsDirs {
		d = trimSpace(d)
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	if len(dirs) == 0 && trimSpace(c.DocsDir) != "" {
		dirs = []string{trimSpace(c.DocsDir)}
	}
	return dirs
}

// Load 读取配置文件; 不存在时返回默认配置。
// 配置损坏时备份为 config.json.bak 后返回默认配置。
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	// 容忍 UTF-8 BOM
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if err := json.Unmarshal(data, &cfg); err != nil {
		_ = os.Rename(path, path+".bak") // 备份损坏文件, 防止覆盖丢失
		return cfg, err
	}
	return cfg, nil
}

// Save 写入配置文件。
func Save(path string, cfg Config) error {
	if cfg.Port <= 0 || cfg.Port > 65535 {
		cfg.Port = 8080
	}
	// 同步兼容字段: 首个目录写入 docsDir
	dirs := cfg.GetDocsDirs()
	if len(dirs) > 0 {
		cfg.DocsDir = dirs[0]
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// trimSpace 去空白(局部辅助)。
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

// ExeDir 返回可执行文件所在目录。
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}
