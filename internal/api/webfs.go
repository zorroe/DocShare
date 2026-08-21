package api

import (
	"embed"
	"io/fs"
)

//go:embed all:web
var webFS embed.FS

// WebFS 内嵌前端静态资源(单文件分发核心), 根目录即前端页面根。
var WebFS fs.FS = func() fs.FS {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	return sub
}()
