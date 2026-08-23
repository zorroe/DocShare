// App 桌面端应用: 配置管理 + HTTP 服务生命周期 + 审批直调。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"docshare/internal/api"
	"docshare/internal/autostart"
	"docshare/internal/config"
	"docshare/internal/store"
)

// App 是 Wails 绑定对象(方法名首字母大写即暴露给前端)。
type App struct {
	ctx          context.Context
	cfg          config.Config
	cfgPath      string
	dataDir      string
	stores       []*store.Store
	server       *http.Server
	listener     net.Listener
	started      bool
	errMsg       string
	desktopToken string
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
	return a.switchServer(a.cfg)
}

type preparedServer struct {
	stores       []*store.Store
	server       *http.Server
	desktopToken string
	addr         string
}

func (p *preparedServer) close() {
	for _, st := range p.stores {
		st.Close()
	}
}

func (a *App) prepareServer(cfg config.Config) (*preparedServer, error) {
	dirs := cfg.GetDocsDirs()
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
			for _, opened := range stores {
				opened.Close()
			}
			return nil, err
		}
		stores = append(stores, st)
	}
	srv, err := api.NewMulti(stores, "", api.WebFS, cfg.Blacklist, cfg.Password)
	if err != nil {
		for _, opened := range stores {
			opened.Close()
		}
		return nil, err
	}
	host := "127.0.0.1"
	if cfg.LAN {
		host = "0.0.0.0"
	}
	return &preparedServer{
		stores: stores, desktopToken: srv.DesktopToken(), addr: fmt.Sprintf("%s:%d", host, cfg.Port),
		server: &http.Server{
			Handler:           srv.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
	}, nil
}

// switchServer 先准备并尽可能预绑定新服务，成功后才替换旧服务。
func (a *App) switchServer(cfg config.Config) error {
	prepared, err := a.prepareServer(cfg)
	if err != nil {
		return err
	}

	oldCfg := a.cfg
	sameAddr := a.started && oldCfg.Port == cfg.Port && oldCfg.LAN == cfg.LAN
	var ln net.Listener
	if sameAddr {
		a.stopServer()
	} else {
		ln, err = net.Listen("tcp", prepared.addr)
		if err != nil {
			prepared.close()
			return fmt.Errorf("端口 %d 被占用, 请在设置中更换端口", cfg.Port)
		}
	}
	if ln == nil {
		ln, err = net.Listen("tcp", prepared.addr)
		if err != nil {
			prepared.close()
			if rollbackErr := a.switchServer(oldCfg); rollbackErr != nil {
				return fmt.Errorf("端口 %d 启动失败，旧服务恢复也失败: %v / %v", cfg.Port, err, rollbackErr)
			}
			return fmt.Errorf("端口 %d 启动失败，已恢复旧服务: %w", cfg.Port, err)
		}
	}
	if !sameAddr {
		a.stopServer()
	}

	a.cfg = cfg
	a.stores = prepared.stores
	a.server = prepared.server
	a.listener = ln
	a.desktopToken = prepared.desktopToken
	server := prepared.server
	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[错误] 服务异常退出: %v", err)
		}
	}()
	a.started = true
	a.errMsg = ""
	log.Printf("服务已启动: http://localhost:%d (局域网访问: %v)", cfg.Port, cfg.LAN)
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
	for _, st := range a.stores {
		st.Close()
	}
	a.stores = nil
	a.desktopToken = ""
	a.started = false
}

// ---- 前端绑定方法 ----

// LanURL 当前访问地址(局域网开启时取本机 IP, 否则回退本机地址)。
func (a *App) LanURL() string {
	url := fmt.Sprintf("http://127.0.0.1:%d", a.cfg.Port)
	if a.cfg.LAN {
		if ips := api.LanIPs(); len(ips) > 0 {
			url = fmt.Sprintf("http://%s:%d", ips[0], a.cfg.Port)
		}
	}
	return url
}

