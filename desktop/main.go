// DocShare 桌面端 - Wails v2 应用入口。
// 单实例互斥: 重复启动时提示并退出。
// 托盘支持: 关闭/最小化隐藏到系统托盘, 托盘右键菜单打开/退出。
package main

import (
	"context"
	"log"
	"sync/atomic"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"

	"docshare/internal/api"
	"docshare/internal/tray"
)

const singleInstanceMutex = "Local\\DocShare_SingleInstance"

var (
	trayInst *tray.Tray
	quitting atomic.Bool
)

func main() {
	// 单实例检测: 已有实例运行时直接提示退出
	h, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr(singleInstanceMutex))
	if err == windows.ERROR_ALREADY_EXISTS {
		_, _ = windows.MessageBox(0,
			windows.StringToUTF16Ptr("DocShare 已在运行中。\n如需退出, 请右键系统托盘图标选择「退出」。"),
			windows.StringToUTF16Ptr("DocShare"),
			windows.MB_OK|windows.MB_ICONINFORMATION|windows.MB_TOPMOST)
		return
	}
	if err != nil {
		log.Printf("[警告] 单实例锁获取失败: %v", err)
	} else if h != 0 {
		defer windows.CloseHandle(h)
	}

	app := NewApp()

	err = wails.Run(&options.App{
		Title:     "DocShare · MD 文档中心",
		Width:     1280,
		Height:    860,
		MinWidth:  960,
		MinHeight: 640,
		Assets:    api.WebFS,
		OnStartup: func(ctx context.Context) {
			app.startup(ctx)
		},
		OnDomReady: func(ctx context.Context) {
			// DOM 就绪后窗口必然已创建, 启动托盘更可靠
			startTray()
		},
		OnShutdown: func(ctx context.Context) {
			app.shutdown(ctx)
			if trayInst != nil {
				trayInst.Stop()
			}
		},
		OnBeforeClose: func(ctx context.Context) bool {
			if quitting.Load() {
				return false // 托盘"退出": 允许关闭
			}
			// 点击关闭按钮 → 隐藏到托盘(服务继续运行)
			runtime.WindowHide(ctx)
			if trayInst != nil {
				trayInst.NotifyFirst()
			}
			return true
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatalf("桌面应用启动失败: %v", err)
	}
}

// startTray 启动系统托盘(查找主窗口 + 子类化 + 图标)。
func startTray() {
	t, err := tray.Start("wailsWindow", func() {
		quitting.Store(true)
		if trayInst != nil {
			trayInst.Quit()
		}
	})
	if err != nil {
		log.Printf("[警告] 托盘启动失败: %v", err)
		return
	}
	trayInst = t
	log.Printf("托盘已就绪")
}
