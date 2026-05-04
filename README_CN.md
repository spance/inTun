# inTun

[English](README.md) | [简体中文](README_CN.md)

Interactive SSH Tunnel - 跨平台 SSH 隧道管理器，基于纯 Go 实现，提供现代化 TUI 界面。

[![Go Version](https://img.shields.io/badge/go-1.21%2B-blue)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

## 功能特性

- **三种隧道模式**：本地端口转发 (-L)、远程端口转发 (-R)、动态 SOCKS 代理 (-D)
- **纯 Go SSH 实现**：不依赖系统 ssh/plink，完全跨平台
- **实时监控**：上下行流量统计 (TX/RX)、传输速率、网络延迟
- **自动配置**：解析 `~/.ssh/config` 自动发现主机
- **标签分组**：解析 `#!! GroupLabels` 注释，支持主机标签和过滤
- **交互式主机密钥验证**：可视化界面接受或拒绝未知主机密钥
- **密码认证**：通过 TUI 交互式输入密码，支持键盘交互式认证
- **连接健康检测**：SSH/TCP 双重保活，连接断开自动提示重连
- **远程隧道 LAN 目标**：本地目标和远程监听均支持 `ip:port` 格式
- **键盘驱动界面**：快捷键操作，高效导航

## 安装

### 从源码构建

```bash
git clone https://github.com/spance/intun.git
cd intun
make build

# 或交叉编译所有平台
make all
```

### 系统安装

```bash
make install    # 构建并复制到 /usr/local/bin/
```

### 预编译二进制

从 [Releases](https://github.com/spance/intun/releases) 页面下载最新版本。

## 快速开始

启动 intun：

```bash
./intun
```

### 创建隧道

1. 按 `c` 创建新隧道
2. 从 `~/.ssh/config` 列表中选择主机
3. 选择隧道类型：
   - **本地**：将本地端口转发至远程服务
   - **远程**：将远程端口转发至本地服务（支持 LAN 目标地址）
   - **动态**：创建 SOCKS 代理
4. 按提示输入端口号

### 快捷键

| 按键 | 操作 |
|------|------|
| `c` | 创建隧道 |
| `r` | 重新连接 |
| `s` | 停止/启动 |
| `d` | 删除隧道 |
| `↑↓` | 导航选择 |
| `e` | 退出 |

## 系统要求

- Go 1.21+（构建时）
- SSH 私钥：`~/.ssh/id_rsa`、`id_ed25519` 或 `id_ecdsa`，或使用密码认证
- SSH 配置文件：`~/.ssh/config`（可选，用于主机发现）

## 配置

intun 自动读取 `~/.ssh/config`：

```ssh
Host myserver
    Hostname example.com
    User root
    Port 2222
    IdentityFile ~/.ssh/custom_key
    #!! GroupLabels production web
```

支持字段：
- `Host` - 别名
- `Hostname` - 实际主机地址
- `User` - 用户名
- `Port` - 端口（默认 22）
- `IdentityFile` - 私钥路径
- `#!! GroupLabels` - 标签（以金色高亮显示）

## 技术架构

- **UI 框架**: [bubbletea](https://github.com/charmbracelet/bubbletea) (Charm TUI)
- **SSH 库**: [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh)
- **样式渲染**: [lipgloss](https://github.com/charmbracelet/lipgloss)
- **统计监控**: 1秒间隔采样，5秒间隔 SSH 探测，TX/RX 总量 + ↑↓ 速率指示

## 开发

```bash
# 本地构建
make build

# 运行测试
make test

# 注入版本号
VERSION=$(git describe --tags)
go build -ldflags "-X main.Version=$VERSION" ./cmd/intun

# 交叉编译
make all    # 全平台编译
```

### 调试

设置 `INTUN_LOG` 环境变量启用 SSH 连接诊断日志：

```bash
INTUN_LOG=/tmp/intun.log ./intun
```

## 项目结构

```
cmd/intun/
  └── main.go              # 入口程序

internal/
  ├── config/              # SSH 配置解析 + GroupLabels
  ├── platform/            # SSH 连接、主机密钥管理、Mock 测试
  ├── tunnel/              # 隧道生命周期管理（线程安全）
  ├── monitor/             # 统计监控（同步更新）
  └── tui/                 # TUI 界面渲染 + 认证队列
```

## 许可协议

MIT License - 详见 [LICENSE](LICENSE) 文件。
