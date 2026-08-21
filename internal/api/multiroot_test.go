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
					Path string `json:"path"`
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
		if c.Path != c.Name {
			t.Fatalf("根节点 path 应为自身名: %s != %s", c.Path, c.Name)
		}
		names[c.Name] = len(c.Children)
		// 子节点路径必须带根名前缀(否则 /api/doc 无法路由)
		for _, child := range c.Children {
			want := c.Name + "/" + child.Name
			if child.Path != want {
				t.Fatalf("子节点 %q 的 path 应为 %q, got %q", child.Name, want, child.Path)
			}
		}
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

// 文档内图片资源接口测试。
func imageServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	base := t.TempDir()
	docs := filepath.Join(base, "docs")
	_ = os.MkdirAll(filepath.Join(docs, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(docs, "a.md"), []byte("# A"), 0o644)
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}
	_ = os.WriteFile(filepath.Join(docs, "pic.png"), png, 0o644)
	_ = os.WriteFile(filepath.Join(docs, "sub", "pic2.png"), png, 0o644)
	_ = os.WriteFile(filepath.Join(docs, "secret.txt"), []byte("secret"), 0o644)
	// 根外文件
	_ = os.WriteFile(filepath.Join(base, "outside.png"), png, 0o644)
	st, err := store.New(docs, filepath.Join(base, "data"))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(st, "", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(srv.Handler()), docs
}

func TestFileImage(t *testing.T) {
	ts, _ := imageServer(t)
	defer ts.Close()
	// 相对路径(单根)
	resp, err := http.Get(ts.URL + "/api/file?path=pic.png")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("相对路径图片应 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type 应为 image/png, got %s", ct)
	}
	// 子目录图片
	resp2, _ := http.Get(ts.URL + "/api/file?path=sub/pic2.png")
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("子目录图片应 200, got %d", resp2.StatusCode)
	}
}

func TestFileAbsoluteInRoot(t *testing.T) {
	ts, docs := imageServer(t)
	defer ts.Close()
	// 文档根内的本地绝对路径(Windows Markdown 常见写法)
	abs := filepath.Join(docs, "pic.png")
	resp, err := http.Get(ts.URL + "/api/file?path=" + abs)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("根内绝对路径图片应 200, got %d", resp.StatusCode)
	}
	// 根外绝对路径应拒绝
	outside := filepath.Join(filepath.Dir(docs), "outside.png")
	resp2, _ := http.Get(ts.URL + "/api/file?path=" + outside)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("根外绝对路径应 404, got %d", resp2.StatusCode)
	}
	// 不存在文件
	resp3, _ := http.Get(ts.URL + "/api/file?path=nope.png")
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Fatalf("不存在文件应 404, got %d", resp3.StatusCode)
	}
}

func TestFileNonImageRejected(t *testing.T) {
	ts, _ := imageServer(t)
	defer ts.Close()
	resp, _ := http.Get(ts.URL + "/api/file?path=secret.txt")
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("非图片文件应 403, got %d", resp.StatusCode)
	}
}

func TestFileTraversalRejected(t *testing.T) {
	ts, _ := imageServer(t)
	defer ts.Close()
	resp, _ := http.Get(ts.URL + "/api/file?path=..%2Foutside.png")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("穿越路径应被拒绝, got %d", resp.StatusCode)
	}
}
