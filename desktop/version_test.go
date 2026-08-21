package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.9.9", 1},
		{"0.9.0", "1.0.0", -1},
		{"1.0", "1.0.0", 0},
		{"1.0.10", "1.0.9", 1},
		{"", "1.0.0", -1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDownloadFile(t *testing.T) {
	content := "fake-installer-bytes"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(content))
	}))
	defer ts.Close()

	dest := filepath.Join(t.TempDir(), "update.exe")
	if err := downloadFile(ts.URL+"/setup.exe", dest); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("下载内容不符: %q", string(data))
	}
}

func TestDownloadFileError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer ts.Close()
	if err := downloadFile(ts.URL+"/x", filepath.Join(t.TempDir(), "x.exe")); err == nil {
		t.Fatal("404 应返回错误")
	} else if !strings.Contains(err.Error(), "404") {
		t.Fatalf("错误信息应含状态码: %v", err)
	}
}

func TestWriteUpdateBat(t *testing.T) {
	// 含空格路径(最易出问题的场景)
	installer := `C:\Users\test user\AppData\Local\Temp\DocShare-Setup-1.1.1.exe`
	batPath := filepath.Join(t.TempDir(), "update.bat")
	if err := writeUpdateBat(batPath, installer); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(batPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)
	want := `start "" "C:\Users\test user\AppData\Local\Temp\DocShare-Setup-1.1.1.exe"`
	if !strings.Contains(s, want) {
		t.Fatalf("bat 应包含正确的 start 命令:\n---\n%s\n---\n缺少: %s", s, want)
	}
	if !strings.Contains(s, "timeout /t 3") {
		t.Fatalf("bat 应包含延迟: %s", s)
	}
	if !strings.Contains(s, "del \"%~f0\"") {
		t.Fatalf("bat 应自删: %s", s)
	}
}
