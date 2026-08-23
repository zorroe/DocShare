package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func BenchmarkRecordAccess(b *testing.B) {
	st, err := New(b.TempDir(), filepath.Join(b.TempDir(), "data"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(st.Close)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st.RecordAccess(fmt.Sprintf("doc-%d.md", i), "127.0.0.1", "benchmark")
	}
}

func TestListAccessReturnsCopy(t *testing.T) {
	st, err := New(t.TempDir(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	st.RecordAccess("a.md", "127.0.0.1", "test")
	first := st.ListAccess(10)
	first[0].Doc = "mutated.md"
	second := st.ListAccess(10)
	if second[0].Doc != "a.md" {
		t.Fatalf("调用方修改了 Store 内部缓存: %+v", second)
	}
}

func TestCloseFlushesAccessRecords(t *testing.T) {
	root, dataDir := t.TempDir(), filepath.Join(t.TempDir(), "data")
	st, err := New(root, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	st.RecordAccess("persisted.md", "127.0.0.1", "test")
	st.Close()

	reopened, err := New(root, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	logs := reopened.ListAccess(10)
	if len(logs) != 1 || logs[0].Doc != "persisted.md" {
		t.Fatalf("Close 未落盘访问记录: %+v", logs)
	}
}

func TestRecordAccessConcurrentClose(t *testing.T) {
	st, err := New(t.TempDir(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				st.RecordAccess("doc.md", "127.0.0.1", "test")
			}
		}()
	}
	st.Close()
	wg.Wait()
	st.Close()
}
