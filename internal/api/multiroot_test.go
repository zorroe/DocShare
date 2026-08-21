package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"docshare/internal/store"
)

// 多文档根目录聚合测试。

func multiRootServer(t *testing.T) *httptest.Server {
	t.Helper()
	base := t.TempDir()
	mkStore := func(dir string, files map[string]string) *store.Store {
		full := filepath.Join(base, dir)
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		for rel, content := range files {
			p := filepath.Join(full, rel)
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		st, err := store.New(full, filepath.Join(base, "data-"+dir))
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	st1 := mkStore("root-a", map[string]string{"README.md": "# A\n\nalpha 关键词"})
	st2 := mkStore("root-b", map[string]string{"guide.md": "# B\n\nbeta 关键词"})
	srv, err := NewMulti([]*store.Store{st1, st2}, "", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(srv.Handler())
}

func TestMultiRootTree(t *testing.T) {
	ts := multiRootServer(t)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/tree")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var data struct {
		Ready bool `json:"ready"`
		Node  struct {
			Children []struct {
				Name     string `json:"name"`
				Path     string `json:"path"`
				IsDir    bool   `json:"isDir"`
				Children []struct {
					Name string `json:"name"`
				} `json:"children"`
			} `json:"children"`
		} `json:"node"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	if !data.Ready {
		t.Fatal("多根应 ready")
	}
	if len(data.Node.Children) != 2 {
		t.Fatalf("应有 2 个根节点, got %d", len(data.Node.Children))
	}
	names := map[string]int{}
	for _, c := range data.Node.Children {
		if !c.IsDir {
			t.Fatalf("根节点应为目录: %s", c.Name)
		}
		names[c.Name] = len(c.Children)
	}
	if names["root-a"] != 1 || names["root-b"] != 1 {
		t.Fatalf("根节点文件数错误: %v", names)
	}
}

func TestMultiRootDoc(t *testing.T) {
	ts := multiRootServer(t)
	defer ts.Close()
	for _, p := range []string{"root-a/README.md", "root-b/guide.md"} {
		resp, err := http.Get(ts.URL + "/api/doc?path=" + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("读取 %s 应 200, got %d", p, resp.StatusCode)
		}
	}
	// 不存在的根回落首个 store → 404
	resp, _ := http.Get(ts.URL + "/api/doc?path=root-x/nope.md")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("未知根应 404, got %d", resp.StatusCode)
	}
}

func TestMultiRootSearch(t *testing.T) {
	ts := multiRootServer(t)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/search?q=alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var results []struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Path != "root-a/README.md" {
		t.Fatalf("跨根搜索应带前缀: %+v", results)
	}
}
