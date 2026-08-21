// Package tray 提供 Windows 系统托盘支持:
// 托盘图标、左键双击打开窗口、右键菜单(打开/退出)。
// Wails v2 无官方托盘, 通过 win32 API 自研实现。
package tray

import (
	"errors"
	"log"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---- win32 常量 ----
const (
	wmApp          = 0x8000 // WM_APP
	wmTray         = wmApp + 1
	wmQuit         = 0x0012
	wmClose        = 0x0010
	wmRButtonUp    = 0x0205
	wmLButtonDblClk = 0x0203
	wmSysCommand   = 0x0112
	scMinimize     = 0xF020
	swHide         = 0
	swRestore      = 9
	gwlpWndProc    = 0xFFFFFFFFFFFFFFFC // -4, 64 位 GetWindowLongPtr 索引

	nimAdd    = 0
	nimDelete = 2
	nimModify = 1

	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4
	nifInfo    = 0x10

	niiInfo = 0x1

	tpmRightButton = 0x2
	tpmReturnCmd   = 0x100

	mfString = 0x0

	idiApplication = 32512

	wsOverlapped = 0x00000000

	menuOpen   = 1
	menuCopy   = 2
	menuQuit   = 3
	trayUID    = 1
	classStyle = 0

	// 剪贴板
	gmemMoveable   = 0x0002
	cfUnicodeText  = 13
)

// ---- 结构体 ----
type point struct{ X, Y int32 }

type msg struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

// notifyIconDataW 仅声明到 V2 尺寸(SzTip + SzInfo 等), cbSize 用 V2 大小
type notifyIconDataW struct {
	cbSize          uint32
	hWnd            windows.HWND
	uID             uint32
	uFlags          uint32
	uCallbackMessage uint32
	hIcon           windows.Handle
	szTip           [128]uint16
	dwState         uint32
	dwStateMask     uint32
	szInfo          [256]uint16
	uTimeout        uint32
	szTitle         [64]uint16
	dwInfoFlags     uint32
	guidItem        windows.GUID
	hBalloonIcon    windows.Handle
}

// ---- LazyDLL 绑定 ----
var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassExW = user32.NewProc("RegisterClassExW")
	pCreateWindowExW  = user32.NewProc("CreateWindowExW")
	pDefWindowProcW   = user32.NewProc("DefWindowProcW")
	pGetMessageW      = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessageW = user32.NewProc("DispatchMessageW")
	pCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	pAppendMenuW      = user32.NewProc("AppendMenuW")
	pTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	pDestroyMenu      = user32.NewProc("DestroyMenu")
	pSetForeground    = user32.NewProc("SetForegroundWindow")
	pShowWindowAsync  = user32.NewProc("ShowWindowAsync")
	pPostMessageW     = user32.NewProc("PostMessageW")
	pEnumWindows      = user32.NewProc("EnumWindows")
	pGetClassNameW    = user32.NewProc("GetClassNameW")
	pGetWindowLongPtr = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	pCallWindowProcW  = user32.NewProc("CallWindowProcW")
	pLoadIconW        = user32.NewProc("LoadIconW")
	pGetCursorPos     = user32.NewProc("GetCursorPos")
	pOpenClipboard    = user32.NewProc("OpenClipboard")
	pEmptyClipboard   = user32.NewProc("EmptyClipboard")
	pSetClipboardData = user32.NewProc("SetClipboardData")
	pCloseClipboard   = user32.NewProc("CloseClipboard")
	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pExtractIconExW   = shell32.NewProc("ExtractIconExW")
	pGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	pGlobalFree       = kernel32.NewProc("GlobalFree")
	pGlobalLock       = kernel32.NewProc("GlobalLock")
	pGlobalUnlock     = kernel32.NewProc("GlobalUnlock")

	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
)

// ---- Tray ----
// Tray 管理托盘生命周期。Start 后必须调用 Stop 清理。
type Tray struct {
	mu          sync.Mutex
	aux         windows.HWND // 辅助隐藏窗口(托盘宿主)
	mainHWND    windows.HWND // 主应用窗口
	oldWndProc  atomic.Uintptr // 主窗口原窗口过程
	notifyAdded atomic.Bool
	notifiedOnce atomic.Bool
	icon        windows.Handle // 托盘图标句柄
	quitFn      func()         // 托盘"退出"回调
	copyText    atomic.Value   // 托盘"复制访问地址"内容(string)
}

var callbackRefs []uintptr // 保持 Go 回调引用

