// Package config uses YAML format to define local→remote mappings.
// 使用 YAML 格式，定义多组本地→远程映射。
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// SyncMode defines the synchronization strategy.
// SyncMode 定义同步策略。
type SyncMode string

const (
	// SyncOverlayPatch: incremental overlay — upload new/modified files, preserve remote-only files.
	// 增量覆盖模式：仅上传新增/修改文件，保留远端独有文件。
	SyncOverlayPatch SyncMode = "overlay"
	// SyncFullReplace: tar.gz full replace — pack, upload, extract, clean remote orphans.
	// 压缩全量替换模式：本地打包上传，远端解压覆盖，清理冗余文件。
	SyncFullReplace SyncMode = "full_replace"
)

// AuthType defines the SSH authentication method.
// AuthType 定义 SSH 认证方式。
type AuthType string

const (
	// AuthAuto: auto-detect (try key first, then password).
	// 自动探测（优先私钥，其次密码）。
	AuthAuto AuthType = "auto"
	// AuthPassword: password authentication only.
	// 仅密码认证。
	AuthPassword AuthType = "password"
	// AuthPrivateKey: private key file authentication only.
	// 仅私钥文件认证。
	AuthPrivateKey AuthType = "private_key"
)

// RetryConfig holds retry policy for transient failures.
// RetryConfig 瞬态失败重试策略。
type RetryConfig struct {
	MaxRetries int `yaml:"max_retries"` // max retry attempts / 最大重试次数
	DelayMs    int `yaml:"delay_ms"`    // delay between retries in milliseconds / 重试间隔(毫秒)
}

type Task struct {
	Name    string  `yaml:"name"`
	Source  string  `yaml:"source"`
	Target  string  `yaml:"target"`
	Options Options `yaml:"options"`
	Hooks   Hooks   `yaml:"hooks,omitempty"` // remote command hooks / 远端命令钩子
}

// HookErrorPolicy defines behavior when a hook command fails.
// HookErrorPolicy 定义钩子命令失败时的行为。
type HookErrorPolicy string

const (
	// HookAbort: abort the sync task on hook failure (default).
	// 钩子失败时中止同步任务（默认）。
	HookAbort HookErrorPolicy = "abort"
	// HookWarn: print a warning but continue sync.
	// 仅警告，继续同步。
	HookWarn HookErrorPolicy = "warn"
	// HookIgnore: silently ignore hook failures.
	// 静默忽略钩子失败。
	HookIgnore HookErrorPolicy = "ignore"
)

// Hooks defines remote commands to execute before/after sync.
// Hooks 定义同步前后执行的远端命令。
type Hooks struct {
	Before  []string        `yaml:"before,omitempty"`  // commands before sync / 同步前命令
	After   []string        `yaml:"after,omitempty"`   // commands after sync / 同步后命令
	OnError HookErrorPolicy `yaml:"on_error,omitempty"` // abort / warn / ignore (default abort)
}

// HasHooks returns true if any hooks are configured.
func (h Hooks) HasHooks() bool {
	return len(h.Before) > 0 || len(h.After) > 0
}

// Options represents sync options for a task.
// Options 同步选项。
type Options struct {
	SyncMode    SyncMode `yaml:"sync_mode"`   // overlay (default) or full_replace / 同步模式
	Delete      bool     `yaml:"delete"`      // delete extra files on target (overlay mode) / 删除目标多余文件
	Exclude     []string `yaml:"exclude"`     // exclude file patterns / 排除文件模式
	Checksum    bool     `yaml:"checksum"`    // use checksum to detect changes / 用校验和判断差异
	Flat        bool     `yaml:"flat"`        // map content directly, no source folder wrapping / 直接映射不套源文件夹
	ShowDots    bool     `yaml:"show_dots"`   // show hidden files/dirs (starting with .) / 显示.开头的隐藏文件
	Incremental *bool    `yaml:"incremental"` // incremental toggle; nil=use global default / 增量开关；nil=使用全局默认
}

