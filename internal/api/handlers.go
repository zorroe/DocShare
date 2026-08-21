// Package api 提供 HTTP 接口: 文档浏览、访问记录以及前端静态资源。
package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"docshare/internal/store"
)

// Server 聚合 HTTP 处理所需依赖。
type Server struct {
	stores     []*store.Store // 文档存储(支持多根目录)
	frontDir   string         // 磁盘前端目录(可选, 调试用); 为空时使用内嵌资源
	webFS      fs.FS          // 内嵌前端资源
	blacklist  []string       // IP 黑名单(精确 IP 或 CIDR)
	password   string         // 只读访问密码(空 = 不启用)
	authSecret []byte         // 会话令牌签名密钥

	lockMu   sync.Mutex              // 登录失败锁定
	lockFails map[string]*loginLock  // IP -> 失败计数/锁定期
	lockSec  int                     // 连续失败 N 次后锁定秒数(0 = 不锁定)
}

const (
	loginMaxFails = 5  // 连续失败次数阈值
	loginLockSec  = 30 // 默认锁定时长(秒)
)

type loginLock struct {
	count int
	until time.Time
}

// New 创建单根 Server(兼容旧签名)。
func New(st *store.Store, frontDir string, webFS fs.FS, blacklist []string, password string) (*Server, error) {
	return NewMulti([]*store.Store{st}, frontDir, webFS, blacklist, password)
}

// NewMulti 创建多根 Server: 多个文档目录聚合展示,
// 目录树中每个根作为一级节点(以目录名区分), 路径形如 "根名/相对路径"。
func NewMulti(stores []*store.Store, frontDir string, webFS fs.FS, blacklist []string, password string) (*Server, error) {
	if len(stores) == 0 {
		return nil, errors.New("至少需要一个文档存储")
	}
	s := &Server{stores: stores, webFS: webFS, password: password, lockFails: map[string]*loginLock{}, lockSec: loginLockSec}
	if s.password != "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		s.authSecret = b
	}
	if frontDir != "" {
		absFront, err := filepath.Abs(frontDir)
		if err != nil {
			return nil, err
		}
		if info, err := os.Stat(absFront); err == nil && info.IsDir() {
			s.frontDir = absFront
		}
	}
	for _, b := range blacklist {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		if strings.Contains(b, "/") {
			if _, _, err := net.ParseCIDR(b); err == nil {
				s.blacklist = append(s.blacklist, b)
			} else {
				log.Printf("[警告] 忽略无效黑名单条目: %s", b)
			}
			continue
		}
		if net.ParseIP(b) != nil {
			s.blacklist = append(s.blacklist, b)
		} else {
			log.Printf("[警告] 忽略无效黑名单条目: %s", b)
		}
	}
	return s, nil
}

// multiRoot 是否多根模式(树中显示根节点层级)。
func (s *Server) multiRoot() bool { return len(s.stores) > 1 }

// SetLockSeconds 设置登录失败锁定秒数(0 = 不锁定)。供 CLI 参数与测试使用。
func (s *Server) SetLockSeconds(sec int) {
	s.lockMu.Lock()
	s.lockSec = sec
	s.lockFails = map[string]*loginLock{}
	s.lockMu.Unlock()
}

// rootName 返回 store 的逻辑根名(目录 basename)。
func rootName(st *store.Store) string {
	name := filepath.Base(st.Root())
	if name == "." || name == string(filepath.Separator) {
		return "docs"
	}
	return name
}

// resolveStore 将请求路径路由到对应 store:
// 多根模式路径形如 "根名/相对路径", 首段匹配根名; 无匹配回落首个 store。
func (s *Server) resolveStore(path string) (*store.Store, string) {
	if len(s.stores) == 1 {
		return s.stores[0], path
	}
	idx := strings.IndexByte(path, '/')
	first := path
	rest := ""
	if idx >= 0 {
		first, rest = path[:idx], path[idx+1:]
	}
	for _, st := range s.stores {
		if rootName(st) == first {
			return st, rest
		}
	}
	return s.stores[0], path
}