// ServerInfo 返回当前服务状态。
func (a *App) ServerInfo() map[string]any {
	return map[string]any{
		"port":               a.cfg.Port,
		"docsDir":            a.cfg.DocsDir,
		"docsDirs":           a.cfg.GetDocsDirs(),
		"lan":                a.cfg.LAN,
		"lanUrl":             a.LanURL(), // 局域网访问地址(复制/托盘菜单用)
		"running":            a.started,
		"dataDir":            a.dataDir,
		"error":              a.errMsg,
		"blacklist":          a.cfg.Blacklist,
		"passwordConfigured": a.cfg.Password != "",
		"authToken":          a.desktopToken,
		"version":            appVersion, // 当前程序版本(设置面板显示)
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

// SaveConfig 事务化应用配置；运行时和磁盘保存都成功后才提交。
func (a *App) SaveConfig(docsDirs []string, port int, lan bool, blacklist []string, password string, passwordChanged bool) (map[string]any, error) {
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("端口必须在 1-65535 之间")
	}
	var dirs []string
	seenDirs := map[string]bool{}
	for _, d := range docsDirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		abs, err := filepath.Abs(d)
		if err != nil {
			return nil, fmt.Errorf("文档目录无效: %s", d)
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("文档目录不存在: %s", d)
		}
		key := strings.ToLower(filepath.Clean(abs))
		if !seenDirs[key] {
			seenDirs[key] = true
			dirs = append(dirs, abs)
		}
	}
	next := a.cfg
	next.DocsDirs = dirs
	next.DocsDir = ""
	if len(dirs) > 0 {
		next.DocsDir = dirs[0] // 兼容字段同步
	}
	next.Port = port
	next.LAN = lan
	if passwordChanged {
		next.Password = strings.TrimSpace(password)
	}
	var bl []string
	for _, b := range blacklist {
		b = strings.TrimSpace(b)
		if b != "" {
			bl = append(bl, b)
		}
	}
	next.Blacklist = bl
	old := a.cfg
	if err := a.switchServer(next); err != nil {
		a.errMsg = err.Error()
		return a.ServerInfo(), err
	}
	if err := config.Save(a.cfgPath, next); err != nil {
		rollbackErr := a.switchServer(old)
		if rollbackErr != nil {
			return a.ServerInfo(), fmt.Errorf("保存配置失败且旧服务恢复失败: %v / %v", err, rollbackErr)
		}
		return a.ServerInfo(), fmt.Errorf("保存配置失败，已恢复旧服务: %w", err)
	}
	updateTrayCopyText() // 配置变化后同步托盘菜单的访问地址
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
	if len(a.stores) == 0 {
		return []store.AccessRecord{}
	}
	var all []store.AccessRecord
	for _, st := range a.stores {
		all = append(all, st.ListAccess(200)...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Time > all[j].Time })
	if len(all) > 200 {
		all = all[:200]
	}
	return all
}

// ---- 自动更新 ----

const appVersion = "1.4.2"

// UpdateInfo 更新检查结果。
type UpdateInfo struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	URL         string `json:"url"`
	DownloadURL string `json:"downloadUrl"` // 安装包直链
	ChecksumURL string `json:"-"`           // SHA-256 校验文件直链(仅后端使用)
	Notes       string `json:"notes"`       // 更新内容(Release 说明)
	HasUpdate   bool   `json:"hasUpdate"`
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
		Body    string `json:"body"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	info := &UpdateInfo{
		Current: appVersion,
		Latest:  strings.TrimPrefix(rel.TagName, "v"),
		URL:     rel.HtmlURL,
		Notes:   rel.Body,
	}
	if notes := []rune(info.Notes); len(notes) > 3000 {
		info.Notes = string(notes[:3000]) + "\n…"
	}
	// 自动更新只接受固定命名的安装包及其配套 SHA-256 文件。
	for _, a := range rel.Assets {
		switch {
		case strings.EqualFold(a.Name, "DocShare-Setup.exe"):
			info.DownloadURL = a.BrowserDownloadURL
		case strings.EqualFold(a.Name, "DocShare-Setup.exe.sha256"):
			info.ChecksumURL = a.BrowserDownloadURL
		}
	}
	info.HasUpdate = compareVersions(info.Latest, appVersion) > 0
	return info, nil
}

const maxInstallerBytes = 200 << 20

// downloadVerifiedFile 下载到 .part，验证大小与 SHA-256 后再原子发布到目标路径。
func downloadVerifiedFile(url, dest string, maxBytes int64, expected [sha256.Size]byte) (err error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return fmt.Errorf("下载文件超过 %d MB 上限", maxBytes>>20)
	}
	part := dest + ".part"
	_ = os.Remove(part)
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
		if err != nil {
			_ = os.Remove(part)
		}
	}()
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxBytes+1))
	if copyErr != nil {
		return copyErr
	}
	if n > maxBytes {
		return fmt.Errorf("下载文件超过 %d MB 上限", maxBytes>>20)
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	var actual [sha256.Size]byte
	copy(actual[:], h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("安装包 SHA-256 校验失败")
	}
	_ = os.Remove(dest)
	return os.Rename(part, dest)
}

func fetchSmallText(url string, maxBytes int64) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("校验文件下载失败: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxBytes {
		return "", fmt.Errorf("校验文件过大")
	}
	return string(data), nil
}

func parseChecksum(content string) ([sha256.Size]byte, error) {
	var out [sha256.Size]byte
	fields := strings.Fields(content)
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return out, fmt.Errorf("SHA-256 校验文件格式错误")
	}
	decoded, err := hex.DecodeString(fields[0])
	if err != nil {
		return out, fmt.Errorf("SHA-256 校验文件格式错误: %w", err)
	}
	copy(out[:], decoded)
	return out, nil
}

// DownloadUpdate 下载新版安装包到临时目录, 返回本地路径。
func (a *App) DownloadUpdate() (string, error) {
	info, err := a.CheckUpdate()
	if err != nil {
		return "", err
	}
	if info.DownloadURL == "" {
		return "", fmt.Errorf("未找到安装包下载地址")
	}
	if info.ChecksumURL == "" {
		return "", fmt.Errorf("新版缺少 SHA-256 校验文件，已拒绝下载")
	}
	checksumText, err := fetchSmallText(info.ChecksumURL, 4<<10)
	if err != nil {
		return "", err
	}
	checksum, err := parseChecksum(checksumText)
	if err != nil {
		return "", err
	}
	if !safeVersionRE.MatchString(info.Latest) {
		return "", fmt.Errorf("更新版本号格式不安全")
	}
	dest := filepath.Join(os.TempDir(), fmt.Sprintf("DocShare-Setup-%s.exe", info.Latest))
	if err := downloadVerifiedFile(info.DownloadURL, dest, maxInstallerBytes, checksum); err != nil {
		return "", fmt.Errorf("下载安装包失败: %v", err)
	}
	return dest, nil
}

var safeVersionRE = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,3}(?:[-+][0-9A-Za-z.-]+)?$`)

