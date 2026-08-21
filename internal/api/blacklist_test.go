package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"docshare/internal/store"
)

// IP 黑名单匹配测试(直接测 ipBlocked 判定逻辑)。

func blacklistServer(t *testing.T, rules []string) *Server {
	t.Helper()
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	_ = os.MkdirAll(docs, 0o755)
	_ = os.WriteFile(filepath.Join(docs, "a.md"), []byte("# A"), 0o644)
	st, err := store.New(docs, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(st, "", nil, rules, "")
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func blocked(srv *Server, remoteAddr string) bool {
	req, _ := http.NewRequest("GET", "/api/tree", nil)
	req.RemoteAddr = remoteAddr
	return srv.ipBlocked(req)
}

func TestBlacklistExactIP(t *testing.T) {
	srv := blacklistServer(t, []string{"192.168.1.66"})
	if !blocked(srv, "192.168.1.66:1234") {
		t.Fatal("精确 IP 应命中黑名单")
	}
	if blocked(srv, "192.168.1.67:1234") {
		t.Fatal("无关 IP 不应命中")
	}
}

func TestBlacklistCIDR(t *testing.T) {
	srv := blacklistServer(t, []string{"10.0.0.0/8"})
	if !blocked(srv, "10.1.2.3:8080") {
		t.Fatal("CIDR 内 IP 应命中")
	}
	if blocked(srv, "11.0.0.1:8080") {
		t.Fatal("CIDR 外 IP 不应命中")
	}
	if !blocked(srv, "10.0.0.1:1") {
		t.Fatal("边界 IP 10.0.0.1 应命中")
	}
}

func TestBlacklistInvalidRulesIgnored(t *testing.T) {
	srv := blacklistServer(t, []string{"not-an-ip", "999.999.1.1", "10.0.0.0/999", "192.168.1.66"})
	if !blocked(srv, "192.168.1.66:1") {
		t.Fatal("有效规则应生效")
	}
	if blocked(srv, "192.168.1.99:1") {
		t.Fatal("无关 IP 不应命中")
	}
}

func TestBlacklistIPv6(t *testing.T) {
	srv := blacklistServer(t, []string{"2001:db8::1", "fe80::/10"})
	if !blocked(srv, "[2001:db8::1]:8080") {
		t.Fatal("IPv6 精确匹配应命中")
	}
	if !blocked(srv, "[fe80::1234]:8080") {
		t.Fatal("IPv6 CIDR 应命中")
	}
	if blocked(srv, "[2001:db9::1]:8080") {
		t.Fatal("无关 IPv6 不应命中")
	}
}

func TestBlacklistEmpty(t *testing.T) {
	srv := blacklistServer(t, nil)
	if blocked(srv, "192.168.1.66:1") {
		t.Fatal("无黑名单时不应拦截")
	}
}
