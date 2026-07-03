// Package ui 提供 zzauto 的 Web 界面：embed 静态资源 + HTTP API + SSE 推送。
//
// 静态资源（index.html / app.js / style.css）在编译期通过 go:embed 打包进
// 二进制，运行时由 Handler 提供 HTTP 服务，无需额外的静态资源目录。
package ui

import (
	"embed"
	"io/fs"
)

// embeddedFiles 内嵌 web/ 下的全部静态资源。
//
//go:embed web/*
var embeddedFiles embed.FS

// assets 返回静态资源文件系统，根目录对应 web/。
// 供 http.FileServer 直接使用。
func assets() fs.FS {
	sub, err := fs.Sub(embeddedFiles, "web")
	if err != nil {
		// 理论上不会失败：web/ 在编译期已嵌入。
		return embeddedFiles
	}
	return sub
}