// rootPrefix 为搜索结果/访问记录补充根前缀(多根模式)。
func (s *Server) rootPrefix(st *store.Store, rel string) string {
	if len(s.stores) == 1 {
		return rel
	}
	return rootName(st) + "/" + rel
}

// ---- 文档内图片等静态资源 ----

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".svg": true, ".bmp": true, ".ico": true, ".avif": true,
}

// resolveAbsolute 将绝对路径限定在文档根目录内(含符号链接校验), 否则返回空。
func (s *Server) resolveAbsolute(p string) string {
	abs := filepath.Clean(filepath.FromSlash(p))
	if !filepath.IsAbs(abs) {
		return ""
	}
	for _, st := range s.stores {
		rootR, err := filepath.EvalSymlinks(st.Root())
		if err != nil {
			rootR = st.Root()
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			continue
		}
		if resolved != rootR && !strings.HasPrefix(resolved, rootR+string(os.PathSeparator)) {
			continue
		}
		return resolved
	}
	return ""
}

// resolveDocAsset 解析文档内资源路径(允许 ../ 一级):
// 目标可位于文档根内, 或根的直接父目录内(如 ../img/x.png 引用兄弟目录图片)。
// 始终做符号链接校验, 防止逃逸出根与根父级。
func (s *Server) resolveDocAsset(st *store.Store, rel string) (string, bool) {
	base := filepath.Clean(st.Root())
	parent := filepath.Dir(base)
	candidate := filepath.Clean(filepath.Join(base, filepath.FromSlash(rel)))

	// 目标必须位于根内或根的直接父级内(../ 只允许一级)
	if candidate != base && candidate != parent &&
		!strings.HasPrefix(candidate, base+string(os.PathSeparator)) &&
		!strings.HasPrefix(candidate, parent+string(os.PathSeparator)) {
		return "", false
	}
	// 符号链接校验: 解析结果不能逃出根或根父级
	baseR, err := filepath.EvalSymlinks(base)
	if err != nil {
		baseR = base
	}
	parentR, err := filepath.EvalSymlinks(parent)
	if err != nil {
		parentR = parent
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", false
	}
	inBase := resolved == baseR || strings.HasPrefix(resolved, baseR+string(os.PathSeparator))
	inParent := resolved == parentR || strings.HasPrefix(resolved, parentR+string(os.PathSeparator))
	if !inBase && !inParent {
		return "", false
	}
	return resolved, true
}

// handleFile 提供 Markdown 文档内的图片资源。
// path 支持: 相对路径(多根时含根前缀, 允许 ../ 一级) / 文档根内的本地绝对路径。
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		writeErr(w, http.StatusBadRequest, "缺少 path 参数")
		return
	}
	// 1) 相对路径(含根前缀) → 常规安全解析
	st, rel := s.resolveStore(p)
	full, err := st.Resolve(rel)
	if err != nil {
		// 2) 本地绝对路径(必须位于某文档根内)
		full = s.resolveAbsolute(p)
		if full == "" {
			// 3) ../ 相对路径: 允许访问根的直接父级内资源(如图片目录)
			if f, ok := s.resolveDocAsset(st, rel); ok {
				full = f
			}
		}
		if full == "" {
			writeErr(w, http.StatusNotFound, "文件不存在: "+p)
			return
		}
	}
	if !imageExts[strings.ToLower(filepath.Ext(full))] {
		writeErr(w, http.StatusForbidden, "仅支持图片资源")
		return
	}
	http.ServeFile(w, r, full)
}

// Handler 返回完整路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/tree", s.handleTree)
	mux.HandleFunc("GET /api/doc", s.handleDoc)
	mux.HandleFunc("GET /api/file", s.handleFile)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/", s.handleStatic)
	return logRequests(withCORS(s.blockIP(s.requireAuth(mux))))
}

// ---- 只读访问密码 ----

// authToken 有效期 12 小时的无状态会话令牌(hmac 签名)。
const authTokenTTL = 12 * time.Hour