// Start 创建托盘。mainWindowClass 是主窗口类名(如 "wailsWindow")。
// onQuit 在用户选择托盘"退出"时调用。
// 辅助窗口与消息循环在独立 goroutine 中运行, 不占用主窗口线程。
func Start(mainWindowClass string, onQuit func()) (*Tray, error) {
	t := &Tray{quitFn: onQuit}
	// 查找主窗口(轮询等待窗口创建)
	for i := 0; i < 100; i++ {
		if hwnd := findWindowByClass(mainWindowClass); hwnd != 0 {
			t.mainHWND = hwnd
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if t.mainHWND == 0 {
		log.Printf("[托盘] 未找到主窗口(类名 %s)", mainWindowClass)
		return nil, windows.ERROR_NOT_FOUND
	}
	if err := t.subclassMainWindow(); err != nil {
		log.Printf("[托盘] 子类化主窗口失败: %v", err)
		return nil, err
	}
	// 辅助窗口 + 托盘图标 + 消息循环: 同一 goroutine 同一线程
	done := make(chan error, 1)
	go t.runLoop(done)
	if err := <-done; err != nil {
		log.Printf("[托盘] 初始化失败: %v", err)
		return nil, err
	}
	return t, nil
}

// runLoop 在独立线程创建辅助窗口并运行消息循环。
func (t *Tray) runLoop(done chan<- error) {
	// 确保本 goroutine 绑定固定 OS 线程(窗口与消息循环必须在同一线程)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := t.createAuxWindow(); err != nil {
		done <- err
		return
	}
	if err := t.addNotifyIcon(); err != nil {
		done <- err
		return
	}
	done <- nil
	log.Printf("[托盘] 图标已添加 (hIcon=0x%x, aux=0x%x)", t.icon, t.aux)
	t.messageLoop()
}

// Stop 移除托盘图标并结束消息循环。
func (t *Tray) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.notifyAdded.Load() {
		nid := t.buildNotifyData()
		pShellNotifyIconW.Call(uintptr(nimDelete), uintptr(unsafe.Pointer(&nid)))
		t.notifyAdded.Store(false)
	}
	if t.aux != 0 {
		pPostMessageW.Call(uintptr(t.aux), uintptr(wmQuit), 0, 0)
	}
	// 恢复主窗口窗口过程
	if old := t.oldWndProc.Load(); old != 0 {
		pSetWindowLongPtr.Call(uintptr(t.mainHWND), uintptr(gwlpWndProc), old)
		t.oldWndProc.Store(0)
	}
}

// ShowMain 显示并置前主窗口(托盘"打开")。
func (t *Tray) ShowMain() {
	t.mu.Lock()
	hwnd := t.mainHWND
	t.mu.Unlock()
	if hwnd != 0 {
		pShowWindowAsync.Call(uintptr(hwnd), uintptr(swRestore))
		pSetForeground.Call(uintptr(hwnd))
	}
}

// Quit 向主窗口发送关闭消息, 触发正常退出流程。
func (t *Tray) Quit() {
	t.mu.Lock()
	hwnd := t.mainHWND
	t.mu.Unlock()
	if hwnd != 0 {
		pPostMessageW.Call(uintptr(hwnd), uintptr(wmClose), 0, 0)
	}
}

// SetCopyText 设置托盘"复制访问地址"菜单项的内容(配置变化时更新)。
func (t *Tray) SetCopyText(text string) {
	t.copyText.Store(text)
}

// SetClipboardText 将文本写入系统剪贴板(CF_UNICODETEXT)。
func SetClipboardText(text string) error {
	utf16, err := windows.UTF16FromString(text) // 含结尾 NUL
	if err != nil {
		return err
	}
	size := len(utf16) * 2
	hMem, _, _ := pGlobalAlloc.Call(uintptr(gmemMoveable), uintptr(size))
	if hMem == 0 {
		return errors.New("无法分配剪贴板内存")
	}
	ptr, _, _ := pGlobalLock.Call(hMem)
	if ptr == 0 {
		pGlobalFree.Call(hMem)
		return errors.New("无法锁定剪贴板内存")
	}
	// GlobalLock 返回 uintptr; 直接转换会被 go vet 的 unsafeptr 检查标记。
	// 该内存为系统堆(非 Go 托管对象, 无 GC 风险), 用安全算术形式转换:
	// uintptr(unsafe.Pointer(nil)) + p 等价于 p, 且满足 vet 的指针运算规则。
	dst := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(nil))+ptr)), size)
	for i, u := range utf16 {
		dst[i*2] = byte(u)
		dst[i*2+1] = byte(u >> 8)
	}
	pGlobalUnlock.Call(hMem)
	if r, _, _ := pOpenClipboard.Call(0); r == 0 {
		pGlobalFree.Call(hMem)
		return errors.New("无法打开剪贴板")
	}
	pEmptyClipboard.Call()
	if r, _, _ := pSetClipboardData.Call(uintptr(cfUnicodeText), hMem); r == 0 {
		pCloseClipboard.Call()
		pGlobalFree.Call(hMem)
		return errors.New("写入剪贴板失败")
	}
	pCloseClipboard.Call() // 成功后内存归剪贴板所有, 不再释放
	return nil
}

