// Package autostart 提供 Windows 开机自启动(注册表 Run 键)管理。
package autostart

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// IsEnabled 查询应用是否已配置开机自启动。
func IsEnabled(appName string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	_, _, err = k.GetStringValue(appName)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// SetEnabled 设置/取消开机自启动。
func SetEnabled(appName string, enabled bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("无法访问注册表: %w", err)
	}
	defer k.Close()
	if !enabled {
		if err := k.DeleteValue(appName); err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("清除自启动失败: %w", err)
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取程序路径失败: %w", err)
	}
	if err := k.SetStringValue(appName, `"`+exe+`"`); err != nil {
		return fmt.Errorf("写入自启动失败: %w", err)
	}
	return nil
}