// IsIncremental returns whether incremental mode is enabled for this task.
// Falls back to the global default when the task-level field is nil.
// IsIncremental 返回该任务是否启用增量模式。任务级为 nil 时回退全局默认。
func (o Options) IsIncremental(globalDefault bool) bool {
	if o.Incremental != nil {
		return *o.Incremental
	}
	return globalDefault
}

type Server struct {
	Name     string   `yaml:"name"`
	Host     string   `yaml:"host"`
	Port     int      `yaml:"port"`
	User     string   `yaml:"user"`
	AuthType AuthType `yaml:"auth_type,omitempty"` // auto / password / private_key
	KeyFile  string   `yaml:"key_file"`            // SSH private key path / SSH 私钥路径
	Pass     string   `yaml:"password"`            // or password (not recommended) / 或密码（不推荐）
	Protect  []string `yaml:"protect,omitempty"`   // protect patterns: remote files never overwritten/deleted / 保护模式
}

// Config is the top-level configuration.
// Config 顶层配置。
type Config struct {
	Version     string      `yaml:"version"`
	Language    string      `yaml:"language,omitempty"`    // "en" or "zh"
	Checksum    string      `yaml:"checksum,omitempty"`    // default checksum algo
	Workers     int         `yaml:"workers,omitempty"`     // delta parallel workers / delta并行数
	Incremental *bool       `yaml:"incremental,omitempty"` // global incremental default (true) / 全局增量默认
	Retry       RetryConfig `yaml:"retry,omitempty"`       // retry policy / 重试策略
	Servers     []Server    `yaml:"servers"`
	Tasks       []Task      `yaml:"tasks"`
}

// IsIncremental returns the global incremental default (true if unset).
// IsIncremental 返回全局增量默认值（未设置时为 true）。
func (c *Config) IsIncremental() bool {
	if c.Incremental != nil {
		return *c.Incremental
	}
	return true
}

// GetRetry returns the retry config with sane defaults.
// GetRetry 返回带默认值的重试配置。
func (c *Config) GetRetry() RetryConfig {
	r := c.Retry
	if r.MaxRetries <= 0 {
		r.MaxRetries = 3
	}
	if r.DelayMs <= 0 {
		r.DelayMs = 1000
	}
	return r
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config failed / 读取配置失败: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config failed / 解析配置失败: %w", err)
	}

	return &cfg, nil
}

func (c *Config) Save(path string) error {
	if c.Version == "" {
		c.Version = "1.0"
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("serialize config failed / 序列化配置失败: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func (c *Config) Validate() error {
	for i, t := range c.Tasks {
		if t.Name == "" {
			return fmt.Errorf("task #%d missing name / 任务 #%d 缺少名称", i+1, i+1)
		}
		if t.Source == "" {
			return fmt.Errorf("task '%s' missing source / 任务 '%s' 缺少 source", t.Name, t.Name)
		}
		if t.Target == "" {
			return fmt.Errorf("task '%s' missing target / 任务 '%s' 缺少 target", t.Name, t.Name)
		}
	}
	return nil
}

func (c *Config) GetTask(name string) *Task {
	for i := range c.Tasks {
		if c.Tasks[i].Name == name {
			return &c.Tasks[i]
		}
	}
	return nil
}

// GetServer looks up a server by name.
// GetServer 按名称查找服务器。
func (c *Config) GetServer(name string) *Server {
	for i := range c.Servers {
		if c.Servers[i].Name == name {
			return &c.Servers[i]
		}
	}
	return nil
}

func ParseTarget(target string) (serverName, path string) {
	// IPv6 address in brackets: [::1]:/path or [::1]:path
	if strings.HasPrefix(target, "[") {
		if idx := strings.IndexByte(target, ']'); idx > 0 && idx+1 < len(target) && target[idx+1] == ':' {
			return target[:idx+1], target[idx+2:]
		}
	}
	for i := 0; i < len(target); i++ {
		if target[i] == ':' && i > 0 && target[i-1] != '\\' && i < len(target)-1 && target[i+1] != '\\' {
			return target[:i], target[i+1:]
		}
	}
	return "", target
}