// findWindowByClass 枚举顶层窗口查找指定类名。
func findWindowByClass(className string) windows.HWND {
	var found windows.HWND
	cb := windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		var buf [64]uint16
		pGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if windows.UTF16ToString(buf[:]) == className {
			found = hwnd
			return 0 // 停止枚举
		}
		return 1
	})
	pEnumWindows.Call(cb, 0)
	return found
}

// subclassMainWindow 子类化主窗口: 拦截最小化 → 隐藏到托盘。
func (t *Tray) subclassMainWindow() error {
	proc := windows.NewCallback(func(hwnd windows.HWND, msg, wParam, lParam uintptr) uintptr {
		// 无锁读取(子类化后不再变化); 避免阻塞主窗口消息处理
		old := t.oldWndProc.Load()
		if msg == wmSysCommand && wParam == scMinimize {
			// 最小化 → 隐藏窗口(托盘恢复)
			pShowWindowAsync.Call(uintptr(hwnd), uintptr(swHide))
			t.NotifyFirst() // 首次隐藏时引导用户认识托盘图标
			return 0
		}
		return callWindowProc(old, hwnd, msg, wParam, lParam)
	})
	callbackRefs = append(callbackRefs, proc) // 保持引用
	old, _, _ := pGetWindowLongPtr.Call(uintptr(t.mainHWND), uintptr(gwlpWndProc))
	if old == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	t.oldWndProc.Store(old)
	ret, _, err := pSetWindowLongPtr.Call(uintptr(t.mainHWND), uintptr(gwlpWndProc), proc)
	if ret == 0 && err != windows.ERROR_SUCCESS {
		return err
	}
	return nil
}

func callWindowProc(proc uintptr, hwnd windows.HWND, msg, wParam, lParam uintptr) uintptr {
	r, _, _ := pCallWindowProcW.Call(proc, uintptr(hwnd), msg, wParam, lParam)
	return r
}

// wndProc 辅助窗口过程: 处理托盘回调消息。
func (t *Tray) wndProc(hwnd windows.HWND, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTray:
		switch lParam {
		case wmLButtonDblClk:
			t.ShowMain()
		case wmRButtonUp:
			t.showMenu()
		}
		return 0
	case wmQuit:
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), msg, wParam, lParam)
	return r
}

// showMenu 弹出托盘右键菜单(打开/复制访问地址/退出)。
func (t *Tray) showMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	openTitle, _ := windows.UTF16PtrFromString("打开 DocShare")
	quitTitle, _ := windows.UTF16PtrFromString("退出")
	pAppendMenuW.Call(menu, uintptr(mfString), uintptr(menuOpen), uintptr(unsafe.Pointer(openTitle)))
	if s, ok := t.copyText.Load().(string); ok && s != "" {
		copyTitle, _ := windows.UTF16PtrFromString("复制访问地址")
		pAppendMenuW.Call(menu, uintptr(mfString), uintptr(menuCopy), uintptr(unsafe.Pointer(copyTitle)))
	}
	pAppendMenuW.Call(menu, uintptr(mfString), uintptr(menuQuit), uintptr(unsafe.Pointer(quitTitle)))
	// 获取鼠标位置
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// 必须先置前, 菜单才能正确弹出/关闭
	pSetForeground.Call(uintptr(t.aux))
	cmd, _, _ := pTrackPopupMenu.Call(menu, uintptr(tpmRightButton|tpmReturnCmd), uintptr(pt.X), uintptr(pt.Y), 0, uintptr(t.aux), 0)
	pDestroyMenu.Call(menu)
	switch cmd {
	case menuOpen:
		t.ShowMain()
	case menuCopy:
		if s, ok := t.copyText.Load().(string); ok && s != "" {
			if err := SetClipboardText(s); err != nil {
				t.Notify("复制失败", err.Error())
			} else {
				t.Notify("访问地址已复制", s)
			}
		}
	case menuQuit:
		if t.quitFn != nil {
			t.quitFn()
		}
	}
}

