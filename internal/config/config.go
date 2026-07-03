// Package config 负责 zzauto 的配置加载。
//
// 加载顺序：先读 ./zzauto.yaml，再用 ZZAUTO_* 环境变量覆盖。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// GithubConfig git 远程仓库配置。
type GithubConfig struct {
	Remote string `yaml:"remote"`
	Branch string `yaml:"branch"`
	Token  string `yaml:"token"`
}

// Config zzauto 运行配置。
type Config struct {
	Listen       string       `yaml:"listen"`
	AicliAddr    string       `yaml:"aicli_addr"`
	AicliKey     string       `yaml:"aicli_key"`
	WorkspaceDir string       `yaml:"workspace_dir"`
	Github       GithubConfig `yaml:"github"`
}

// Default 返回默认配置。
func Default() *Config {
	return &Config{
		Listen:       "127.0.0.1:8788",
		AicliAddr:    "127.0.0.1:8787",
		AicliKey:     "",
		WorkspaceDir: "./workspace",
		Github:       GithubConfig{},
	}
}

// Load 加载配置：先读 ./zzauto.yaml，再用 ZZAUTO_* 环境变量覆盖。
func Load() (*Config, error) {
	cfg := Default()

	if data, err := os.ReadFile("zzauto.yaml"); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("解析 zzauto.yaml 失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取 zzauto.yaml 失败: %w", err)
	}

	applyEnv(cfg)
	return cfg, nil
}

// applyEnv 用 ZZAUTO_* 环境变量覆盖配置。
func applyEnv(cfg *Config) {
	if v := os.Getenv("ZZAUTO_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("ZZAUTO_AICLI_ADDR"); v != "" {
		cfg.AicliAddr = v
	}
	if v := os.Getenv("ZZAUTO_AICLI_KEY"); v != "" {
		cfg.AicliKey = v
	}
	if v := os.Getenv("ZZAUTO_WORKSPACE_DIR"); v != "" {
		cfg.WorkspaceDir = v
	}
	if v := os.Getenv("ZZAUTO_GITHUB_REMOTE"); v != "" {
		cfg.Github.Remote = v
	}
	if v := os.Getenv("ZZAUTO_GITHUB_BRANCH"); v != "" {
		cfg.Github.Branch = v
	}
	if v := os.Getenv("ZZAUTO_GITHUB_TOKEN"); v != "" {
		cfg.Github.Token = v
	}
}
