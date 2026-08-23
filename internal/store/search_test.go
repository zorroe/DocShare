package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"README.md":       "# 项目说明\n\nDocShare 是一个局域网 Markdown 文档预览工具。\n支持全文搜索功能。",
		"指南/使用说明.md":      "# 使用说明\n\n欢迎使用 DocShare 文档中心。本文介绍如何搜索文档。",
		"指南/最佳实践.md":      "# 最佳实践\n\n文档目录建议按主题组织，文件名清晰简短。",
		"other/notes.txt": "这不是 Markdown 文件，不应该被索引。",
	}
	for rel, content := range files {
		p := filepath.Join(docs, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st, err := New(docs, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	return st, docs
}

func TestTokenize(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"hello World", []string{"hello", "world"}},
		{"DocShare-v2", []string{"docshare", "v2"}},
		{"使用说明", []string{"使用", "用说", "说明", "使", "用", "说", "明"}},
		{"ABC中文", []string{"abc", "中文", "中", "文"}},
	}
	for _, c := range cases {
		got := tokenize(c.in)
		if len(got) != len(c.want) {
			t.Errorf("tokenize(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("tokenize(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestSearchChinese(t *testing.T) {
	st, _ := newTestStore(t)
	results, err := st.Search("搜索")
	if err != nil {
		t.Fatal(err)
	}
	// "搜索" 命中 README.md 与 使用说明.md
	if len(results) != 2 {
		t.Fatalf("搜索「搜索」命中 %d 篇, 期望 2: %+v", len(results), results)
	}
	for _, r := range results {
		if !strings.Contains(r.Snippet, "搜索") {
			t.Errorf("snippet 未包含关键词: %q", r.Snippet)
		}
	}
}

func TestSearchEnglish(t *testing.T) {
	st, _ := newTestStore(t)
	results, err := st.Search("DocShare")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 {
		t.Fatalf("搜索 DocShare 命中 %d 篇, 期望 >=2", len(results))
	}
	// 相关度排序: README 出现 2 次应排第一
	if results[0].Name != "README.md" {
		t.Errorf("相关度排序错误: 第一是 %s, 期望 README.md", results[0].Name)
	}
}

func TestSearchNoResult(t *testing.T) {
	st, _ := newTestStore(t)
	results, err := st.Search("不存在的词xyzzy")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("期望无结果, 得到 %d 条", len(results))
	}
}

func TestSearchIgnoresNonMarkdown(t *testing.T) {
	st, _ := newTestStore(t)
	results, err := st.Search("不应该被索引")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("非 Markdown 文件不应被索引: %+v", results)
	}
}

func TestSearchIncrementalUpdate(t *testing.T) {
	st, docs := newTestStore(t)
	if _, err := st.Search("增量更新词"); err != nil {
		t.Fatal(err)
	}
	// 新增文档后再次搜索应命中
	p := filepath.Join(docs, "新增.md")
	if err := os.WriteFile(p, []byte("# 新增\n\n这里包含增量更新词。"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := st.Search("增量更新词")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("增量索引未生效: %+v", results)
	}
}

func TestSearchMultiTokenAnd(t *testing.T) {
	st, _ := newTestStore(t)
	// 多词查询需全部命中: "DocShare" 且 "最佳实践"
	results, err := st.Search("DocShare 最佳实践")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("AND 查询应无结果(两词不在同一文档): %+v", results)
	}
	results, err = st.Search("DocShare 搜索")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("AND 查询应命中 README 与 使用说明: %+v", results)
	}
}

func TestSearchEmpty(t *testing.T) {
	st, _ := newTestStore(t)
	results, err := st.Search("   ")
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Fatalf("空查询应返回 nil")
	}
}

func BenchmarkSearchWarmIndex(b *testing.B) {
	root := b.TempDir()
	dataDir := filepath.Join(b.TempDir(), "data")
	for i := 0; i < 200; i++ {
		name := filepath.Join(root, fmt.Sprintf("doc-%03d.md", i))
		content := fmt.Sprintf("# Document %d\n\nalpha beta gamma 性能测试 文档搜索", i)
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	st, err := New(root, dataDir)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(st.Close)
	if _, err := st.Search("alpha"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.Search("alpha"); err != nil {
			b.Fatal(err)
		}
	}
}
