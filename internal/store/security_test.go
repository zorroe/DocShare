package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// 安全测试: 路径穿越防护(核心安全逻辑, 防回归)。

func newSecurityStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "a.md"), []byte("# A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(docs, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "sub", "b.md"), []byte("# B"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 根目录外的文件(穿越目标)
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.md"), []byte("# secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := New(docs, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	return st, dir
}

func TestResolveNormal(t *testing.T) {
	st, _ := newSecurityStore(t)
	full, err := st.Resolve("sub/b.md")
	if err != nil {
		t.Fatalf("正常路径应可解析: %v", err)
	}
	if filepath.Base(full) != "b.md" {
		t.Fatalf("解析结果错误: %s", full)
	}
}

func TestResolveTraversal(t *testing.T) {
	st, _ := newSecurityStore(t)
	cases := []string{
		"../secret.md",
		"../../secret.md",
		"..",
		"../",
		"a/../../secret.md",
		"sub/../../secret.md",
		`..\..\secret.md`,
		"C:/Windows/system32/drivers/etc/hosts",
	}
	for _, c := range cases {
		if _, err := st.Resolve(c); !errors.Is(err, ErrForbidden) {
			t.Errorf("穿越路径 %q 应被拒绝(ErrForbidden), got %v", c, err)
		}
	}
}

func TestResolveNotFound(t *testing.T) {
	st, _ := newSecurityStore(t)
	for _, c := range []string{"", ".", "nope.md", "sub/nope.md"} {
		if _, err := st.Resolve(c); !errors.Is(err, ErrNotFound) {
			t.Errorf("路径 %q 应返回 ErrNotFound, got %v", c, err)
		}
	}
}

func TestResolveAbsolute(t *testing.T) {
	st, root := newSecurityStore(t)
	abs := filepath.Join(root, "secret.md")
	if _, err := st.Resolve(abs); !errors.Is(err, ErrForbidden) {
		t.Errorf("绝对路径应被拒绝, got %v", err)
	}
}

func TestReadDocNonMarkdown(t *testing.T) {
	st, root := newSecurityStore(t)
	// 根目录内的非 md 文件(在 docs 内放一个 txt)
	txt := filepath.Join(st.root, "note.txt")
	if err := os.WriteFile(txt, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.ReadDoc("note.txt"); !errors.Is(err, ErrForbidden) {
		t.Errorf("非 md 文件应拒绝, got %v", err)
	}
	_ = root
}

func TestResolveBackslashTraversal(t *testing.T) {
	st, _ := newSecurityStore(t)
	// Windows 风格反斜杠穿越
	if _, err := st.Resolve(`..\..\secret.md`); !errors.Is(err, ErrForbidden) {
		t.Errorf("反斜杠穿越应被拒绝, got %v", err)
	}
}

func TestReadDocOutsideRoot(t *testing.T) {
	st, root := newSecurityStore(t)
	outside := filepath.Join(root, "secret.md")
	if _, _, _, err := st.ReadDoc(outside); !errors.Is(err, ErrForbidden) {
		t.Errorf("读取根目录外文档应拒绝, got %v", err)
	}
}
