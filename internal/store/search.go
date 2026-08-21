package store

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// ---- 全文搜索 ----
// 轻量倒排索引: 中文按双字滑动窗口(bigram)分词, 英文/数字按词分词。
// 索引惰性构建并按文件 mtime 增量更新。

var (
	reWord = regexp.MustCompile(`[a-zA-Z0-9]+`)
	reHan  = regexp.MustCompile(`[\p{Han}]+`)
)

// SearchResult 一条搜索结果。
type SearchResult struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Snippet string `json:"snippet"`
	Score   int    `json:"-"`
}

type docEntry struct {
	path     string
	tokens   map[string]int // token -> 出现次数
	modified time.Time
	size    int64
}

// SearchIndex 倒排索引。
type SearchIndex struct {
	mu    sync.RWMutex
	docs  map[string]*docEntry // path -> entry
	index map[string][]string  // token -> paths
	built bool
}

func newSearchIndex() *SearchIndex {
	return &SearchIndex{
		docs:  map[string]*docEntry{},
		index: map[string][]string{},
	}
}

// tokenize 分词: 英文词 + 中文连续段的双字滑动窗口。
func tokenize(text string) []string {
	var tokens []string
	for _, w := range reWord.FindAllString(text, -1) {
		tokens = append(tokens, strings.ToLower(w))
	}
	// 中文: 提取连续汉字段, 每段按双字滑动窗口切分
	for _, seg := range reHan.FindAllString(text, -1) {
		runes := []rune(seg)
		if len(runes) == 1 {
			tokens = append(tokens, string(runes))
			continue
		}
		for i := 0; i+1 < len(runes); i++ {
			tokens = append(tokens, string(runes[i:i+2]))
		}
		// 单字补充: 保证单字查询也能命中
		for _, r := range runes {
			tokens = append(tokens, string(r))
		}
	}
	return tokens
}

// addDoc 将文档内容并入索引。
func (idx *SearchIndex) addDoc(path string, content string, modified time.Time, size int64) {
	tokens := tokenize(content)
	if len(tokens) == 0 {
		return
	}
	counts := map[string]int{}
	for _, t := range tokens {
		counts[t]++
	}
	entry := &docEntry{path: path, tokens: counts, modified: modified, size: size}
	if old, ok := idx.docs[path]; ok {
		idx.removeDoc(old)
	}
	idx.docs[path] = entry
	for t := range counts {
		idx.index[t] = append(idx.index[t], path)
	}
}

func (idx *SearchIndex) removeDoc(entry *docEntry) {
	for t := range entry.tokens {
		paths := idx.index[t]
		for i, p := range paths {
			if p == entry.path {
				idx.index[t] = append(paths[:i], paths[i+1:]...)
				break
			}
		}
		if len(idx.index[t]) == 0 {
			delete(idx.index, t)
		}
	}
	delete(idx.docs, entry.path)
}

// ensure 惰性构建/增量更新索引(校验文件 mtime)。
func (s *Store) ensureIndex() {
	idx := s.searchIndex
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if !s.ready {
		idx.built = false
		return
	}

	// 全量扫描: 找到所有 md 文件并核对 mtime
	found := map[string]os.FileInfo{}
	filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && path != s.root {
				return filepath.SkipDir
			}
			return nil
		}
		if isMarkdown(info.Name()) {
			found[path] = info
		}
		return nil
	})

	if !idx.built {
		// 首次: 全部索引
		for path, info := range found {
			idx.indexFile(path, info)
		}
		// 清理已删除文档
		for p := range idx.docs {
			if _, ok := found[p]; !ok {
				idx.removeDoc(idx.docs[p])
			}
		}
		idx.built = true
		return
	}

	// 增量: 仅处理 mtime/size 变化的文件与新增文件
	for path, info := range found {
		if old, ok := idx.docs[path]; ok {
			if old.modified.Equal(info.ModTime()) && old.size == info.Size() {
				continue
			}
		}
		idx.indexFile(path, info)
	}
	for p := range idx.docs {
		if _, ok := found[p]; !ok {
			idx.removeDoc(idx.docs[p])
		}
	}
}

func (idx *SearchIndex) indexFile(path string, info os.FileInfo) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	idx.addDoc(path, string(data), info.ModTime(), info.Size())
}

// Search 全文搜索, 返回按相关度排序的结果(含摘要片段)。
func (s *Store) Search(query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	s.ensureIndex()

	idx := s.searchIndex
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	// 交集: 所有 token 都必须命中
	candidates := map[string]int{}
	first := true
	for _, t := range tokens {
		paths := idx.index[t]
		hit := map[string]int{}
		for _, p := range paths {
			hit[p] = idx.docs[p].tokens[t]
		}
		if first {
			candidates = hit
			first = false
		} else {
			for p := range candidates {
				if _, ok := hit[p]; !ok {
					delete(candidates, p)
				} else {
					candidates[p] += hit[p]
				}
			}
		}
		if len(candidates) == 0 {
			break
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	results := make([]SearchResult, 0, len(candidates))
	for p, score := range candidates {
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			continue
		}
		results = append(results, SearchResult{
			Path:  filepath.ToSlash(rel),
			Name:  filepath.Base(p),
			Score: score,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Path < results[j].Path
	})
	if len(results) > 50 {
		results = results[:50]
	}

	// 生成摘要片段(读文件定位首个命中词)
	for i := range results {
		abs := filepath.Join(s.root, filepath.FromSlash(results[i].Path))
		results[i].Snippet = snippetOf(abs, query, tokens[0])
	}
	return results, nil
}

// snippetOf 生成命中上下文片段(约 100 字符)。
func snippetOf(absPath, query, firstToken string) string {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}
	content := string(data)
	lower := strings.ToLower(content)
	// 优先定位完整查询词, 否则首个 token
	pos := strings.Index(lower, strings.ToLower(query))
	if pos < 0 {
		pos = strings.Index(lower, firstToken)
	}
	if pos < 0 {
		if len(content) > 100 {
			return content[:100] + "…"
		}
		return content
	}
	start := pos - 40
	if start < 0 {
		start = 0
	}
	end := pos + len(query) + 60
	if end > len(content) {
		end = len(content)
	}
	// 保证不截断在 rune 中间
	start = truncateToRune(content, start)
	end = truncateToRune(content, end)
	prefix, suffix := "", ""
	if start > 0 {
		prefix = "…"
	}
	if end < len(content) {
		suffix = "…"
	}
	return prefix + content[start:end] + suffix
}

func truncateToRune(s string, pos int) int {
	if pos >= len(s) {
		return len(s)
	}
	for pos > 0 && !utf8.RuneStart(s[pos]) {
		pos--
	}
	return pos
}

// countHan 判断字符串是否包含汉字(供测试断言用)。
func countHan(s string) int {
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			n++
		}
	}
	return n
}
