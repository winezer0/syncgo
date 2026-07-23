[English](README_EN.md) | 简体中文

# Shuttle — 跨平台增量文件同步工具

**Shuttle** 是一个跨平台（Windows / macOS / Linux）的文件同步工具，通过 `syncd.yaml` 定义本地→远程映射，一键推送。内置 [`delta`](delta/) 包（源自 [go-rsync](https://github.com/henryborner/go-rsync)）实现 rsync delta 算法，与标准 rsync 线协议不兼容（使用 CHAR_OFFSET=31 的自有线协议）。纯 Go + Go 汇编实现，`CGO_ENABLED=0` 编译为完全静态二进制，无外部依赖。

> 原始项目请访问 [Shuttle](https://github.com/henryborner/shuttle)

```powershell
shuttle                    # 双击启动 TUI
shuttle push web           # 一键同步
shuttle exec vps "uptime"  # 远端执行命令
```

## 功能

- **跨平台** — Windows / macOS / Linux，支持 amd64 / arm64（Apple Silicon、AWS Graviton、树莓派）
- **纯 Go 构建** — `CGO_ENABLED=0` 静态二进制，无 CGO、无 libc 依赖
- **双同步模式** — `overlay`（增量覆盖）/ `full_replace`（tar.gz 压缩全量替换）
- **增量传输** — rsync delta 算法，仅传输文件变化部分
- **Task Hooks** — 同步前后自动执行远端命令（停服/重启/清缓存）
- **远端命令** — `shuttle exec` 独立执行 SSH 命令，无需同步任务
- **Agent 自动部署** — `shuttle deploy-agent` 三级回退（本地文件 → Release 下载 → 交叉编译）+ 远端执行验证
- **认证方式** — 配置 password 用密码，配置 key_file 用密钥，都不配则自动使用本机 ~/.ssh 密钥
- **重试策略** — 可配置最大重试次数和间隔，瞬态失败自动恢复
- **增量开关** — 全局 + 任务级 incremental 开关，关闭时强制全量比对
- **服务器保护** — glob 模式保护列表，远端关键文件不被覆盖或删除
- **冲突检测** — 检测远端文件比本地新的情况并统计
- **TUI 界面** — 仪表盘、映射管理、服务器管理、文件浏览器、设置
- **SFTP/SSH** — 本地 → 远程，自动检测密钥
- **mmap 内存映射** — 大文件比对使用 mmap，减少内存拷贝
- **签名缓存** — 远端 agent 缓存块签名，重复同步跳过读盘
- **中英双语** — 设置页切换
- **库化 API** — 可作为 Go 库嵌入第三方项目（`syncer` 包）
- **单文件** — 零额外依赖，双击即用

## 安装

从 [Releases](https://github.com/winezer0/syncgo/releases) 下载对应平台二进制：

- **`shuttle.exe`** — Windows 主程序
- **`shuttle_linux`** — Linux 远程 agent（通过 `deploy-agent` 或 TUI 部署）

### 从源码构建

需要 Go 1.26+。纯 Go 实现，无需 C 编译器：

```bash
git clone https://github.com/winezer0/syncgo.git
cd syncgo

# 本机平台
CGO_ENABLED=0 go build -o shuttle ./cmd/shuttle

# 交叉编译示例
CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -o shuttle_linux  ./cmd/shuttle
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o shuttle_mac    ./cmd/shuttle
```

支持的目标平台：`windows`、`darwin`、`linux` × `amd64`、`arm64`（`deploy-agent` 远端另支持 `arm`/`386`/`riscv64`）。

## 快速开始

```powershell
.\shuttle.exe                        # 双击进 TUI
.\shuttle.exe tui                    # 命令行启动 TUI
.\shuttle.exe init                   # 生成配置模板
.\shuttle.exe list                   # 列出任务/服务器
.\shuttle.exe test myserver          # 测试 SSH 连接
.\shuttle.exe push web               # 一键同步
.\shuttle.exe push --dry-run         # 模拟运行，预览变更
.\shuttle.exe deploy-agent myserver  # 部署远端 delta agent
.\shuttle.exe exec myserver "df -h"  # 远端执行命令
.\shuttle.exe exec --all "uptime"    # 所有服务器执行
.\shuttle.exe version                # 版本信息
```

> 双击 `shuttle.exe` 进入 TUI 即可创建配置，无需手写 YAML。

## 配置文件

```yaml
# syncd.yaml
version: "1.0"
language: zh               # en / zh
checksum: xxh64            # md5 / sha256 / xxh64 / xxh3
workers: 4                 # delta 并行数: 1=串行 2/4/8=并行
incremental: true          # 全局增量开关

retry:                     # 重试策略
  max_retries: 3
  delay_ms: 1000

servers:
  - name: myserver
    host: 192.168.1.100
    port: 22
    user: deploy
    key_file: ~/.ssh/id_ed25519
    protect:               # 保护列表（glob）
      - "*.db"
      - "*.pem"
      - ".env"

tasks:
  # 增量覆盖（默认模式）
  - name: web
    source: E:\projects\web\dist\
    target: myserver:/var/www/html/
    options:
      sync_mode: overlay
      delete: true
      exclude: ["*.tmp", ".git/"]
      checksum: false
      flat: false
    hooks:
      before:
        - "systemctl stop nginx"
      after:
        - "systemctl start nginx"
      on_error: abort      # abort / warn / ignore

  # 压缩全量替换
  - name: app-release
    source: E:\builds\app\
    target: myserver:/opt/app/
    options:
      sync_mode: full_replace
      exclude: ["*.log"]

  # 单文件同步
  - name: nginx-config
    source: E:\configs\nginx.conf
    target: myserver:/etc/nginx/nginx.conf
    options:
      checksum: true
```

## CLI 命令

| 命令 | 说明 |
|------|------|
| `shuttle` | 双击启动 TUI |
| `shuttle push [name]` | 执行同步任务 |
| `shuttle list` | 列出所有任务和服务器 |
| `shuttle config` | 配置摘要 |
| `shuttle config --schema` | 完整字段参考 |
| `shuttle test <server>` | 测试 SSH 连接 |
| `shuttle init` | 生成配置模板 |
| `shuttle exec <server> "cmd"` | 远端执行命令 |
| `shuttle exec --all "cmd"` | 所有服务器执行 |
| `shuttle exec <server> --file script.sh` | 从文件读取命令 |
| `shuttle deploy-agent <server>` | 部署远端 delta agent |
| `shuttle version` | 版本信息 |

### push 参数

| 参数 | 说明 |
|------|------|
| `--dry-run` | 模拟运行，不实际修改文件 |
| `-v` | 详细输出（含 delta 匹配字节） |
| `-w N` | 并行 worker 数（默认 4） |
| `--algo md5\|xxh64\|xxh3\|sha256` | 校验和算法 |
| `-c path` | 指定配置文件路径 |

## 同步模式

### overlay（增量覆盖，默认）

- 仅上传新增/修改的文件
- 保留远端独有文件（配置、密钥、日志）
- 可选删除远端多余文件（`delete: true`）
- 支持 rsync delta 增量传输
- 适用场景：迭代部署、保留远端配置

### full_replace（压缩全量替换）

- 本地目录打包为 tar.gz
- SFTP 上传至远端 `/tmp`
- 远端清理目标目录 + 解压覆盖
- 远端独有文件**被删除**（强一致性）
- 适用场景：纯净发布、二进制更新、环境重置
- 要求：远端服务器需有 `tar` 命令

## Task Hooks（远端命令钩子）

在同步前后自动执行远端 SSH 命令：

```yaml
tasks:
  - name: web
    source: E:\projects\dist\
    target: myserver:/var/www/html/
    hooks:
      before:
        - "systemctl stop nginx"       # 同步前停服务
        - "cp -r /var/www /tmp/backup" # 备份
      after:
        - "systemctl start nginx"      # 同步后重启
        - "rm -rf /tmp/cache/*"        # 清缓存
      on_error: abort                  # abort=中止 / warn=警告 / ignore=忽略
```

**执行规则：**
- `before` 在 SSH 连接后、文件传输前执行
- `after` 仅在同步成功后执行
- 命令按列表顺序依次执行
- 建议使用绝对路径（SSH exec 可能不加载完整 PATH）

## 远端命令执行

```powershell
# 单服务器执行
shuttle exec vps "ls -la /var/www"
shuttle exec vps "systemctl restart nginx"
shuttle exec vps "df -h && free -m"

# 所有服务器执行
shuttle exec --all "uptime"

# 从文件读取命令
shuttle exec vps --file deploy.sh
```

## Agent 部署

`shuttle deploy-agent` 自动完成远端 delta agent 的部署：

```powershell
shuttle deploy-agent myserver
```

### Agent 与主程序的关系

Agent 与主程序是**同一个二进制**（同一份源码 `cmd/shuttle`）。远端仅使用其中的 `receive` 子命令进行 delta 签名计算和文件重建，但部署的是包含所有功能的完整 shuttle 可执行文件。

### 二进制获取策略（三级回退）

| 优先级 | 来源 | 说明 |
|--------|------|------|
| 1 | 本地文件 | 程序目录下的 `shuttle_linux_<arch>`（如 `shuttle_linux_amd64`） |
| 2 | GitHub Releases | 自动下载 `v<version>/shuttle_linux_<arch>` |
| 3 | 交叉编译 | 本机 `go build`（需 Go 环境，`CGO_ENABLED=0` 静态编译） |

> 提示：将预构建的 `shuttle_linux_amd64` / `shuttle_linux_arm64` 放在 `shuttle.exe` 同级目录，即可离线部署，无需网络和 Go 环境。

### 执行步骤

1. 连接服务器，检测 CPU 架构（`uname -m`）
2. 按三级回退策略获取 agent 二进制
3. SFTP 上传至 `~/.local/bin/shuttle`
4. 设置可执行权限（`chmod 0755`）
5. 确保 `~/.local/bin` 在 PATH 中
6. **执行验证** — 远端运行 `shuttle version` 确认可执行
7. **动态库诊断** — 若执行失败，自动运行 `ldd` 检测缺失的 `.so` 并给出修复建议

### 验证失败诊断示例

```
  Verifying agent... failed: remote exec: exit status 127

  ⚠ Missing shared libraries on remote:
    libpthread.so.0 => not found
    libc.so.6 => not found

  The agent binary requires dynamic libraries not available on this system.
  Solution: rebuild with CGO_ENABLED=0 for a fully static binary.
```

> 使用 `CGO_ENABLED=0` 编译的二进制为完全静态链接，不依赖任何 `.so`。此诊断主要防护手动放置了动态链接二进制的情况。

部署后，`shuttle push` 自动使用 delta 增量传输。

## 库化调用（Go SDK）

Shuttle 可作为 Go 库嵌入第三方项目：

```go
import "github.com/winezer0/syncgo/syncer"

// 编程式创建（无需 YAML 配置）
s := syncer.New(syncer.Options{
    Host:    "192.168.1.100",
    Port:    22,
    User:    "deploy",
    KeyFile: "~/.ssh/id_ed25519",
    Workers: 4,
})
defer s.Close()

// 上传单文件
s.UploadFile("local.conf", "/etc/app/config.conf")

// 同步目录（增量覆盖）
s.UploadDir("./dist", "/var/www/html")

// 同步目录（全量替换 + 删除冗余）
s.UploadDirWithOptions("./build", "/opt/app", syncer.SyncDirOptions{
    Mode:   "full_replace",
    Delete: true,
})

// 远端执行命令
out, _ := s.ExecRemote("systemctl restart nginx")

// 下载远端目录
s.DownloadDir("/var/log/app", "./logs")
```

### Context 支持

所有公开方法均有 `*Context` 变体，支持取消和超时：

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

err := s.ConnectContext(ctx)
err = s.UploadDirContext(ctx, "./dist", "/var/www/html")
out, err := s.ExecRemoteContext(ctx, "df -h")
```

### 函数式 Hook

```go
import "github.com/winezer0/syncgo/transport"

s.SetHook(&transport.HookFunc{
    OnStart: func(taskName string, totalFiles int) error {
        fmt.Printf("同步 %s: %d 个文件\n", taskName, totalFiles)
        return nil
    },
    OnProgress: func(path string, sent, total int64) {
        fmt.Printf("\r%s: %d%%", path, sent*100/total)
    },
    OnDone: func(evt transport.FileEvent) error {
        if evt.Error != nil {
            log.Printf("失败: %s: %v", evt.RelPath, evt.Error)
        }
        return nil
    },
})
```

## 快捷键（TUI）

| 按键 | 功能 |
|------|------|
| `Enter` | 同步选中 |
| `A` `E` `D` | 添加/编辑/删除映射 |
| `R` | 直接同步当前映射 |
| `Ctrl+T` | 测试服务器连接 |
| `P` | 编辑保护列表 |
| `Tab` | 切换文件浏览器 |

## 工作原理

### 增量传输（rsync delta 算法）

1. **分块** — 将源文件按固定大小切分为数据块
2. **签名** — 对每个块计算快速滚动校验和 + 强校验和（xxh64/md5/sha256）
3. **匹配** — 远端 agent 在已有文件上滑动窗口搜索匹配块
4. **delta** — 只传输不匹配的字节序列，匹配块只发送引用
5. **重构** — 远端根据 delta 指令重建完整文件（原子 rename）

文件完全相同时仅传输签名列表（约几 KB），无文件数据传输。

### 智能分层

```
有 agent → delta 增量传输（仅传变化块）
无 agent → 自动 fallback 全量 SFTP 上传
```

### 签名缓存

远端 agent 将块签名缓存至 `~/.shuttle_cache/`，文件未变化时跳过读盘直接返回缓存签名，大幅加速重复同步。

### 线协议

- **CHAR_OFFSET = 31**：字符偏移参数
- **默认强校验和 = xxh64**：64 位 xxHash
- 支持 md5（128 位）、xxh3（128 位）、sha256（256 位）

### 服务器保护

glob 模式保护列表，匹配的远端文件**不会被覆盖或删除**。适用于保护数据库、证书、配置文件等关键数据。

### 权限要求

| 操作 | 所需权限 |
|------|----------|
| 文件列表 / 签名计算 | 目录 `r-x`，文件 `r--` |
| 上传 / delta 重建 | 目标目录 `w` 权限 |
| 删除文件 | 文件所在目录 `w` 权限 |
| Agent 部署 / 缓存 | 用户 HOME 写权限 |

> 建议：目标目录属主为 SSH 登录用户，普通用户权限即可完成所有操作。

## 许可证

MIT
