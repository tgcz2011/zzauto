// Command zzauto 是多层 agent 协作的 AI 自主编程平台主入口。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/tgcz2011/zzauto/internal/aicli"
	"github.com/tgcz2011/zzauto/internal/config"
	"github.com/tgcz2011/zzauto/internal/daemon"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/ghcli"
	"github.com/tgcz2011/zzauto/internal/installer"
	"github.com/tgcz2011/zzauto/internal/projects"
	"github.com/tgcz2011/zzauto/internal/ui"
)

// Version zzauto 版本号。
const Version = "v0.4.0"

func main() {
	log.SetFlags(0)

	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		return
	}

	switch args[0] {
	case "serve":
		runServe(args[1:])
	case "start":
		runStart(args[1:])
	case "stop":
		runStop(args[1:])
	case "restart":
		runRestart(args[1:])
	case "status":
		runStatus(args[1:])
	case "uninstall":
		runUninstall(args[1:])
	case "upgrade":
		runUpgrade(args[1:])
	case "version":
		runVersion(args[1:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n", args[0])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `zzauto %s - 多层 agent 协作的 AI 自主编程平台

用法:
  zzauto [command]

命令:
  serve       前台启动 HTTP 服务（开发调试）
  start       后台启动 daemon（terminal 可关闭）
  stop        停止后台 daemon
  restart     重启后台 daemon
  status      查看 daemon 状态
  upgrade     从 GitHub releases 升级
  uninstall   移除二进制与配置（保留项目数据）
  version     打印版本号

无参数等同 -h。daemon 日志: ~/.zzauto/zzauto.log
`, Version)
}

func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	listen := fs.String("listen", "", "监听地址（覆盖配置）")
	noAutoInstall := fs.Bool("no-auto-install", false, "aiclibridge 不可达时不自动安装，仅提示并退出")
	_ = fs.Parse(args)

	serveArgs := []string{}
	if *listen != "" {
		serveArgs = append(serveArgs, "--listen", *listen)
	}
	if *noAutoInstall {
		serveArgs = append(serveArgs, "--no-auto-install")
	}

	if err := daemon.Start(serveArgs); err != nil {
		fmt.Fprintf(os.Stderr, "启动 daemon 失败: %v\n", err)
		os.Exit(1)
	}
}

func runStop(args []string) {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	_ = fs.Parse(args)
	if err := daemon.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "停止 daemon 失败: %v\n", err)
		os.Exit(1)
	}
}

func runRestart(args []string) {
	fs := flag.NewFlagSet("restart", flag.ExitOnError)
	listen := fs.String("listen", "", "监听地址（覆盖配置）")
	noAutoInstall := fs.Bool("no-auto-install", false, "aiclibridge 不可达时不自动安装，仅提示并退出")
	_ = fs.Parse(args)

	serveArgs := []string{}
	if *listen != "" {
		serveArgs = append(serveArgs, "--listen", *listen)
	}
	if *noAutoInstall {
		serveArgs = append(serveArgs, "--no-auto-install")
	}

	if err := daemon.Restart(serveArgs); err != nil {
		fmt.Fprintf(os.Stderr, "重启 daemon 失败: %v\n", err)
		os.Exit(1)
	}
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	_ = fs.Parse(args)
	running, pid, listen, err := daemon.Status()
	if err != nil {
		fmt.Fprintf(os.Stderr, "查询状态失败: %v\n", err)
		os.Exit(1)
	}
	if !running {
		fmt.Println("daemon 未运行")
		return
	}
	fmt.Printf("daemon 运行中 (PID=%d)\n", pid)
	if listen != "" {
		fmt.Printf("监听: %s\n", listen)
	}
	fmt.Println("日志: ~/.zzauto/zzauto.log")
}

func runVersion(args []string) {
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	_ = fs.Parse(args)
	fmt.Println(Version)
}

func runUninstall(args []string) {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	_ = fs.Parse(args)
	if err := installer.Uninstall(); err != nil {
		fmt.Fprintf(os.Stderr, "卸载失败: %v\n", err)
		os.Exit(1)
	}
}

func runUpgrade(args []string) {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	_ = fs.Parse(args)
	installer.CurrentVersion = Version
	if err := installer.Upgrade(); err != nil {
		fmt.Fprintf(os.Stderr, "升级失败: %v\n", err)
		os.Exit(1)
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := fs.String("listen", "", "监听地址（覆盖配置）")
	noAutoInstall := fs.Bool("no-auto-install", false, "aiclibridge 不可达时不自动安装，仅提示并退出")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}

	// 创建事件总线
	bus := eventbus.New()

	// 创建项目注册表（多项目管理）
	reg := projects.New(cfg.WorkspaceDir)

	// 检查 aiclibridge 可达性，不可达时按需自动安装
	aiClient := aicli.New(cfg.AicliAddr, cfg.AicliKey)
	healthCtx, healthCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := aiClient.Health(healthCtx); err != nil {
		healthCancel()
		log.Printf("aiclibridge 不可达: %v", err)
		if *noAutoInstall {
			fmt.Fprintln(os.Stderr, "请先安装并启动 aiclibridge：")
			fmt.Fprintln(os.Stderr, "  curl -fsSL https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.sh | sh")
			os.Exit(1)
		}
		// 自动安装
		log.Println("正在自动安装 aiclibridge...")
		installCtx, installCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := aicli.EnsureInstalled(installCtx, cfg.AicliAddr, cfg.AicliKey); err != nil {
			installCancel()
			fmt.Fprintf(os.Stderr, "aiclibridge 自动安装失败: %v\n", err)
			fmt.Fprintln(os.Stderr, "请手动安装：")
			fmt.Fprintln(os.Stderr, "  curl -fsSL https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.sh | sh")
			os.Exit(1)
		}
		installCancel()
		log.Println("aiclibridge 安装完成，继续启动")
	} else {
		healthCancel()
	}

	// gh CLI 检查：未安装则打印平台安装提示并退出
	if err := ghcli.EnsureInstalled(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	// gh auth 检查：未登录则打印登录提示并退出
	ghCheckCtx, ghCancel := context.WithTimeout(context.Background(), 10*time.Second)
	loggedIn, err := ghcli.AuthStatus(ghCheckCtx)
	ghCancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "检查 gh 登录状态失败: %v\n", err)
		os.Exit(1)
	}
	if !loggedIn {
		fmt.Fprintln(os.Stderr, ghcli.LoginHint())
		os.Exit(1)
	}

	// 创建 UI handler（orchestrator 按需在 handleStartProject 中装配）
	handler := ui.New(bus, reg, cfg)

	// 注册 HTTP 路由
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "zzauto running")
	})
	handler.Register(mux)

	// 启动 HTTP 服务
	log.Printf("zzauto %s 监听 %s", Version, cfg.Listen)
	if err := http.ListenAndServe(cfg.Listen, mux); err != nil {
		log.Fatalf("HTTP 服务退出: %v", err)
	}
}