// writeUpdateBat 生成延迟启动安装程序的批处理脚本。
// 通过临时 .bat 文件避免 cmd /c 内嵌引号的转义问题(路径含空格时尤其重要)。
func writeUpdateBat(batPath, installerPath string) error {
	content := "@echo off\r\n" +
		"timeout /t 3 /nobreak >nul\r\n" +
		"start \"\" \"" + installerPath + "\"\r\n" +
		"del \"%~f0\"\r\n"
	return os.WriteFile(batPath, []byte(content), 0o644)
}

func validateInstallerPath(installerPath string) (string, error) {
	clean, err := filepath.Abs(installerPath)
	if err != nil {
		return "", fmt.Errorf("安装包路径无效")
	}
	temp, _ := filepath.Abs(os.TempDir())
	base := filepath.Base(clean)
	const prefix, suffix = "DocShare-Setup-", ".exe"
	if !strings.EqualFold(filepath.Dir(clean), temp) ||
		!strings.HasPrefix(base, prefix) || !strings.EqualFold(filepath.Ext(base), suffix) {
		return "", fmt.Errorf("安装包必须是 DocShare 下载到临时目录的安装程序")
	}
	version := strings.TrimSuffix(strings.TrimPrefix(base, prefix), filepath.Ext(base))
	if !safeVersionRE.MatchString(version) {
		return "", fmt.Errorf("安装包文件名中的版本号不安全")
	}
	return clean, nil
}

// ApplyUpdate 延迟启动安装程序(等待本应用退出)并退出当前应用。
func (a *App) ApplyUpdate(installerPath string) error {
	clean, err := validateInstallerPath(installerPath)
	if err != nil {
		return err
	}
	installerPath = clean
	info, err := os.Lstat(installerPath)
	if err != nil {
		return fmt.Errorf("安装包不存在: %s", installerPath)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("安装包必须是普通文件")
	}
	// 批处理: 3 秒后启动安装程序(等本应用完全退出释放文件占用), 随后自删
	batPath := filepath.Join(os.TempDir(), "docshare-update.bat")
	if err := writeUpdateBat(batPath, installerPath); err != nil {
		return fmt.Errorf("创建更新脚本失败: %v", err)
	}
	cmd := exec.Command("cmd", "/c", batPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动安装程序失败: %v", err)
	}
	// 触发正常退出流程(OnBeforeClose 放行)
	quitting.Store(true)
	if trayInst != nil {
		trayInst.Quit()
	}
	return nil
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
