// Package config 负责 zzauto 的配置加载。
//
// 加载顺序：先读 ./zzauto.yaml，再用 ZZAUTO_* 环境变量覆盖。
package config

import (
	"fmt"
	"os"
	"strings"

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
	Listen       string            `yaml:"listen"`
	AicliAddr    string            `yaml:"aicli_addr"`
	AicliKey     string            `yaml:"aicli_key"`
	WorkspaceDir string            `yaml:"workspace_dir"`
	Github       GithubConfig      `yaml:"github"`
	RoleModels   map[string]string `yaml:"role_models"`
}

// Default 返回默认配置。
func Default() *Config {
	return &Config{
		Listen:       "127.0.0.1:8788",
		AicliAddr:    "127.0.0.1:8787",
		AicliKey:     "",
		WorkspaceDir: "./workspace",
		Github:       GithubConfig{},
		RoleModels:   map[string]string{},
	}
}

// Load 加载配置：先读 ./zzauto.yaml，再用 ZZAUTO_* 环境变量覆盖。
func Load() (*Config, error) {
	return LoadFrom("zzauto.yaml")
}

// LoadFrom 从指定路径加载配置：先读文件，再用 ZZAUTO_* 环境变量覆盖。
func LoadFrom(path string) (*Config, error) {
	cfg := Default()

	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}

	applyEnv(cfg)
	return cfg, nil
}

// Save 将配置序列化为 yaml 写入指定路径，权限 0o644。
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", path, err)
	}
	return nil
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

	// 解析 ZZAUTO_ROLE_MODEL_<STAGE> 形式的环境变量，
	// key 由大写转小写，例如 ZZAUTO_ROLE_MODEL_LISTENER=foo -> cfg.RoleModels["listener"] = "foo"。
	const prefix = "ZZAUTO_ROLE_MODEL_"
	for _, env := range os.Environ() {
		k, v, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		stage := strings.ToLower(strings.TrimPrefix(k, prefix))
		if stage == "" {
			continue
		}
		if cfg.RoleModels == nil {
			cfg.RoleModels = map[string]string{}
		}
		cfg.RoleModels[stage] = v
	}
}
