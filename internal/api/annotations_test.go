package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// ---- 批注接口测试 ----

// annoReq 发送批注请求并返回状态码与原始响应体。
func annoReq(t *testing.T, method, url, body, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestAnnotationFlow(t *testing.T) {
	ts := newTestServer(t, "")
	defer ts.Close()
	base := ts.URL + "/api/annotations"

	// 初始为空
	code, body := annoReq(t, "GET", base+"?path=a.md", "", "")
	if code != http.StatusOK {
		t.Fatalf("列表应 200, got %d", code)
	}
	var list []map[string]any
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("列表解析失败: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("初始应无批注, got %d", len(list))
	}

	// 创建
	code, body = annoReq(t, "POST", base, `{"doc":"a.md","quote":"hello","offset":5,"author":"张三","content":"这里有疑问"}`, "")
	if code != http.StatusOK {
		t.Fatalf("创建应 200, got %d: %s", code, body)
	}
	var created map[string]any
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	if id == "" || created["doc"] != "a.md" || created["quote"] != "hello" {
		t.Fatalf("创建结果字段异常: %v", created)
	}

	// 列表含 1 条
	_, body = annoReq(t, "GET", base+"?path=a.md", "", "")
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0]["id"] != id {
		t.Fatalf("列表应含新批注: %v", list)
	}

	// 回复
	code, body = annoReq(t, "POST", base+"/"+id+"/reply", `{"doc":"a.md","author":"李四","content":"我来回答"}`, "")
	if code != http.StatusOK {
		t.Fatalf("回复应 200, got %d: %s", code, body)
	}
	var updated map[string]any
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatal(err)
	}
	replies, _ := updated["replies"].([]any)
	if len(replies) != 1 {
		t.Fatalf("回复后应含 1 条回复: %v", updated)
	}

	// 删除
	code, _ = annoReq(t, "DELETE", base+"/"+id+"?path=a.md", "", "")
	if code != http.StatusOK {
		t.Fatalf("删除应 200, got %d", code)
	}
	_, body = annoReq(t, "GET", base+"?path=a.md", "", "")
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("删除后应无批注, got %d", len(list))
	}

	// 删除不存在 / 回复不存在的批注
	code, _ = annoReq(t, "DELETE", base+"/nope?path=a.md", "", "")
	if code != http.StatusNotFound {
		t.Fatalf("删除不存在应 404, got %d", code)
	}
	code, _ = annoReq(t, "POST", base+"/nope/reply", `{"doc":"a.md","author":"x","content":"y"}`, "")
	if code != http.StatusNotFound {
		t.Fatalf("回复不存在应 404, got %d", code)
	}
}

func TestAnnotationValidation(t *testing.T) {
	ts := newTestServer(t, "")
	defer ts.Close()
	base := ts.URL + "/api/annotations"

	// 缺 doc
	code, _ := annoReq(t, "POST", base, `{"quote":"q","content":"c"}`, "")
	if code != http.StatusBadRequest {
		t.Fatalf("缺 doc 应 400, got %d", code)
	}
	// 空内容
	code, _ = annoReq(t, "POST", base, `{"doc":"a.md","quote":"q","content":"  "}`, "")
	if code != http.StatusBadRequest {
		t.Fatalf("空内容应 400, got %d", code)
	}
	// 空回复
	code, _ = annoReq(t, "POST", base+"/x/reply", `{"doc":"a.md","content":""}`, "")
	if code != http.StatusBadRequest {
		t.Fatalf("空回复应 400, got %d", code)
	}
	// 列表缺 path
	code, _ = annoReq(t, "GET", base, "", "")
	if code != http.StatusBadRequest {
		t.Fatalf("缺 path 应 400, got %d", code)
	}
}

