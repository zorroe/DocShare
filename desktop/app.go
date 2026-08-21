// App 桌面端应用: 配置管理 + HTTP 服务生命周期 + 审批直调。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"docshare/internal/api"
	"docshare/internal/autostart"
	"docshare/internal/config"
	"docshare/internal/store"
)

// App 是 Wails 绑定对象(方法名首字母大写即暴露给前端)。
type App struct {
	ctx      context.Context
	cfg      config.Config
	cfgPath  string
	dataDir  string
	st       *store.Store
	server   *http.Server
	listener net.Listener
	started  bool
	errMsg   string
}

// pickDataDir 选择数据目录: 优先 exe 目录(可写), 否则退回用户配置目录,
// 保证配置与访问记录始终可持久化(重启后恢复)。
func pickDataDir() (dataDir, cfgPath string) {
	base := config.ExeDir()
	cand := filepath.Join(base, "data")
	if writable(cand) {
		return cand, filepath.Join(cand, "config.json")
	}
	if appData, err := os.UserConfigDir(); err == nil {
		cand = filepath.Join(appData, "DocShare")
		_ = os.MkdirAll(cand, 0o755)
		return cand, filepath.Join(cand, "config.json")
	}
	return cand, filepath.Join(cand, "config.json")
}

// writable 检测目录是否可写(创建 + 探测文件)。
func writable(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	probe := filepath.Join(dir, ".write-test")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return true
}

// NewApp 加载配置并确定数据目录。
func NewApp() *App {
	dataDir, cfgPath := pickDataDir()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Printf("[警告] 读取配置失败(原文件已备份), 使用默认配置: %v", err)
		cfg = config.Default()
	}
	log.Printf("数据目录: %s", dataDir)
	return &App{cfg: cfg, cfgPath: cfgPath, dataDir: dataDir}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Wails 初始化会替换标准 logger, 这里重新接管: 日志写入 data/app.log
	// 注意: GUI 应用 stderr 句柄无效, 文件句柄必须放在 MultiWriter 首位
	if f, err := os.OpenFile(filepath.Join(a.dataDir, "app.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(io.MultiWriter(f, os.Stderr))
	}
	log.Printf("桌面端启动, 数据目录: %s", a.dataDir)
	if err := a.startServer(); err != nil {
		a.errMsg = err.Error()
		log.Printf("[错误] 服务启动失败: %v", err)
	}
}

func (a *App) shutdown(ctx context.Context) {
	a.stopServer()
}

// ---- 服务生命周期 ----

func (a *App) startServer() error {
	a.stopServer()
	dirs := a.cfg.GetDocsDirs()
	if len(dirs) == 0 {
		dirs = []string{""} // 未配置: 单个未就绪 store
	}
	var stores []*store.Store
	for i, d := range dirs {
		stDir := a.dataDir
		if len(dirs) > 1 { // 多根: 访问记录按根隔离存储
			stDir = filepath.Join(a.dataDir, "roots", strconv.Itoa(i))
		}
		st, err := store.New(d, stDir)
		if err != nil {
			return err
		}
		stores = append(stores, st)
	}
	a.st = stores[0] // 访问记录读取等沿用首个 store
	srv, err := api.NewMulti(stores, "", api.WebFS, a.cfg.Blacklist, a.cfg.Password)
	if err != nil {
		return err
	}
	host := "127.0.0.1"
	if a.cfg.LAN {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", host, a.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("端口 %d 被占用, 请在设置中更换端口", a.cfg.Port)
	}
	a.listener = ln
	a.server = &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := a.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[错误] 服务异常退出: %v", err)
		}
	}()
	a.started = true
	a.errMsg = ""
	log.Printf("服务已启动: http://localhost:%d (局域网访问: %v)", a.cfg.Port, a.cfg.LAN)
	return nil
}

func (a *App) stopServer() {
	if a.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = a.server.Shutdown(ctx)
		cancel()
		a.server = nil
	}
	if a.listener != nil {
		_ = a.listener.Close()
		a.listener = nil
	}
	a.started = false
}

// ---- 前端绑定方法 ----

