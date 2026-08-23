// 文档批注接口: 创建/列表/回复/删除。
// 启用访问密码时, 这些接口同样受 requireAuth 保护(需登录令牌)。
package api

import (
	"errors"
	"net/http"
	"strings"

	"docshare/internal/store"
)

// annoDoc 规范化批注所属文档路径:
// 解析多根路由后还原带根前缀的完整路径(与 /api/doc 返回的 path 一致)。
func (s *Server) annoDoc(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	st, rel := s.resolveStore(path)
	if rel == "" {
		return "", false
	}
	return s.rootPrefix(st, rel), true
}

// handleListAnnotations GET /api/annotations?path=xxx 列出文档批注。
func (s *Server) handleListAnnotations(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.annoDoc(r.URL.Query().Get("path"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "缺少 path 参数")
		return
	}
	st, _ := s.resolveStore(doc)
	writeJSON(w, http.StatusOK, st.ListAnnotations(doc))
}

// handleAddAnnotation POST /api/annotations 创建批注。
func (s *Server) handleAddAnnotation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Doc     string `json:"doc"`
		Quote   string `json:"quote"`
		Offset  int    `json:"offset"`
		Author  string `json:"author"`
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	doc, ok := s.annoDoc(body.Doc)
	if !ok {
		writeErr(w, http.StatusBadRequest, "缺少 doc 参数")
		return
	}
	st, _ := s.resolveStore(doc)
	a, err := st.AddAnnotation(doc, body.Quote, body.Offset, body.Author, body.Content)
	if err != nil {
		if errors.Is(err, store.ErrAnnoBadParam) {
			writeErr(w, http.StatusBadRequest, "批注内容不能为空(引文与正文均必填)")
			return
		}
		writeErr(w, http.StatusInternalServerError, "保存批注失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// handleAddReply POST /api/annotations/{id}/reply 回复批注。
func (s *Server) handleAddReply(w http.ResponseWriter, r *http.Request) {
	annoID := strings.TrimSpace(r.PathValue("id"))
	var body struct {
		Doc     string `json:"doc"`
		Author  string `json:"author"`
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	doc, ok := s.annoDoc(body.Doc)
	if !ok {
		writeErr(w, http.StatusBadRequest, "缺少 doc 参数")
		return
	}
	st, _ := s.resolveStore(doc)
	a, err := st.AddReply(doc, annoID, body.Author, body.Content)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrAnnoNotFound):
			writeErr(w, http.StatusNotFound, "批注不存在或已被删除")
		case errors.Is(err, store.ErrAnnoBadParam):
			writeErr(w, http.StatusBadRequest, "回复内容不能为空")
		default:
			writeErr(w, http.StatusInternalServerError, "保存回复失败: "+err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// handleResolveAnnotation POST /api/annotations/{id}/resolve 标记解决 / 重新打开。
func (s *Server) handleResolveAnnotation(w http.ResponseWriter, r *http.Request) {
	annoID := strings.TrimSpace(r.PathValue("id"))
	var body struct {
		Doc      string `json:"doc"`
		Resolved bool   `json:"resolved"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	doc, ok := s.annoDoc(body.Doc)
	if !ok {
		writeErr(w, http.StatusBadRequest, "缺少 doc 参数")
		return
	}
	st, _ := s.resolveStore(doc)
	a, err := st.ResolveAnnotation(doc, annoID, body.Resolved)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrAnnoNotFound):
			writeErr(w, http.StatusNotFound, "批注不存在或已被删除")
		case errors.Is(err, store.ErrAnnoBadParam):
			writeErr(w, http.StatusBadRequest, "缺少批注 ID")
		default:
			writeErr(w, http.StatusInternalServerError, "保存状态失败: "+err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// handleDeleteAnnotation DELETE /api/annotations/{id}?path=xxx 删除批注。
func (s *Server) handleDeleteAnnotation(w http.ResponseWriter, r *http.Request) {
	annoID := strings.TrimSpace(r.PathValue("id"))
	doc, ok := s.annoDoc(r.URL.Query().Get("path"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "缺少 path 参数")
		return
	}
	st, _ := s.resolveStore(doc)
	if err := st.DeleteAnnotation(doc, annoID); err != nil {
		if errors.Is(err, store.ErrAnnoNotFound) {
			writeErr(w, http.StatusNotFound, "批注不存在或已被删除")
			return
		}
		writeErr(w, http.StatusInternalServerError, "删除批注失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