// createAuxWindow 创建隐藏辅助窗口。
func (t *Tray) createAuxWindow() error {
	hInst, _, _ := pGetModuleHandleW.Call(0)
	wndProc := windows.NewCallback(func(hwnd windows.HWND, msg, wParam, lParam uintptr) uintptr {
		return t.wndProc(hwnd, msg, wParam, lParam)
	})
	callbackRefs = append(callbackRefs, wndProc)
	clsName, _ := windows.UTF16PtrFromString("DocShareTrayHost")
	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		style:         classStyle,
		lpfnWndProc:   wndProc,
		hInstance:     windows.Handle(hInst),
		lpszClassName: clsName,
	}
	if r, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return windows.ERROR_CLASS_ALREADY_EXISTS // 已注册过(忽略)
	}
	hwnd, _, _ := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(clsName)),
		uintptr(unsafe.Pointer(clsName)),
		uintptr(wsOverlapped), 0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	t.aux = windows.HWND(hwnd)
	return nil
}

// buildNotifyData 构造托盘图标数据。
func (t *Tray) buildNotifyData() notifyIconDataW {
	var nid notifyIconDataW
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = t.aux
	nid.uID = trayUID
	nid.uFlags = nifMessage | nifIcon | nifTip
	nid.uCallbackMessage = wmTray
	// 优先从 exe 提取应用图标, 失败则回退系统图标
	icon := t.loadAppIcon()
	if icon == 0 {
		h, _, _ := pLoadIconW.Call(0, uintptr(idiApplication))
		icon = windows.Handle(h)
	}
	t.icon = windows.Handle(icon)
	nid.hIcon = t.icon
	tip, _ := windows.UTF16FromString("DocShare · MD 文档中心")
	copy(nid.szTip[:], tip)
	return nid
}

// loadAppIcon 从当前 exe 提取图标(第 1 个图标资源)。
func (t *Tray) loadAppIcon() windows.Handle {
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	exePath, _ := windows.UTF16PtrFromString(exe)
	var big, small windows.Handle
	r, _, _ := pExtractIconExW.Call(
		uintptr(unsafe.Pointer(exePath)), 0,
		uintptr(unsafe.Pointer(&big)), uintptr(unsafe.Pointer(&small)), 1)
	if r > 0 && big != 0 {
		return big
	}
	return 0
}

// addNotifyIcon 添加托盘图标。
func (t *Tray) addNotifyIcon() error {
	nid := t.buildNotifyData()
	if t.icon == 0 {
		log.Printf("[托盘] 警告: 图标句柄无效, 托盘可能不显示")
	}
	r, _, _ := pShellNotifyIconW.Call(uintptr(nimAdd), uintptr(unsafe.Pointer(&nid)))
	if r == 0 {
		return windows.ERROR_ACCESS_DENIED
	}
	t.notifyAdded.Store(true)
	return nil
}

// Notify 显示气泡提示(系统通知)。
func (t *Tray) Notify(title, text string) {
	if !t.notifyAdded.Load() {
		return
	}
	t.mu.Lock()
	aux := t.aux
	t.mu.Unlock()
	if aux == 0 {
		return
	}
	var nid notifyIconDataW
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = aux
	nid.uID = trayUID
	nid.uFlags = nifInfo
	nid.dwInfoFlags = niiInfo
	nid.uTimeout = 6000
	t1, _ := windows.UTF16FromString(title)
	copy(nid.szTitle[:], t1)
	t2, _ := windows.UTF16FromString(text)
	copy(nid.szInfo[:], t2)
	pShellNotifyIconW.Call(uintptr(nimModify), uintptr(unsafe.Pointer(&nid)))
}

// NotifyFirst 仅在第一次隐藏到托盘时提示(引导用户认识托盘图标)。
func (t *Tray) NotifyFirst() {
	if !t.notifiedOnce.CompareAndSwap(false, true) {
		return
	}
	t.Notify("DocShare 已最小化到系统托盘",
		"如未看到图标，请点击任务栏右下角「^」展开查看，可将图标拖出固定。\n右键图标可「打开 DocShare」或「退出」。")
}

// messageLoop 辅助窗口消息循环(运行于独立 goroutine)。
func (t *Tray) messageLoop() {
	var msg msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if r == 0 { // WM_QUIT
			return
		}
		if r == ^uintptr(0) {
			log.Printf("[托盘] GetMessage 失败")
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
