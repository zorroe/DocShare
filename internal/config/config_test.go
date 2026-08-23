package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 配置持久化测试: 读写与损坏恢复。

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Config{DocsDir: "D:/docs", Port: 9090, LAN: false, Blacklist: []string{"1.2.3.4"}, Password: "pw"}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.DocsDir != cfg.DocsDir || got.Port != cfg.Port || got.LAN != cfg.LAN {
		t.Fatalf("加载配置不一致: %+v vs %+v", got, cfg)
	}
	if len(got.Blacklist) != 1 || got.Blacklist[0] != "1.2.3.4" {
		t.Fatalf("黑名单加载错误: %v", got.Blacklist)
	}
	if got.Password != "pw" {
		t.Fatalf("密码加载错误: %q", got.Password)
	}
}

func TestSaveProtectsPasswordAtRest(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI 仅适用于 Windows 桌面端")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.Password = "super-secret-password"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), cfg.Password) {
		t.Fatal("配置文件不应包含明文密码")
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Password != cfg.Password {
		t.Fatalf("解密后的密码不一致: %q", loaded.Password)
	}
}

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 || !cfg.LAN {
		t.Fatalf("缺失配置应返回默认: %+v", cfg)
	}
}

func TestLoadCorruptedBacksUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{invalid json!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err == nil {
		t.Fatal("损坏配置应返回错误")
	}
	if cfg.Port != 8080 {
		t.Fatalf("损坏配置应回退默认, got %+v", cfg)
	}
	// 原文件应被备份
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("损坏配置应备份为 .bak: %v", err)
	}
}

func TestSaveInvalidPortFallsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Config{Port: 99999}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, _ := Load(path)
	if got.Port != 8080 {
		t.Fatalf("非法端口应回退 8080, got %d", got.Port)
	}
}

func TestLoadWithBOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// 带 UTF-8 BOM 的配置(Windows 编辑器常见)
	data := append([]byte("\xef\xbb\xbf"), []byte(`{"docsDir":"C:\\md","port":8088,"lan":true}`)...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DocsDir != "C:\\md" || cfg.Port != 8088 {
		t.Fatalf("BOM 配置解析错误: %+v", cfg)
	}
}