func TestAnnotationAuthProtected(t *testing.T) {
	ts := newTestServer(t, "secret-pass")
	defer ts.Close()
	base := ts.URL + "/api/annotations"

	// 未登录创建被拦截
	code, _ := annoReq(t, "POST", base, `{"doc":"a.md","quote":"q","content":"c"}`, "")
	if code != http.StatusUnauthorized {
		t.Fatalf("未登录创建应 401, got %d", code)
	}
	// 未登录列表被拦截
	code, _ = annoReq(t, "GET", base+"?path=a.md", "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("未登录列表应 401, got %d", code)
	}
	// 登录后成功
	code, data := postJSON(t, ts.URL+"/api/auth/login", `{"password":"secret-pass"}`, "")
	if code != http.StatusOK {
		t.Fatalf("登录应 200, got %d", code)
	}
	token, _ := data["token"].(string)
	code, body := annoReq(t, "POST", base, `{"doc":"a.md","quote":"q","content":"c"}`, token)
	if code != http.StatusOK {
		t.Fatalf("登录后创建应 200, got %d: %s", code, body)
	}
}

func TestAnnotationResolveFlow(t *testing.T) {
	ts := newTestServer(t, "")
	defer ts.Close()
	base := ts.URL + "/api/annotations"

	// 创建
	code, body := annoReq(t, "POST", base, `{"doc":"a.md","quote":"hello","content":"待确认"}`, "")
	if code != http.StatusOK {
		t.Fatalf("创建应 200, got %d", code)
	}
	var created map[string]any
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	if created["resolved"] != false {
		t.Fatalf("新建批注应默认未解决: %v", created)
	}

	// 标记解决
	code, body = annoReq(t, "POST", base+"/"+id+"/resolve", `{"doc":"a.md","resolved":true}`, "")
	if code != http.StatusOK {
		t.Fatalf("解决应 200, got %d: %s", code, body)
	}
	var resolved map[string]any
	if err := json.Unmarshal(body, &resolved); err != nil {
		t.Fatal(err)
	}
	if resolved["resolved"] != true || resolved["id"] != id {
		t.Fatalf("解决结果异常: %v", resolved)
	}
	// 列表反映状态
	_, body = annoReq(t, "GET", base+"?path=a.md", "", "")
	var list []map[string]any
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0]["resolved"] != true {
		t.Fatalf("列表应反映解决状态: %v", list)
	}
	// 重新打开
	code, body = annoReq(t, "POST", base+"/"+id+"/resolve", `{"doc":"a.md","resolved":false}`, "")
	if code != http.StatusOK {
		t.Fatalf("重新打开应 200, got %d", code)
	}
	if err := json.Unmarshal(body, &resolved); err != nil {
		t.Fatal(err)
	}
	if resolved["resolved"] != false {
		t.Fatalf("重新打开后应未解决: %v", resolved)
	}
	// 不存在的批注
	code, _ = annoReq(t, "POST", base+"/nope/resolve", `{"doc":"a.md","resolved":true}`, "")
	if code != http.StatusNotFound {
		t.Fatalf("解决不存在应 404, got %d", code)
	}
}

func TestAnnotationMultiRoot(t *testing.T) {
	ts := multiRootServer(t)
	defer ts.Close()
	base := ts.URL + "/api/annotations"

	// 两个根的文档路径不同, 批注互不干扰
	code, _ := annoReq(t, "POST", base, `{"doc":"root-a/README.md","quote":"alpha","content":"A 的批注"}`, "")
	if code != http.StatusOK {
		t.Fatalf("root-a 创建应 200, got %d", code)
	}
	code, _ = annoReq(t, "POST", base, `{"doc":"root-b/guide.md","quote":"beta","content":"B 的批注"}`, "")
	if code != http.StatusOK {
		t.Fatalf("root-b 创建应 200, got %d", code)
	}

	_, body := annoReq(t, "GET", base+"?path=root-a/README.md", "", "")
	var listA []map[string]any
	if err := json.Unmarshal(body, &listA); err != nil {
		t.Fatal(err)
	}
	if len(listA) != 1 || listA[0]["doc"] != "root-a/README.md" {
		t.Fatalf("root-a 应只有自己的批注: %v", listA)
	}

	_, body = annoReq(t, "GET", base+"?path=root-b/guide.md", "", "")
	var listB []map[string]any
	if err := json.Unmarshal(body, &listB); err != nil {
		t.Fatal(err)
	}
	if len(listB) != 1 || listB[0]["doc"] != "root-b/guide.md" {
		t.Fatalf("root-b 应只有自己的批注: %v", listB)
	}
}