// ServerInfo 返回当前服务状态。
func (a *App) ServerInfo() map[string]any {
	return map[string]any{
		"port":      a.cfg.Port,
		"docsDir":   a.cfg.DocsDir,
		"docsDirs":  a.cfg.GetDocsDirs(),
		"lan":       a.cfg.LAN,
		"running":   a.started,
		"dataDir":   a.dataDir,
		"error":     a.errMsg,
		"blacklist": a.cfg.Blacklist,
		"password":  a.cfg.Password, // 桌面壳页自动登录用
	}
}

// DirEntry 目录选择器条目。
type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ListDir 列出目录的子文件夹; path 为空时返回盘符列表。
func (a *App) ListDir(path string) ([]DirEntry, error) {
	if strings.TrimSpace(path) == "" {
		return a.listDrives()
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("无法访问目录: %s", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var out []DirEntry
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, DirEntry{Name: e.Name(), Path: filepath.Join(path, e.Name())})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (a *App) listDrives() ([]DirEntry, error) {
	var out []DirEntry
	for _, c := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		p := string(c) + ":\\"
		if _, err := os.Stat(p); err == nil {
			out = append(out, DirEntry{Name: p, Path: p})
		}
	}
	return out, nil
}

// SaveConfig 保存配置并重启服务(支持多文档目录)。
func (a *App) SaveConfig(docsDirs []string, port int, lan bool, blacklist []string, password string) (map[string]any, error) {
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("端口必须在 1-65535 之间")
	}
	var dirs []string
	for _, d := range docsDirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		info, err := os.Stat(d)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("文档目录不存在: %s", d)
		}
		dirs = append(dirs, d)
	}
	a.cfg.DocsDirs = dirs
	a.cfg.DocsDir = ""
	if len(dirs) > 0 {
		a.cfg.DocsDir = dirs[0] // 兼容字段同步
	}
	a.cfg.Port = port
	a.cfg.LAN = lan
	a.cfg.Password = strings.TrimSpace(password)
	var bl []string
	for _, b := range blacklist {
		b = strings.TrimSpace(b)
		if b != "" {
			bl = append(bl, b)
		}
	}
	a.cfg.Blacklist = bl
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		return nil, fmt.Errorf("保存配置失败: %w", err)
	}
	if err := a.startServer(); err != nil {
		a.errMsg = err.Error()
		return a.ServerInfo(), err
	}
	return a.ServerInfo(), nil
}

// ---- 开机自启动 ----

// AutoStart 查询开机自启动状态。
func (a *App) AutoStart() bool {
	ok, err := autostart.IsEnabled("DocShare")
	if err != nil {
		return false
	}
	return ok
}

// SetAutoStart 设置/取消开机自启动。
func (a *App) SetAutoStart(enabled bool) error {
	return autostart.SetEnabled("DocShare", enabled)
}

// OpenBrowser 在系统浏览器中打开局域网页面。
func (a *App) OpenBrowser() {
	if a.started {
		runtime.BrowserOpenURL(a.ctx, fmt.Sprintf("http://127.0.0.1:%d/", a.cfg.Port))
	}
}

// ---- 访问记录 ----

// ListAccessLogs 返回最近访问记录(桌面端查看)。
func (a *App) ListAccessLogs() []store.AccessRecord {
	if a.st == nil {
		return []store.AccessRecord{}
	}
	return a.st.ListAccess(200)
}

// ---- 自动更新检查 ----

const appVersion = "1.0.0"

// UpdateInfo 更新检查结果。
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
	HasUpdate bool   `json:"hasUpdate"`
}

// CheckUpdate 查询 GitHub Release 最新版本。
func (a *App) CheckUpdate() (*UpdateInfo, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/zorroe/DocShare/releases/latest")
	if err != nil {
		return nil, fmt.Errorf("无法连接更新服务器: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("更新服务返回 %d", resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		HtmlURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	info := &UpdateInfo{
		Current: appVersion,
		Latest:  strings.TrimPrefix(rel.TagName, "v"),
		URL:     rel.HtmlURL,
	}
	info.HasUpdate = compareVersions(info.Latest, appVersion) > 0
	return info, nil
}

// compareVersions 按点分段比较版本号: a>b 返回 1, 相等 0, 小于 -1。
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}
