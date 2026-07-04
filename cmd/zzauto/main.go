// Command zzauto 是多层 agent 协作的 AI 自主编程平台主入口。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tgcz2011/zzauto/internal/agents"
	"github.com/tgcz2011/zzauto/internal/aicli"
	"github.com/tgcz2011/zzauto/internal/config"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/installer"
	"github.com/tgcz2011/zzauto/internal/registry"
	"github.com/tgcz2011/zzauto/internal/ui"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// Version zzauto 版本号。
const Version = "v0.2.0"

func main() {
	log.SetFlags(0)

	args := os.Args[1:]
	if len(args) == 0 {
		runServe(args)
		return
	}

	switch args[0] {
	case "serve":
		runServe(args[1:])
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
  serve       启动 HTTP 服务与编排器（默认，--no-auto-install 禁用自动安装）
  uninstall   移除二进制与配置（保留项目数据）
  upgrade     从 GitHub releases 升级
  version     打印版本号

默认（无子命令）等同于 serve。
`, Version)
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

	// 创建工作区并确保目录就绪
	ws := workspace.NewProject(cfg.WorkspaceDir)
	if err := ws.EnsureDirs(); err != nil {
		log.Fatalf("创建工作区目录失败: %v", err)
	}

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

	// 创建 UI handler，适配 AskUser 为 agents.AskFunc 签名
	handler := ui.New(bus, ws, cfg)
	askFunc := agents.AskFunc(func(ctx context.Context, question string) (string, error) {
		return handler.AskUser(question)
	})

	// 装配编排器
	orch, err := registry.BuildOrchestrator(cfg, ws, bus, askFunc)
	if err != nil {
		log.Fatalf("装配编排器失败: %v", err)
	}

	// 注册 HTTP 路由
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "zzauto running")
	})
	handler.Register(mux)

	// 启动编排器（后台 goroutine，监听 SIGINT/SIGTERM 取消 context）
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := orch.Run(runCtx); err != nil {
			log.Printf("编排器退出: %v", err)
		}
	}()

	// 启动 HTTP 服务
	log.Printf("zzauto %s 监听 %s", Version, cfg.Listen)
	if err := http.ListenAndServe(cfg.Listen, mux); err != nil {
		log.Fatalf("HTTP 服务退出: %v", err)
	}
}
