// Package api 提供 HTTP 接口: 文档浏览、访问记录以及前端静态资源。
package api

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"docshare/internal/store"
)

// Server 聚合 HTTP 处理所需依赖。
type Server struct {
	st        *store.Store
	frontDir  string // 磁盘前端目录(可选, 调试用); 为空时使用内嵌资源
	webFS     fs.FS  // 内嵌前端资源
	blacklist []string // IP 黑名单(精确 IP 或 CIDR)
}

// New 创建 Server。
// frontDir 非空且存在时优先使用磁盘目录, 否则使用 webFS 内嵌资源。
func New(st *store.Store, frontDir string, webFS fs.FS, blacklist []string) (*Server, error) {
	s := &Server{st: st, webFS: webFS}
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

// Handler 返回完整路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/tree", s.handleTree)
	mux.HandleFunc("GET /api/doc", s.handleDoc)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("/", s.handleStatic)
	return logRequests(withCORS(s.blockIP(mux)))
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
	tree, err := s.st.Tree()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取文档目录失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ready": s.st.Ready(),
		"node":  tree,
	})
}

func (s *Server) handleDoc(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeErr(w, http.StatusBadRequest, "缺少 path 参数")
		return
	}
	content, modified, size, err := s.st.ReadDoc(path)
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
		"path":     path,
		"name":     filepath.Base(path),
		"content":  content,
		"modified": modified,
		"size":     size,
	})
	// 记录访问(异步, 不影响响应)
	go s.st.RecordAccess(path, clientIP(r), r.UserAgent())
}

func clientIP(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

// handleSearch 全文搜索。
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
	results, err := s.st.Search(q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "搜索失败: "+err.Error())
		return
	}
	if results == nil {
		results = []store.SearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
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
