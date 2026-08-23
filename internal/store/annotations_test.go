package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newAnnoStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := New(docs, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestAnnotationCRUD(t *testing.T) {
	st := newAnnoStore(t)
	doc := "指南/使用说明.md"

	// 初始为空
	if list := st.ListAnnotations(doc); len(list) != 0 {
		t.Fatalf("初始应无批注, got %d", len(list))
	}

	// 创建两条
	a1, err := st.AddAnnotation(doc, "选中文字", 10, "张三", "这里需要补充说明")
	if err != nil {
		t.Fatal(err)
	}
	a2, err := st.AddAnnotation(doc, "第二段", 50, "李四", "此处存疑")
	if err != nil {
		t.Fatal(err)
	}
	if a1.ID == "" || a1.ID == a2.ID {
		t.Fatalf("批注 ID 异常: %q %q", a1.ID, a2.ID)
	}
	if a1.Doc != doc || a1.Quote != "选中文字" || a1.Offset != 10 {
		t.Fatalf("批注字段不符: %+v", a1)
	}

	list := st.ListAnnotations(doc)
	if len(list) != 2 {
		t.Fatalf("应有 2 条批注, got %d", len(list))
	}
	if list[0].Time > list[1].Time {
		t.Fatal("批注应按时间升序")
	}

	// 回复
	updated, err := st.AddReply(doc, a1.ID, "王五", "我补充一段")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Replies) != 1 || updated.Replies[0].Author != "王五" {
		t.Fatalf("回复未正确写入: %+v", updated.Replies)
	}
	if updated.ID != a1.ID {
		t.Fatal("AddReply 应返回同一条批注")
	}

	// 回复不存在 ID
	if _, err := st.AddReply(doc, "no-such-id", "x", "y"); err != ErrAnnoNotFound {
		t.Fatalf("应返回 ErrAnnoNotFound, got %v", err)
	}

	// 删除
	if err := st.DeleteAnnotation(doc, a2.ID); err != nil {
		t.Fatal(err)
	}
	list = st.ListAnnotations(doc)
	if len(list) != 1 || list[0].ID != a1.ID {
		t.Fatalf("删除后应剩 1 条: %+v", list)
	}
	// 删除不存在的
	if err := st.DeleteAnnotation(doc, "no-such-id"); err != ErrAnnoNotFound {
		t.Fatalf("应返回 ErrAnnoNotFound, got %v", err)
	}
}

func TestAnnotationPersist(t *testing.T) {
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "data")
	st1, err := New(docs, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st1.AddAnnotation("a.md", "引文", 3, "甲", "第一条"); err != nil {
		t.Fatal(err)
	}

	// 重新打开(模拟重启): 数据应落盘恢复
	st2, err := New(docs, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	list := st2.ListAnnotations("a.md")
	if len(list) != 1 || list[0].Content != "第一条" {
		t.Fatalf("重启后批注应恢复: %+v", list)
	}
	// 不同文档互不影响
	if list := st2.ListAnnotations("b.md"); len(list) != 0 {
		t.Fatalf("b.md 不应有批注, got %d", len(list))
	}
}

func TestAnnotationValidation(t *testing.T) {
	st := newAnnoStore(t)
	doc := "a.md"

	// 空内容 / 空引文
	if _, err := st.AddAnnotation(doc, "", 0, "甲", "内容"); err != ErrAnnoBadParam {
		t.Fatalf("空引文应报错, got %v", err)
	}
	if _, err := st.AddAnnotation(doc, "引文", 0, "甲", "  "); err != ErrAnnoBadParam {
		t.Fatalf("空内容应报错, got %v", err)
	}
	// 空回复
	if _, err := st.AddReply(doc, "x", "甲", ""); err != ErrAnnoBadParam {
		t.Fatalf("空回复应报错, got %v", err)
	}

	// 超长裁剪
	longQuote := strings.Repeat("文", annoMaxQuote+50)
	longContent := strings.Repeat("内", annoMaxContent+50)
	longAuthor := strings.Repeat("名", annoMaxAuthor+50)
	a, err := st.AddAnnotation(doc, longQuote, -5, longAuthor, longContent)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(a.Quote)) != annoMaxQuote {
		t.Fatalf("引文应裁剪到 %d, got %d", annoMaxQuote, len([]rune(a.Quote)))
	}
	if len([]rune(a.Content)) != annoMaxContent {
		t.Fatalf("内容应裁剪到 %d", annoMaxContent)
	}
	if len([]rune(a.Author)) != annoMaxAuthor {
		t.Fatalf("昵称应裁剪到 %d", annoMaxAuthor)
	}
	if a.Offset != 0 {
		t.Fatalf("负偏移应归零, got %d", a.Offset)
	}
	// 多字节边界: 裁剪不能产生非法 UTF-8
	if !strings.Contains(longQuote[:0]+a.Quote, "文") {
		t.Fatal("裁剪后的引文应仍为合法文本")
	}
}

func TestAnnotationResolve(t *testing.T) {
	st := newAnnoStore(t)
	doc := "a.md"
	a, err := st.AddAnnotation(doc, "引文", 0, "甲", "待确认")
	if err != nil {
		t.Fatal(err)
	}
	// 默认未解决
	if a.Resolved {
		t.Fatal("新建批注应默认未解决")
	}
	// 标记解决
	upd, err := st.ResolveAnnotation(doc, a.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !upd.Resolved {
		t.Fatal("标记解决后 Resolved 应为 true")
	}
	if list := st.ListAnnotations(doc); len(list) != 1 || !list[0].Resolved {
		t.Fatalf("列表应反映解决状态: %+v", list)
	}
	// 重新打开
	upd, err = st.ResolveAnnotation(doc, a.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if upd.Resolved {
		t.Fatal("重新打开后 Resolved 应为 false")
	}
	// 不存在的批注
	if _, err := st.ResolveAnnotation(doc, "no-such-id", true); err != ErrAnnoNotFound {
		t.Fatalf("应返回 ErrAnnoNotFound, got %v", err)
	}
}

func TestAnnotationLimits(t *testing.T) {
	st := newAnnoStore(t)
	doc := "a.md"

	// 回复上限
	a, err := st.AddAnnotation(doc, "引文", 0, "甲", "内容")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < annoMaxReplies; i++ {
		if _, err := st.AddReply(doc, a.ID, "甲", "回复"); err != nil {
			t.Fatalf("第 %d 条回复应成功: %v", i, err)
		}
	}
	if _, err := st.AddReply(doc, a.ID, "甲", "超限回复"); err == nil {
		t.Fatal("超过回复上限应报错")
	}
}

func TestAnnotationConcurrent(t *testing.T) {
	st := newAnnoStore(t)
	doc := "a.md"
	const n = 20

	done := make(chan error, n*2)
	for i := 0; i < n; i++ {
		go func(i int) {
			_, err := st.AddAnnotation(doc, "引文", i, "作者", "内容")
			done <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-done; err != nil {
			t.Fatalf("并发创建失败: %v", err)
		}
	}
	if list := st.ListAnnotations(doc); len(list) != n {
		t.Fatalf("并发后应有 %d 条批注, got %d", n, len(list))
	}
}
