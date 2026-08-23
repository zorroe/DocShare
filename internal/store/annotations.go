// 文档批注: 用户可在 Web 端选中正文创建批注, 其他人可回复。
// 批注按文档存储: dataDir/annotations/<sha1(文档路径)>.json。
// 每条批注记录选中文本(quote)与文档纯文本偏移(offset)用于渲染定位;
// 文档内容变化后 quote 可能失配, 批注仍保留在列表中(前端降级为列表展示)。
package store

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	annoMaxQuote   = 300  // 选中文本最长(字符)
	annoMaxContent = 2000 // 批注/回复内容最长(字符)
	annoMaxAuthor  = 50   // 昵称最长(字符)
	annoMaxReplies = 200  // 单条批注最多回复数
	annoMaxPerDoc  = 1000 // 单文档最多批注数
)

var (
	// ErrAnnoNotFound 批注不存在(或已被删除)。
	ErrAnnoNotFound = errors.New("批注不存在")
	// ErrAnnoBadParam 批注参数不合法(必填项为空)。
	ErrAnnoBadParam = errors.New("批注参数不合法")
)

// Reply 一条批注回复。
type Reply struct {
	ID      string `json:"id"`
	Author  string `json:"author"`
	Content string `json:"content"`
	Time    string `json:"time"`
}

// Annotation 一条批注(含回复线程)。
type Annotation struct {
	ID       string  `json:"id"`
	Doc      string  `json:"doc"`    // 文档路径(与 /api/doc 的 path 一致, 多根时含根前缀)
	Quote    string  `json:"quote"`  // 选中文本(定位用)
	Offset   int     `json:"offset"` // 选中文本在文档纯文本中的起始偏移(辅助定位)
	Author   string  `json:"author"`
	Content  string  `json:"content"`
	Time     string  `json:"time"`
	Resolved bool    `json:"resolved"` // 是否已解决(问题确认无误后标记)
	Replies  []Reply `json:"replies"`
}

// ---- 持久化 ----

// annoMu 串行化批注文件读写(写频率低, 全局锁简单可靠)。
var annoMu sync.Mutex

// annoFile 返回文档批注文件路径(sha1 文件名, 无路径注入风险)。
func (s *Store) annoFile(doc string) string {
	sum := sha1.Sum([]byte(doc))
	return filepath.Join(s.dataDir, "annotations", hex.EncodeToString(sum[:])+".json")
}

// newAnnoID 生成随机批注 ID(16 hex)。
func newAnnoID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// sanitizeAnno 裁剪并去除首尾空白(按字符数, 避免截断多字节)。
func sanitizeAnno(s string, max int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return s
}

// loadAnnoFile 读取文档批注文件; 不存在或损坏返回空列表。
func (s *Store) loadAnnoFile(doc string) []Annotation {
	data, err := os.ReadFile(s.annoFile(doc))
	if err != nil {
		return nil
	}
	var list []Annotation
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	return list
}

// saveAnnoFile 原子写入批注文件(临时文件 + rename)。
func (s *Store) saveAnnoFile(doc string, list []Annotation) error {
	dir := filepath.Dir(s.annoFile(doc))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.annoFile(doc) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.annoFile(doc))
}

// ---- 接口 ----

// ListAnnotations 返回文档的全部批注(按创建时间升序)。
func (s *Store) ListAnnotations(doc string) []Annotation {
	annoMu.Lock()
	defer annoMu.Unlock()
	list := s.loadAnnoFile(doc)
	if list == nil {
		return []Annotation{}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Time < list[j].Time })
	return list
}

// AddAnnotation 创建一条批注, 返回完整批注。
func (s *Store) AddAnnotation(doc, quote string, offset int, author, content string) (*Annotation, error) {
	quote = sanitizeAnno(quote, annoMaxQuote)
	author = sanitizeAnno(author, annoMaxAuthor)
	content = sanitizeAnno(content, annoMaxContent)
	if quote == "" || content == "" {
		return nil, ErrAnnoBadParam
	}
	if offset < 0 {
		offset = 0
	}
	annoMu.Lock()
	defer annoMu.Unlock()
	list := s.loadAnnoFile(doc)
	if len(list) >= annoMaxPerDoc {
		return nil, errors.New("该文档批注数量已达上限")
	}
	a := &Annotation{
		ID:      newAnnoID(),
		Doc:     doc,
		Quote:   quote,
		Offset:  offset,
		Author:  author,
		Content: content,
		Time:    time.Now().Format(time.RFC3339Nano),
	}
	list = append(list, *a)
	if err := s.saveAnnoFile(doc, list); err != nil {
		return nil, err
	}
	return a, nil
}

// AddReply 向指定批注追加回复, 返回更新后的批注。
func (s *Store) AddReply(doc, annoID, author, content string) (*Annotation, error) {
	author = sanitizeAnno(author, annoMaxAuthor)
	content = sanitizeAnno(content, annoMaxContent)
	if annoID == "" || content == "" {
		return nil, ErrAnnoBadParam
	}
	annoMu.Lock()
	defer annoMu.Unlock()
	list := s.loadAnnoFile(doc)
	for i := range list {
		if list[i].ID == annoID {
			if len(list[i].Replies) >= annoMaxReplies {
				return nil, errors.New("回复数量已达上限")
			}
			list[i].Replies = append(list[i].Replies, Reply{
				ID:      newAnnoID(),
				Author:  author,
				Content: content,
				Time:    time.Now().Format(time.RFC3339Nano),
			})
			if err := s.saveAnnoFile(doc, list); err != nil {
				return nil, err
			}
			return &list[i], nil
		}
	}
	return nil, ErrAnnoNotFound
}

// ResolveAnnotation 标记批注为已解决 / 重新打开, 返回更新后的批注。
func (s *Store) ResolveAnnotation(doc, annoID string, resolved bool) (*Annotation, error) {
	if annoID == "" {
		return nil, ErrAnnoBadParam
	}
	annoMu.Lock()
	defer annoMu.Unlock()
	list := s.loadAnnoFile(doc)
	for i := range list {
		if list[i].ID == annoID {
			list[i].Resolved = resolved
			if err := s.saveAnnoFile(doc, list); err != nil {
				return nil, err
			}
			return &list[i], nil
		}
	}
	return nil, ErrAnnoNotFound
}

// DeleteAnnotation 删除一条批注(含其全部回复); 文档批注清空后移除文件。
func (s *Store) DeleteAnnotation(doc, annoID string) error {
	annoMu.Lock()
	defer annoMu.Unlock()
	list := s.loadAnnoFile(doc)
	out := list[:0]
	found := false
	for _, a := range list {
		if a.ID == annoID {
			found = true
			continue
		}
		out = append(out, a)
	}
	if !found {
		return ErrAnnoNotFound
	}
	if len(out) == 0 {
		return os.Remove(s.annoFile(doc))
	}
	return s.saveAnnoFile(doc, out)
}
