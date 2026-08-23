package main

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"docshare/internal/config"
	"docshare/internal/store"
)

func TestListAccessLogsAggregatesAllRoots(t *testing.T) {
	base := t.TempDir()
	makeStore := func(name string) *store.Store {
		docs := filepath.Join(base, name)
		if err := os.MkdirAll(docs, 0o755); err != nil {
			t.Fatal(err)
		}
		st, err := store.New(docs, filepath.Join(base, "data-"+name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(st.Close)
		return st
	}

	first := makeStore("first")
	second := makeStore("second")
	first.RecordAccess("first/a.md", "127.0.0.1", "test")
	second.RecordAccess("second/b.md", "127.0.0.2", "test")

	app := &App{stores: []*store.Store{first, second}}
	logs := app.ListAccessLogs()
	if len(logs) != 2 {
		t.Fatalf("应聚合两个根的访问记录, got %+v", logs)
	}
	seen := map[string]bool{}
	for _, record := range logs {
		seen[record.Doc] = true
	}
	if !seen["first/a.md"] || !seen["second/b.md"] {
		t.Fatalf("聚合结果缺少根目录记录: %+v", logs)
	}
}

func TestServerInfoDoesNotExposePassword(t *testing.T) {
	app := &App{
		cfg:          config.Config{Port: 8080, Password: "desktop-secret"},
		desktopToken: "signed-token",
	}
	info := app.ServerInfo()
	if _, exists := info["password"]; exists {
		t.Fatal("ServerInfo 不应向前端暴露明文密码")
	}
	if info["passwordConfigured"] != true {
		t.Fatalf("应告知前端密码已配置: %+v", info)
	}
	if info["authToken"] != "signed-token" {
		t.Fatalf("桌面端令牌缺失: %+v", info)
	}
}

func TestSaveConfigKeepsOldServerWhenNewPortIsUnavailable(t *testing.T) {
	base := t.TempDir()
	docs := filepath.Join(base, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	oldProbe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	oldPort := oldProbe.Addr().(*net.TCPAddr).Port
	_ = oldProbe.Close()
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	newPort := occupied.Addr().(*net.TCPAddr).Port

	oldCfg := config.Config{DocsDirs: []string{docs}, DocsDir: docs, Port: oldPort, LAN: false, Password: "keep-me"}
	app := &App{cfg: oldCfg, dataDir: filepath.Join(base, "data"), cfgPath: filepath.Join(base, "data", "config.json")}
	if err := config.Save(app.cfgPath, oldCfg); err != nil {
		t.Fatal(err)
	}
	if err := app.startServer(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.stopServer)

	if _, err := app.SaveConfig([]string{docs}, newPort, false, nil, "", false); err == nil {
		t.Fatal("端口被占用时保存应失败")
	}
	if app.cfg.Port != oldPort || app.cfg.Password != "keep-me" {
		t.Fatalf("失败后内存配置被修改: %+v", app.cfg)
	}
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(oldPort) + "/api/health")
	if err != nil {
		t.Fatalf("旧服务应继续可用: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("旧服务健康检查失败: %d", resp.StatusCode)
	}
	persisted, err := config.Load(app.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Port != oldPort || persisted.Password != "keep-me" {
		t.Fatalf("失败后磁盘配置被修改: %+v", persisted)
	}
}
