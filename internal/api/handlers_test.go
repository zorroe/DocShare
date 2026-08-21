package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"docshare/internal/store"
)

func newTestServer(t *testing.T, password string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "a.md"), []byte("# A\n\nhello"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(docs, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(st, "", nil, nil, password)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(srv.Handler())
}

func getJSON(t *testing.T, url, token string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var data map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&data)
	return resp.StatusCode, data
}

func TestAuthDisabled(t *testing.T) {
	ts := newTestServer(t, "")
	defer ts.Close()
	code, _ := getJSON(t, ts.URL+"/api/tree", "")
	if code != http.StatusOK {
		t.Fatalf("无密码时 tree 应 200, got %d", code)
	}
	code, data := getJSON(t, ts.URL+"/api/auth/status", "")
	if code != http.StatusOK || data["enabled"] != false {
		t.Fatalf("status 应 enabled=false, got %d %v", code, data)
	}
}

func TestAuthFlow(t *testing.T) {
	ts := newTestServer(t, "secret-pass")
	defer ts.Close()

	// 1. 未登录访问被拦截
	code, _ := getJSON(t, ts.URL+"/api/tree", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("未登录 tree 应 401, got %d", code)
	}
	// 2. status: enabled + 未认证
	code, data := getJSON(t, ts.URL+"/api/auth/status", "")
	if code != http.StatusOK || data["enabled"] != true || data["authed"] != false {
		t.Fatalf("status 异常: %d %v", code, data)
	}
	// 3. 错误密码
	code, _ = postJSON(t, ts.URL+"/api/auth/login", `{"password":"wrong"}`, "")
	if code != http.StatusUnauthorized {
		t.Fatalf("错误密码应 401, got %d", code)
	}
	// 4. 正确密码 → token
	code, data = postJSON(t, ts.URL+"/api/auth/login", `{"password":"secret-pass"}`, "")
	if code != http.StatusOK {
		t.Fatalf("正确密码应 200, got %d", code)
	}
	token, _ := data["token"].(string)
	if token == "" {
		t.Fatal("未返回 token")
	}
	// 5. 带 token 访问成功
	code, _ = getJSON(t, ts.URL+"/api/tree", token)
	if code != http.StatusOK {
		t.Fatalf("带 token tree 应 200, got %d", code)
	}
	// 6. 伪造 token 被拒
	code, _ = getJSON(t, ts.URL+"/api/tree", token+"x")
	if code != http.StatusUnauthorized {
		t.Fatalf("伪造 token 应 401, got %d", code)
	}
	// 7. health 与 auth 接口不受拦截
	code, _ = getJSON(t, ts.URL+"/api/health", "")
	if code != http.StatusOK {
		t.Fatalf("health 应 200, got %d", code)
	}
}

func postJSON(t *testing.T, url, body, token string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var data map[string]any
	if resp.StatusCode == http.StatusOK {
		_ = json.NewDecoder(resp.Body).Decode(&data)
	}
	return resp.StatusCode, data
}
