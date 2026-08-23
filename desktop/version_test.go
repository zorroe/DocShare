package main

import (
	"crypto/sha256"
	"encoding/hex"
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
	wantHash := sha256.Sum256([]byte(content))
	if err := downloadVerifiedFile(ts.URL+"/setup.exe", dest, 1<<20, wantHash); err != nil {
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
	if err := downloadVerifiedFile(ts.URL+"/x", filepath.Join(t.TempDir(), "x.exe"), 1<<20, [32]byte{}); err == nil {
		t.Fatal("404 应返回错误")
	} else if !strings.Contains(err.Error(), "404") {
		t.Fatalf("错误信息应含状态码: %v", err)
	}
}

func TestDownloadFileRejectsHashMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tampered"))
	}))
	defer ts.Close()
	dest := filepath.Join(t.TempDir(), "update.exe")
	if err := downloadVerifiedFile(ts.URL, dest, 1<<20, sha256.Sum256([]byte("expected"))); err == nil {
		t.Fatal("哈希不匹配应返回错误")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("校验失败不应留下目标文件, stat err=%v", err)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf("校验失败不应留下临时文件, stat err=%v", err)
	}
}

func TestDownloadFileRejectsOversizedPayload(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 1025)))
	}))
	defer ts.Close()
	dest := filepath.Join(t.TempDir(), "update.exe")
	if err := downloadVerifiedFile(ts.URL, dest, 1024, sha256.Sum256(nil)); err == nil {
		t.Fatal("超过大小上限应返回错误")
	}
}

func TestParseChecksum(t *testing.T) {
	want := sha256.Sum256([]byte("installer"))
	got, err := parseChecksum(strings.ToUpper(strings.Repeat("0", 0)) + strings.ToUpper(hex.EncodeToString(want[:])) + "  DocShare-Setup.exe\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("校验值解析错误: %x != %x", got, want)
	}
	if _, err := parseChecksum("not-a-checksum"); err == nil {
		t.Fatal("非法校验文件应被拒绝")
	}
}

func TestValidateInstallerPath(t *testing.T) {
	valid := filepath.Join(os.TempDir(), "DocShare-Setup-1.4.3.exe")
	if got, err := validateInstallerPath(valid); err != nil || got == "" {
		t.Fatalf("合法安装包路径被拒绝: %q, %v", got, err)
	}
	bad := filepath.Join(os.TempDir(), "DocShare-Setup-%PATH%.exe")
	if _, err := validateInstallerPath(bad); err == nil {
		t.Fatal("应拒绝可触发批处理变量展开的文件名")
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
