//go:build windows

package config

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const protectedPasswordPrefix = "dpapi:"

func protectPassword(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	plain := []byte(password)
	in := windows.DataBlob{Size: uint32(len(plain)), Data: &plain[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", fmt.Errorf("加密访问密码失败: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	ciphertext := append([]byte(nil), unsafe.Slice(out.Data, out.Size)...)
	return protectedPasswordPrefix + base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func unprotectPassword(password string) (string, error) {
	if password == "" || !strings.HasPrefix(password, protectedPasswordPrefix) {
		return password, nil // 兼容旧版明文配置，下次保存时自动迁移。
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(password, protectedPasswordPrefix))
	if err != nil || len(ciphertext) == 0 {
		return "", fmt.Errorf("访问密码密文格式错误")
	}
	in := windows.DataBlob{Size: uint32(len(ciphertext)), Data: &ciphertext[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", fmt.Errorf("解密访问密码失败: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	plain := append([]byte(nil), unsafe.Slice(out.Data, out.Size)...)
	return string(plain), nil
}
