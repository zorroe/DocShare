// Package store 负责文档目录的遍历、读取与访问记录的持久化。
package store

import (
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

var (
	ErrNotFound  = errors.New("资源不存在")
	ErrForbidden = errors.New("路径超出文档根目录, 已拒绝")
)

// Node 文档目录树节点。
type Node struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	IsDir    bool   `json:"isDir"`
	Size     int64  `json:"size,omitempty"`
	Modified string `json:"modified,omitempty"`
	Children []*Node `json:"children,omitempty"`
}

// Store 持有文档根目录与数据目录。
type Store struct {
	root        string // 文档根目录(绝对路径)
	dataDir     string // 数据目录(访问记录存档)
	ready       bool   // 文档目录是否可用
	searchIndex *SearchIndex
}

// New 创建 Store; 文档目录不存在时进入未配置状态(服务可启动, 目录树为空)。
func New(root, dataDir string) (*Store, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析文档目录失败: %w", err)
	}
	st := &Store{root: absRoot, dataDir: dataDir, searchIndex: newSearchIndex()}
	if info, err := os.Stat(absRoot); err == nil && info.IsDir() {
		st.ready = true
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	return st, nil
}

// Ready 返回文档目录是否已配置。
func (s *Store) Ready() bool { return s.ready }

// Root 返回文档根目录绝对路径。
func (s *Store) Root() string { return s.root }

// SetRoot 热切换文档根目录(桌面端配置后调用)。
func (s *Store) SetRoot(root string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if info, err := os.Stat(absRoot); err != nil || !info.IsDir() {
		return fmt.Errorf("文档目录不可用: %s", absRoot)
	}
	s.root = absRoot
	s.ready = true
	return nil
}

func isMarkdown(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown"
}

// Resolve 将相对路径安全解析为根目录内的绝对路径, 防止目录穿越。
func (s *Store) Resolve(rel string) (string, error) {
	if !s.ready {
		return "", ErrNotFound
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return "", ErrNotFound
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", ErrForbidden
	}
	full := filepath.Join(s.root, clean)

	// 解析符号链接后再次校验, 防止软链逃逸
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", ErrNotFound
	}
	rootResolved, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return "", ErrForbidden
	}
	if resolved != rootResolved &&
		!strings.HasPrefix(resolved, rootResolved+string(os.PathSeparator)) {
		return "", ErrForbidden
	}
	return resolved, nil
}

// Tree 返回文档根目录的目录树(仅包含 Markdown 文件)。
func (s *Store) Tree() (*Node, error) {
	root := &Node{Name: filepath.Base(s.root), Path: ".", IsDir: true}
	if !s.ready {
		return root, nil
	}
	if err := s.walk(s.root, root); err != nil {
		return nil, err
	}
	return root, nil
}

func (s *Store) walk(dir string, node *Node) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue // 跳过隐藏文件
		}
		rel, _ := filepath.Rel(s.root, filepath.Join(dir, name))
		rel = filepath.ToSlash(rel)
		if e.IsDir() {
			child := &Node{Name: name, Path: rel, IsDir: true}
			if err := s.walk(filepath.Join(dir, name), child); err != nil {
				return err
			}
			if len(child.Children) > 0 {
				node.Children = append(node.Children, child)
			}
			continue
		}
		if isMarkdown(name) {
			info, err := e.Info()
			if err != nil {
				continue
			}
			node.Children = append(node.Children, &Node{
				Name:     name,
				Path:     rel,
				Size:     info.Size(),
				Modified: info.ModTime().Format(time.RFC3339),
			})
		}
	}
	sort.Slice(node.Children, func(i, j int) bool {
		a, b := node.Children[i], node.Children[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return nil
}

// ReadDoc 读取一篇 Markdown 文档的原始内容。
func (s *Store) ReadDoc(rel string) (content string, modified string, size int64, err error) {
	full, err := s.Resolve(rel)
	if err != nil {
		return "", "", 0, err
	}
	if !isMarkdown(full) {
		return "", "", 0, ErrForbidden
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", "", 0, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return string(data), "", int64(len(data)), nil
	}
	return string(data), info.ModTime().Format(time.RFC3339), info.Size(), nil
}

// ---- 访问记录 ----

// AccessRecord 一条文档访问记录。
type AccessRecord struct {
	Time string `json:"time"`
	Doc  string `json:"doc"` // 文档相对路径
	IP   string `json:"ip"`
	UA   string `json:"ua"`
}

const (
	accessMax   = 500 // 最多保留的记录数
	accessLimit = 200 // 默认返回条数
)

var accessMu sync.Mutex

func (s *Store) accessFile() string {
	return filepath.Join(s.dataDir, "access.json")
}

func (s *Store) loadAccess() []AccessRecord {
	data, err := os.ReadFile(s.accessFile())
	if err != nil {
		return nil
	}
	var list []AccessRecord
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	return list
}

// RecordAccess 记录一次文档访问(文档浏览成功后调用)。
func (s *Store) RecordAccess(doc, ip, ua string) {
	accessMu.Lock()
	defer accessMu.Unlock()
	if len(ua) > 140 {
		ua = ua[:140]
	}
	list := s.loadAccess()
	list = append([]AccessRecord{{
		Time: time.Now().Format(time.RFC3339),
		Doc:  doc,
		IP:   ip,
		UA:   ua,
	}}, list...)
	if len(list) > accessMax {
		list = list[:accessMax]
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.accessFile(), data, 0o644)
}

// ListAccess 返回最近 N 条访问记录。
func (s *Store) ListAccess(limit int) []AccessRecord {
	accessMu.Lock()
	defer accessMu.Unlock()
	if limit <= 0 || limit > accessMax {
		limit = accessLimit
	}
	list := s.loadAccess()
	if len(list) > limit {
		list = list[:limit]
	}
	return list
}