// validToken 校验会话令牌。
func (s *Server) validToken(tok string) bool {
	if s.password == "" || s.authSecret == nil {
		return false
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, s.authSecret)
	mac.Write(payload)
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))) != 1 {
		return false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return false
	}
	return claims.Exp > time.Now().Unix()
}

// issueToken 签发会话令牌。
func (s *Server) issueToken() string {
	payload, _ := json.Marshal(map[string]int64{"exp": time.Now().Add(authTokenTTL).Unix()})
	mac := hmac.New(sha256.New, s.authSecret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	authed := false
	if tok := r.Header.Get("Authorization"); tok != "" {
		authed = s.validToken(strings.TrimPrefix(tok, "Bearer "))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.password != "",
		"authed":  authed,
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	ip := clientIP(r)
	if secs := s.lockRemain(ip); secs > 0 {
		writeErr(w, http.StatusTooManyRequests, fmt.Sprintf("密码错误次数过多，请 %d 秒后再试", secs))
		return
	}
	if s.password == "" || subtle.ConstantTimeCompare([]byte(body.Password), []byte(s.password)) != 1 {
		s.recordFail(ip)
		writeErr(w, http.StatusUnauthorized, "密码错误")
		return
	}
	s.clearFail(ip)
	writeJSON(w, http.StatusOK, map[string]string{"token": s.issueToken()})
}

// recordFail 记录一次登录失败; 达到阈值后锁定该 IP。
func (s *Server) recordFail(ip string) {
	if s.lockSec <= 0 {
		return
	}
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	l := s.lockFails[ip]
	if l == nil {
		l = &loginLock{}
		s.lockFails[ip] = l
	}
	l.count++
	if l.count >= loginMaxFails {
		l.until = time.Now().Add(time.Duration(s.lockSec) * time.Second)
	}
}

// clearFail 登录成功后清除该 IP 的失败记录。
func (s *Server) clearFail(ip string) {
	s.lockMu.Lock()
	delete(s.lockFails, ip)
	s.lockMu.Unlock()
}

// lockRemain 返回该 IP 剩余锁定秒数(0 = 未锁定)。
func (s *Server) lockRemain(ip string) int {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	l := s.lockFails[ip]
	if l == nil || l.until.IsZero() {
		return 0 // 无记录或尚未触发锁定, 不清除计数
	}
	if l.until.After(time.Now()) {
		return int(time.Until(l.until).Seconds()) + 1
	}
	delete(s.lockFails, ip) // 锁定期已过, 自动解除
	return 0
}

// requireAuth 启用密码时拦截所有 /api/*(auth 与 health 除外)。
// 静态资源不拦截(前端负责展示登录界面)。
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.password == "" {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		if !strings.HasPrefix(p, "/api/") ||
			p == "/api/health" || strings.HasPrefix(p, "/api/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !s.validToken(strings.TrimSpace(tok)) {
			writeErr(w, http.StatusUnauthorized, "需要访问密码")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ipBlocked 判断请求来源 IP 是否命中黑名单(精确 IP 或 CIDR)。
func (s *Server) ipBlocked(r *http.Request) bool {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, rule := range s.blacklist {
		if strings.Contains(rule, "/") {
			if _, cidr, err := net.ParseCIDR(rule); err == nil && cidr.Contains(ip) {
				return true
			}
			continue
		}
		if p := net.ParseIP(rule); p != nil && p.Equal(ip) {
			return true
		}
	}
	return false
}

// blockIP 拦截黑名单 IP 的所有请求(含 CORS 预检)。
func (s *Server) blockIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.blacklist) > 0 && s.ipBlocked(r) {
			log.Printf("[黑名单] 已拦截 %s -> %s", r.RemoteAddr, r.URL.Path)
			writeErr(w, http.StatusForbidden, "您的 IP 已被列入黑名单")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withCORS 允许跨源访问(桌面端壳页面直连本地 API)。
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- 通用工具 ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// ---- 接口实现 ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "time": time.Now().Format(time.RFC3339)})
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	if !s.multiRoot() {
		st := s.stores[0]
		tree, err := st.Tree()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "读取文档目录失败: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ready": st.Ready(),
			"node":  tree,
		})
		return
	}
	// 多根: 聚合, 每个根作为一级目录节点
	root := &store.Node{Name: "文档", Path: ".", IsDir: true}
	ready := false
	for _, st := range s.stores {
		tree, err := st.Tree()
		if err != nil {
			continue
		}
		if st.Ready() {
			ready = true
		}
		name := rootName(st)
		tree.Name = name
		tree.Path = name
		// 子节点路径补根名前缀, 保证 /api/doc 能正确路由到对应根
		for _, c := range tree.Children {
			prefixTreePath(c, name)
		}
		root.Children = append(root.Children, tree)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ready": ready, "node": root})
}

// prefixTreePath 递归为节点路径添加根前缀(多根模式)。
func prefixTreePath(node *store.Node, prefix string) {
	if node.Path != "." {
		node.Path = prefix + "/" + node.Path
	}
	for _, c := range node.Children {
		prefixTreePath(c, prefix)
	}
}

func (s *Server) handleDoc(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeErr(w, http.StatusBadRequest, "缺少 path 参数")
		return
	}
	st, rel := s.resolveStore(path)
	content, modified, size, err := st.ReadDoc(rel)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "文档不存在: "+path)
			return
		}
		if errors.Is(err, store.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权访问该路径")
			return
		}
		writeErr(w, http.StatusInternalServerError, "读取文档失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":     s.rootPrefix(st, rel),
		"name":     filepath.Base(path),
		"content":  content,
		"modified": modified,
		"size":     size,
	})
	// 记录访问(异步, 不影响响应)
	go st.RecordAccess(s.rootPrefix(st, rel), clientIP(r), r.UserAgent())
}

