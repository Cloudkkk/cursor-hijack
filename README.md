# Cursor Hijack —— Cursor IDE 流量拦截与日志监控

> Fork 自 [burpheart/cursor-tap](https://github.com/burpheart/cursor-tap)，原项目实现了 Cursor IDE 的 gRPC MITM 流量分析。本仓库在此基础上进行了功能增强和问题修复。

中文 | [English](./README_EN.md)

Cursor IDE gRPC 中间人流量分析工具。可以解密 TLS、反序列化 protobuf、实时展示 AI 对话产生的 RPC 请求和响应。

## 为什么做这个

Cursor 和后端的通信全是 gRPC，走的 Connect Protocol，body 是二进制 protobuf。用 Burp 或 Fiddler 抓到的都是一堆看不懂的二进制。官方也没公开 proto 定义，想看 AI 对话的具体内容很麻烦。

这个工具能把流量解密成可读的 JSON，还能实时看到 streaming 的每一帧。

## 快速开始

### 前提条件

- Go 1.21+
- Node.js 18+
- macOS / Linux / Windows

### 1. 克隆并构建

```bash
git clone https://github.com/Cloudkkk/cursor-hijack.git
cd cursor-hijack
go build ./cmd/cursor-tap
```

### 2. 启动 cursor-tap 代理

在系统终端（Terminal.app，**不是** Cursor 内置终端）中执行：

```bash
./cursor-tap start --http-parse --http-log 3
```

启动后会看到：

```
╔══════════════════════════════════════════╗
║       cursor-tap Proxy Starting         ║
╠══════════════════════════════════════════╣
║  HTTP Proxy:    127.0.0.1:8080          ║
║  SOCKS5 Proxy:  127.0.0.1:1080          ║
║  API Server:    127.0.0.1:9090          ║
╚══════════════════════════════════════════╝
```

如果需要将流量记录到文件：

```bash
./cursor-tap start --http-parse --http-log 3 --http-record ./traffic.jsonl
```

如果需要通过上游代理访问网络（比如已有科学上网工具）：

```bash
./cursor-tap start --http-parse --http-log 3 --upstream socks5://127.0.0.1:7890
```

### 3. 启动 Web UI

新开一个终端：

```bash
cd web
npm install
npm run dev
```

浏览器访问 `http://localhost:3000` 即可看到实时流量面板。

### 4. 配置 Cursor IDE 走代理

这一步是关键。需要让 Cursor 的所有网络请求（包括 Agent 大模型对话）经过 cursor-tap 代理。

#### 第一步：注入 CA 证书信任（macOS）

cursor-tap 首次启动会自动生成 CA 证书到 `~/.cursor-hijack/ca/ca.crt`。需要让 Cursor 信任这个证书：

```bash
# 注入环境变量到 macOS launchd（影响所有后续启动的应用）
launchctl setenv NODE_EXTRA_CA_CERTS ~/.cursor-hijack/ca/ca.crt
launchctl setenv HTTP_PROXY http://127.0.0.1:8080
launchctl setenv HTTPS_PROXY http://127.0.0.1:8080
```

Linux/Windows 用户直接在启动 Cursor 前设置环境变量：

```bash
# Linux
export NODE_EXTRA_CA_CERTS=~/.cursor-hijack/ca/ca.crt
export HTTP_PROXY=http://127.0.0.1:8080
export HTTPS_PROXY=http://127.0.0.1:8080
```

#### 第二步：修改 Cursor 网络设置

打开 Cursor → Settings → 搜索 `Network`：

- **HTTP Compatibility Mode** → 改为 **HTTP/1.1**（必须，cursor-hijack 目前不支持 HTTP/2 MITM）

再打开 Settings JSON（`Cmd+Shift+P` → `Preferences: Open User Settings (JSON)`），添加：

```json
"http.proxy": "http://127.0.0.1:8080",
"http.proxyStrictSSL": false,
"http.proxySupport": "on"
```

#### 第三步：用 `--proxy-server` 强制启动 Cursor

普通方式启动 Cursor 时，extension-host 子进程会绕过 `http.proxy` 设置直连服务器。必须通过 Chromium 启动参数强制代理：

```bash
# macOS - 先彻底关闭 Cursor
killall -9 Cursor "Cursor Helper" "Cursor Helper (Renderer)" "Cursor Helper (GPU)" "Cursor Helper (Plugin)" 2>/dev/null
sleep 3

# 使用 --proxy-server 强制所有请求走代理
/Applications/Cursor.app/Contents/MacOS/Cursor --proxy-server="http://127.0.0.1:8080" /path/to/your/project
```

> **注意**：必须用二进制路径直接启动，不能用 `open -a Cursor`，否则 macOS LaunchServices 会复用已有进程，环境变量和启动参数都会丢失。

### 5. 验证

在 Cursor 中发起一次 AI 对话，然后：

- **终端**：应该能看到 TLS 握手和 HTTP 解析日志
- **Web UI**：左侧 Services 列表会出现 `BidiService`（对话流）和 `AgentService`（Agent 模式），点击可查看完整的请求/响应 JSON

## 命令参考

```
cursor-tap start [flags]

Flags:
    --http-port int        HTTP 代理端口 (默认 8080)
    --socks5-port int      SOCKS5 代理端口 (默认 1080)
    --api-port int         管理 API / WebSocket 端口 (默认 9090)
    --http-parse           启用 HTTP 流解析和日志
    --http-log int         日志级别 0=关闭 1=基础 2=含header 3=含body 4=debug (默认 1)
    --http-record string   将流量记录到 JSONL 文件（自动启用 --http-parse）
    --upstream string      上游代理 URL (如 socks5://127.0.0.1:7890)
    --cert-dir string      证书存储目录 (默认 ~/.cursor-tap)
    --data-dir string      数据存储目录 (默认 cert-dir/data)

cursor-tap ca info          查看 CA 证书信息
cursor-tap ca export -o .   导出 CA 证书
cursor-tap stats            查看统计信息
cursor-tap sessions         列出活跃会话
```

## Cursor 域名说明

| 域名 | 用途 |
|------|------|
| `api2.cursor.sh` | API 请求（管理、配置、AI 对话） |
| `api3.cursor.sh` | Cursor Tab 代码补全 |
| `api4.cursor.sh` | Cursor Tab（区域节点） |
| `*.api5.cursor.sh` | Agent 请求 |
| `repo42.cursor.sh` | 代码库索引 |
| `*.authentication.cursor.sh` | 认证服务 |

> 在 HTTP/1.1 兼容模式下，大模型对话（`BidiService/BidiAppend`）和 Agent（`AgentService/RunSSE`）的流量都走 `api2.cursor.sh`。

## 清理

使用完毕后，移除注入的环境变量：

```bash
# macOS
launchctl unsetenv NODE_EXTRA_CA_CERTS
launchctl unsetenv HTTP_PROXY
launchctl unsetenv HTTPS_PROXY
```

并从 Cursor 的 `settings.json` 中删除 `http.proxy` 等相关配置，将 HTTP Compatibility Mode 改回 HTTP/2。

## 相关文章

- [Cursor 逆向笔记 1 —— 我是如何拦截解析 Cursor 的 gRPC 通信流量的](./cursor-reverse-notes-1.md)

## 致谢

- [burpheart/cursor-tap](https://github.com/burpheart/cursor-tap) — 原始项目
