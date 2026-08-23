//go:build windows

// 目录变更监听: 基于 ReadDirectoryChangesW(每目录一个句柄, 重叠 I/O + 事件驱动),
// 任何文件/目录增删改都会触发 onEvent 回调(置脏树缓存)。
// 新出现的子目录会被动态补挂监听; 被删除的目录自动移除。
// 停止时通过共享停止事件唤醒所有监听 goroutine, 可干净退出(不阻塞 CloseHandle)。
package store

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// watchMask 关注的变更类型: 文件名/目录名/大小/最后写入。
const watchMask = windows.FILE_NOTIFY_CHANGE_FILE_NAME |
	windows.FILE_NOTIFY_CHANGE_DIR_NAME |
	windows.FILE_NOTIFY_CHANGE_SIZE |
	windows.FILE_NOTIFY_CHANGE_LAST_WRITE

// dirWatcher 监听一个文档根目录的整棵目录树。
type dirWatcher struct {
	root      string
	onEvent   func() // 任意变更回调(置脏)
	stopEvent windows.Handle

	mu      sync.Mutex
	dirs    map[string]windows.Handle // 已监听目录 -> 句柄
	wg      sync.WaitGroup            // 所有监听 goroutine
	started atomic.Bool
	stopped atomic.Bool
}

func newDirWatcher(root string, onEvent func()) *dirWatcher {
	ev, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil
	}
	return &dirWatcher{
		root:      root,
		onEvent:   onEvent,
		stopEvent: ev,
		dirs:      map[string]windows.Handle{},
	}
}

// active 是否正在监听。
func (w *dirWatcher) active() bool { return w != nil && w.started.Load() && !w.stopped.Load() }

// start 递归挂监听(幂等)。已存在的目录逐一打开句柄。
func (w *dirWatcher) start() {
	if w == nil || !w.started.CompareAndSwap(false, true) {
		return
	}
	filepath.WalkDir(w.root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 根目录可能尚不可用, 跳过
		}
		if d.IsDir() {
			w.addDir(p)
		}
		return nil
	})
}

// stop 唤醒并等待所有监听 goroutine 退出, 然后关闭全部句柄(幂等)。
// 注意: 必须在持有 w.mu 时取消+关闭句柄, 且监听 goroutine 也在持锁时发起
// ReadDirectoryChanges —— 保证不会出现「关闭后又有新的挂起操作」的竞态。
func (w *dirWatcher) stop() {
	if w == nil || !w.stopped.CompareAndSwap(false, true) {
		return
	}
	w.mu.Lock()
	_ = windows.SetEvent(w.stopEvent) // 唤醒所有阻塞中的监听
	for _, h := range w.dirs {
		_ = windows.CancelIoEx(h, nil) // 先取消挂起的重叠操作, CloseHandle 才不会阻塞
		_ = windows.CloseHandle(h)
	}
	w.dirs = map[string]windows.Handle{}
	w.mu.Unlock()
	w.wg.Wait()
	_ = windows.CloseHandle(w.stopEvent)
}

// markDirty 事件回调。
func (w *dirWatcher) markDirty() {
	if w.onEvent != nil {
		w.onEvent()
	}
}

// addDir 为目录挂监听(幂等)。
func (w *dirWatcher) addDir(dir string) {
	if w.stopped.Load() {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.dirs[dir]; ok {
		return
	}
	up, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return
	}
	h, err := windows.CreateFile(up,
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return
	}
	w.dirs[dir] = h
	w.wg.Add(1)
	go w.watchDir(dir, h)
}

// removeDir 移除目录监听并关闭句柄(目录被删除/移动时)。
func (w *dirWatcher) removeDir(dir string) {
	w.mu.Lock()
	h, ok := w.dirs[dir]
	if ok {
		delete(w.dirs, dir)
		_ = windows.CancelIoEx(h, nil)
		_ = windows.CloseHandle(h)
	}
	w.mu.Unlock()
}

// watchDir 单个目录的事件驱动监听循环(重叠 I/O + 数据事件/停止事件)。
func (w *dirWatcher) watchDir(dir string, h windows.Handle) {
	defer w.wg.Done()
	dataEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		w.removeDir(dir)
		return
	}
	defer windows.CloseHandle(dataEvent)
	buf := make([]byte, 64*1024)
	ov := &windows.Overlapped{HEvent: dataEvent}
	for {
		if w.stopped.Load() {
			return
		}
		_ = windows.ResetEvent(dataEvent)
		// 持锁发起监听, 与 stop()/removeDir() 的取消+关闭互斥, 避免新挂起操作竞态
		w.mu.Lock()
		err := windows.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)),
			false, watchMask, nil, ov, 0)
		w.mu.Unlock()
		if err != nil && err != windows.ERROR_IO_PENDING {
			// 句柄已关闭(stop)或目录已删除
			w.removeDir(dir)
			return
		}
		idx, werr := windows.WaitForMultipleObjects(
			[]windows.Handle{dataEvent, w.stopEvent}, false, windows.INFINITE)
		if werr != nil || idx == 1 {
			return // 停止
		}
		var ret uint32
		if err := windows.GetOverlappedResult(h, ov, &ret, false); err != nil {
			// 缓冲区溢出(ERROR_NOTIFY_ENUM_DIR)等: 置脏, 重新挂起监听
			w.markDirty()
			continue
		}
		if ret == 0 {
			continue
		}
		w.markDirty()
		w.parseEvents(buf[:ret], dir)
	}
}

// parseEvents 解析 FILE_NOTIFY_INFORMATION 事件流, 动态维护子目录监听。
func (w *dirWatcher) parseEvents(buf []byte, dir string) {
	off := 0
	for off+12 <= len(buf) {
		next := int(binary.LittleEndian.Uint32(buf[off:]))
		action := binary.LittleEndian.Uint32(buf[off+4:])
		nameLen := int(binary.LittleEndian.Uint32(buf[off+8:]))
		if nameLen == 0 || off+12+nameLen > len(buf) {
			break
		}
		nameBytes := buf[off+12 : off+12+nameLen]
		name := windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(&nameBytes[0])), nameLen/2))
		full := filepath.Join(dir, name)
		switch action {
		case windows.FILE_ACTION_ADDED, windows.FILE_ACTION_RENAMED_NEW_NAME:
			if fi, err := os.Stat(full); err == nil && fi.IsDir() {
				w.addDir(full) // 新目录补挂监听
			}
		case windows.FILE_ACTION_REMOVED, windows.FILE_ACTION_RENAMED_OLD_NAME:
			w.removeDir(full) // 目录被删/改名: 移除其监听
		}
		if next == 0 {
			break
		}
		off += next
	}
}