func clientIP(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

// handleSearch 全文搜索(跨所有文档根聚合)。
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []store.SearchResult{})
		return
	}
	if len([]rune(q)) > 100 {
		writeErr(w, http.StatusBadRequest, "搜索词过长")
		return
	}
	var all []store.SearchResult
	for _, st := range s.stores {
		results, err := st.Search(q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "搜索失败: "+err.Error())
			return
		}
		for _, res := range results {
			res.Path = s.rootPrefix(st, res.Path)
			all = append(all, res)
		}
	}
	if all == nil {
		all = []store.SearchResult{}
	}
	writeJSON(w, http.StatusOK, all)
}

// handleStatic 托管前端静态资源, 未命中的路径回退到 index.html(SPA)。
// 优先磁盘目录(frontDir), 否则使用内嵌资源(webFS)。
// /api/ 前缀一律不参与 SPA 回退(未注册的接口返回 404, 而非页面)。
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	// /api/ 前缀一律不参与 SPA 回退(未注册的接口返回 404, 而非页面)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "接口不存在")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// 禁止缓存: 确保网页端始终拿到最新前端
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	upath := path.Clean("/" + r.URL.Path) // 统一为 / 开头的 URL 路径
	if upath == "/" {
		upath = "/index.html"
	}
	rel := strings.TrimPrefix(upath, "/")

	if s.frontDir != "" {
		full := filepath.Join(s.frontDir, filepath.FromSlash(rel))
		if strings.HasPrefix(full, s.frontDir+string(os.PathSeparator)) {
			if info, err := os.Stat(full); err == nil && !info.IsDir() {
				http.ServeFile(w, r, full)
				return
			}
		}
		http.ServeFile(w, r, filepath.Join(s.frontDir, "index.html"))
		return
	}
	s.serveFS(w, r, rel)
}

// serveFS 从内嵌文件系统提供静态资源, 带 SPA 回退。
func (s *Server) serveFS(w http.ResponseWriter, r *http.Request, rel string) {
	f, err := s.webFS.Open(rel)
	if err != nil {
		f, err = s.webFS.Open("index.html") // SPA 回退
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		f, err = s.webFS.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		info, err = f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	seeker, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "unsupported file", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), seeker)
}

// LanIPs 枚举本机非回环 IPv4 地址, 用于启动提示。
func LanIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			ip := ipn.IP.To4()
			if ip != nil && !ip.IsLoopback() {
				ips = append(ips, ip.String())
			}
		}
	}
	return ips
}
