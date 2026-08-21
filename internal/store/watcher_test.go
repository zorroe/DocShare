package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 目录变更监听测试: 树缓存随磁盘变更自动失效重建。
// Windows 上由 ReadDirectoryChangesW 驱动; 其他平台始终全量扫描, 同样通过。

func newWatchStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "a.md"), []byte("# A"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := New(docs, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		st.watchMu.Lock()
		if st.watcher != nil {
			st.watcher.stop()
		}
		st.watchMu.Unlock()
	})
	return st, docs
}

// treeHasPath 轮询直到树中出现/消失指定路径(监听事件异步送达)。
func treeHasPath(t *testing.T, st *Store, path string, want bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		root, err := st.Tree()
		if err != nil {
			t.Fatal(err)
		}
		found := findInTree(root, path)
		if found == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("等待超时: 树中路径 %q want=%v", path, want)
}

func findInTree(n *Node, path string) bool {
	if n.Path == path {
		return true
	}
	for _, c := range n.Children {
		if findInTree(c, path) {
			return true
		}
	}
	return false
}

func TestTreeWatcherAddRemove(t *testing.T) {
	st, docs := newWatchStore(t)
	treeHasPath(t, st, "a.md", true)

	// 新增文件 → 树缓存失效并包含新文件
	if err := os.WriteFile(filepath.Join(docs, "b.md"), []byte("# B"), 0o644); err != nil {
		t.Fatal(err)
	}
	treeHasPath(t, st, "b.md", true)

	// 删除文件 → 树缓存失效并移除
	if err := os.Remove(filepath.Join(docs, "a.md")); err != nil {
		t.Fatal(err)
	}
	treeHasPath(t, st, "a.md", false)
}

func TestTreeWatcherNestedDirs(t *testing.T) {
	st, docs := newWatchStore(t)

	// 新建子目录 + 子目录内新增文件(监听应动态扩展到新目录)
	sub := filepath.Join(docs, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "c.md"), []byte("# C"), 0o644); err != nil {
		t.Fatal(err)
	}
	treeHasPath(t, st, "sub/c.md", true)

	// 子目录内修改内容(树缓存失效)
	if err := os.WriteFile(filepath.Join(sub, "c.md"), []byte("# C2 longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		root, err := st.Tree()
		if err != nil {
			t.Fatal(err)
		}
		if node := findNode(root, "sub/c.md"); node != nil && node.Size > 3 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("等待超时: 子目录文件内容变更未反映到树缓存")

	// 删除子目录 → 树中消失
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	treeHasPath(t, st, "sub/c.md", false)
}

func findNode(n *Node, path string) *Node {
	if n.Path == path {
		return n
	}
	for _, c := range n.Children {
		if found := findNode(c, path); found != nil {
			return found
		}
	}
	return nil
}

func TestTreeCacheIsolated(t *testing.T) {
	st, _ := newWatchStore(t)
	treeHasPath(t, st, "a.md", true)

	// 监听生效时应返回缓存副本; 改写返回节点不得污染缓存
	root, err := st.Tree()
	if err != nil {
		t.Fatal(err)
	}
	root.Path = "hacked"
	root.Children[0].Path = "hacked-child"
	again, err := st.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if again.Path == "hacked" || again.Children[0].Path == "hacked-child" {
		t.Fatal("缓存被调用方改写污染")
	}
}

func TestSetRootRestartsWatcher(t *testing.T) {
	st, _ := newWatchStore(t)
	treeHasPath(t, st, "a.md", true)

	dir := t.TempDir()
	newDocs := filepath.Join(dir, "other")
	if err := os.MkdirAll(newDocs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDocs, "x.md"), []byte("# X"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRoot(newDocs); err != nil {
		t.Fatal(err)
	}
	treeHasPath(t, st, "x.md", true)
	treeHasPath(t, st, "a.md", false)
}
