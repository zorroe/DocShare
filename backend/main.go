// DocShare - MD 文档预览与协作编辑服务(单文件命令行版)
//
// 用法:
//
//	docshare -dir <文档目录> -addr <监听地址> -token <管理令牌>
//
// 默认: 文档目录 ./docs, 监听 0.0.0.0:8080, 令牌自动生成。
// 前端资源已内嵌进本程序, 分发时只需这一个 exe。
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"docshare/internal/api"
	"docshare/internal/store"
)

// resolvePath 将相对路径解析为绝对路径。
// 依次尝试: 当前工作目录 → 可执行文件目录 → 可执行文件上级目录,
// 保证从任意位置启动都能找到资源; 全部不存在时返回工作目录相对路径。
func resolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	var bases []string
	if cwd, err := os.Getwd(); err == nil {
		bases = append(bases, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		bases = append(bases, dir, filepath.Dir(dir))
	}
	for _, b := range bases {
		cand := filepath.Join(b, p)
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	abs, _ := filepath.Abs(p)
	return abs
}

func main() {
	dir := flag.String("dir", "docs", "Markdown 文档根目录(多个目录用逗号分隔, 如: docs1,docs2)")
	addr := flag.String("addr", "0.0.0.0:8080", "HTTP 监听地址(如 0.0.0.0:8080 供局域网访问)")
	dataDir := flag.String("data", "data", "数据目录(访问记录存档)")
	front := flag.String("front", "", "前端静态资源目录(可选, 默认使用内嵌资源)")
	blacklist := flag.String("blacklist", "", "IP 黑名单, 逗号分隔(精确 IP 或 CIDR, 如 192.168.1.66,10.0.0.0/8)")
	password := flag.String("password", "", "只读访问密码(留空 = 不启用)")
	flag.Parse()

	*dataDir = resolvePath(*dataDir)

	var stores []*store.Store
	for i, d := range strings.Split(*dir, ",") {
		d = strings.TrimSpace(resolvePath(d))
		if d == "" {
			continue
		}
		stDir := *dataDir
		if len(strings.Split(*dir, ",")) > 1 {
			stDir = filepath.Join(*dataDir, "roots", strconv.Itoa(i))
		}
		st, err := store.New(d, stDir)
		if err != nil {
			log.Fatalf("初始化失败: %v", err)
		}
		if !st.Ready() {
			log.Printf("[警告] 文档目录不存在, 将显示空目录树: %s", d)
		}
		stores = append(stores, st)
	}
	if len(stores) == 0 {
		log.Fatalf("未指定有效的文档目录")
	}

	var bl []string
	if *blacklist != "" {
		for _, b := range strings.Split(*blacklist, ",") {
			bl = append(bl, strings.TrimSpace(b))
		}
	}

	srv, err := api.NewMulti(stores, *front, api.WebFS, bl, *password)
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("文档根目录: %s", *dir)
	log.Printf("数据目录:   %s", *dataDir)
	log.Printf("本机访问:   http://localhost:%s", portOf(*addr))
	for _, ip := range api.LanIPs() {
		log.Printf("局域网访问: http://%s:%s", ip, portOf(*addr))
	}
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatalf("服务异常退出: %v", err)
	}
}

func portOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i+1:]
	}
	return addr
}
