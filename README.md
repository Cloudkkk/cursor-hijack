# Cursor Hijack —— Cursor IDE 流量拦截与日志监控

> Fork 自 [burpheart/cursor-tap](https://github.com/burpheart/cursor-tap)，原项目实现了 Cursor IDE 的 gRPC MITM 流量分析。
本仓库在此基础上进行了功能增强和问题修复。

Cursor IDE gRPC 中间人流量分析工具。可以解密 TLS、反序列化 protobuf、实时展示 AI 对话产生的 RPC 请求和响应。

| Cursor | 日志 |
|------|------|
| <img width="808" height="1808" alt="image" src="https://github.com/user-attachments/assets/c97ea22c-dd8f-4fa1-9fad-7d4a87915374" />| <img width="2218" height="4084" alt="image (2)" src="https://dev.bsgun.cn/i/2026/05/20/219895.webp" />|

## 快速开始

### 前提条件

- Go 1.21+
- Node.js 18+
- macOS / Linux / Windows

### 一键启动（推荐）

克隆仓库后直接运行，**首次安装也可以直接使用**，无需任何手动配置：

```bash
git clone https://github.com/Cloudkkk/cursor-hijack.git
cd cursor-hijack
./start.sh
```

脚本会自动完成所有工作：

1. **检测并构建** Go 二进制（源码有变化时自动重建）
2. **安装 Web 依赖**（首次运行时自动 `npm install`）
3. **关闭已运行的 Cursor** 进程
4. **修改 `settings.json`**（启用 proxy 配置、开启 HTTP/1.1 模式）
5. **启动 cursor-hijack 代理 + Web UI**
6. **注入 launchctl 环境变量**（CA 证书、代理地址）
7. **以 `--proxy-server` 参数启动 Cursor**

按 **Ctrl+C** 停止时，脚本会自动还原 `settings.json`、清除环境变量、关闭所有相关进程。

#### 常用参数

```bash
# 指定要在 Cursor 中打开的项目目录
./start.sh --project /path/to/your/project

# 同时录制流量到文件
./start.sh --record

# 通过上游代理访问网络
./start.sh --upstream socks5://127.0.0.1:7890

# 只启动代理 + Web UI，不动 Cursor
./start.sh --no-cursor

# 端口被残留进程占用时，强制释放
./start.sh --force

# 组合使用
./start.sh --project ~/my-project --upstream socks5://127.0.0.1:7890 --record
```

> **提示**：脚本退出（Ctrl+C）时会从备份还原 `settings.json` 到启动前的状态，下次 `start.sh` 启动时会重新注入配置。

### 验证

在 Cursor 中发起一次 AI 对话，然后：

- **终端**：应该能看到 TLS 握手和 HTTP 解析日志
- **Web UI**：左侧 Services 列表会出现 `BidiService`（对话流）和 `AgentService`（Agent 模式），点击可查看完整的请求/响应 JSON

## 手动配置（可选）

如果你不想用一键脚本，或者想了解脚本背后做了什么，可以按以下步骤手动配置。

<details>
<summary>点击展开手动配置步骤</summary>

### 1. 构建

```bash
git clone https://github.com/Cloudkkk/cursor-hijack.git
cd cursor-hijack
go build ./cmd/cursor-hijack
```

### 2. 启动 cursor-hijack 代理

在系统终端（Terminal.app，**不是** Cursor 内置终端）中执行：

```bash
./cursor-hijack start --http-parse --http-log 3
```

启动后会看到：

```
╔══════════════════════════════════════════╗
║    cursor-hijack Proxy Starting         ║
╠══════════════════════════════════════════╣
║  HTTP Proxy:    127.0.0.1:8080          ║
║  SOCKS5 Proxy:  127.0.0.1:1080          ║
║  API Server:    127.0.0.1:9090          ║
╚══════════════════════════════════════════╝
```

如果需要将流量记录到文件：

```bash
./cursor-hijack start --http-parse --http-log 3 --http-record ./traffic.jsonl
```

如果需要通过上游代理访问网络（比如已有科学上网工具）：

```bash
./cursor-hijack start --http-parse --http-log 3 --upstream socks5://127.0.0.1:7890
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

这一步是关键。需要让 Cursor 的所有网络请求（包括 Agent 大模型对话）经过 cursor-hijack 代理。

#### 第一步：注入 CA 证书信任（macOS）

cursor-hijack 首次启动会自动生成 CA 证书到 `~/.cursor-hijack/ca/ca.crt`。需要让 Cursor 信任这个证书：

```bash
launchctl setenv NODE_EXTRA_CA_CERTS ~/.cursor-hijack/ca/ca.crt
launchctl setenv HTTP_PROXY http://127.0.0.1:8080
launchctl setenv HTTPS_PROXY http://127.0.0.1:8080
```

Linux/Windows 用户直接在启动 Cursor 前设置环境变量：

```bash
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

</details>

## 命令参考

```
cursor-hijack start [flags]

Flags:
    --http-port int        HTTP 代理端口 (默认 8080)
    --socks5-port int      SOCKS5 代理端口 (默认 1080)
    --api-port int         管理 API / WebSocket 端口 (默认 9090)
    --http-parse           启用 HTTP 流解析和日志
    --http-log int         日志级别 0=关闭 1=基础 2=含header 3=含body 4=debug (默认 1)
    --http-record string   将流量记录到 JSONL 文件（自动启用 --http-parse）
    --upstream string      上游代理 URL (如 socks5://127.0.0.1:7890)
    --cert-dir string      证书存储目录 (默认 ~/.cursor-hijack)
    --data-dir string      数据存储目录 (默认 cert-dir/data)

cursor-hijack ca info          查看 CA 证书信息
cursor-hijack ca export -o .   导出 CA 证书
cursor-hijack stats            查看统计信息
cursor-hijack sessions         列出活跃会话
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

## 致谢

- [burpheart/cursor-tap](https://github.com/burpheart/cursor-tap) — 原始项目
