//go:build !windows

// 非 Windows 平台: 无目录变更监听, Tree() 始终全量扫描(回退行为)。
package store

type dirWatcher struct{}

func newDirWatcher(root string, onEvent func()) *dirWatcher { return nil }
func (w *dirWatcher) start()                                {}
func (w *dirWatcher) stop()                                 {}
func (w *dirWatcher) active() bool                          { return false }
