#!/usr/bin/env bash
# 一键:改 Cursor 设置 + 启代理 + 启 Web UI + 注入环境变量 + 重启 Cursor
# 用法:
#   ./start.sh                                  # 默认打开当前仓库
#   ./start.sh --project /path/to/proj          # 指定要在 Cursor 中打开的项目
#   ./start.sh --record                         # 同时录制流量到 ./traffic.jsonl
#   ./start.sh --upstream socks5://127.0.0.1:7890   # 走上游代理
#   ./start.sh --no-cursor                      # 只起代理+Web,不动 Cursor
#   ./start.sh --force                          # 强制清理残留端口占用后启动
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

BIN="$ROOT_DIR/cursor-hijack"
LOG_DIR="$ROOT_DIR/.run"
PROXY_LOG="$LOG_DIR/proxy.log"
WEB_LOG="$LOG_DIR/web.log"
PID_FILE="$LOG_DIR/pids"

CURSOR_SETTINGS="$HOME/Library/Application Support/Cursor/User/settings.json"
CURSOR_SETTINGS_BAK="$LOG_DIR/cursor-settings.json.bak"
CURSOR_BIN="/Applications/Cursor.app/Contents/MacOS/Cursor"
CA_CERT="$HOME/.cursor-hijack/ca/ca.crt"

mkdir -p "$LOG_DIR"

# 解析参数
DO_RECORD=0
UPSTREAM_ADDR=""
START_CURSOR=1
FORCE_FREE=0
PROJECT_PATH="$ROOT_DIR"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --record)     DO_RECORD=1; shift ;;
    --upstream)   UPSTREAM_ADDR="$2"; shift 2 ;;
    --project)    PROJECT_PATH="$2"; shift 2 ;;
    --no-cursor)  START_CURSOR=0; shift ;;
    --force)      FORCE_FREE=1; shift ;;
    -h|--help)
      sed -n '2,9p' "$0"; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 1 ;;
  esac
done

# 前提检查
command -v go   >/dev/null 2>&1 || { echo "缺少 Go (需要 1.21+)";   exit 1; }
command -v node >/dev/null 2>&1 || { echo "缺少 Node.js (需要 18+)"; exit 1; }
command -v npm  >/dev/null 2>&1 || { echo "缺少 npm"; exit 1; }

# 1) 构建 Go 二进制(源码有变化时重建)
need_build=0
if [[ ! -x "$BIN" ]]; then
  need_build=1
elif [[ -n "$(find cmd internal pkg -newer "$BIN" -type f \( -name '*.go' \) -print -quit 2>/dev/null)" ]]; then
  need_build=1
fi
if [[ $need_build -eq 1 ]]; then
  echo "==> 构建 cursor-hijack..."
  go build -o "$BIN" ./cmd/cursor-hijack
fi

# 2) Web 依赖
if [[ ! -d "$ROOT_DIR/web/node_modules" ]]; then
  echo "==> 安装 Web 依赖..."
  (cd "$ROOT_DIR/web" && npm install)
fi

# 端口占用检查;--force 时自动清掉残留监听者
check_port() {
  local port=$1 name=$2
  local listening_pids
  listening_pids=$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null || true)
  [[ -z "$listening_pids" ]] && return 0
  if [[ $FORCE_FREE -eq 1 ]]; then
    echo "  端口 $port ($name) 被占用,--force 强制释放: PID $listening_pids"
    kill $listening_pids 2>/dev/null || true
    sleep 1
    # 还在?升级到 -9
    listening_pids=$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null || true)
    [[ -n "$listening_pids" ]] && kill -9 $listening_pids 2>/dev/null || true
  else
    echo "端口 $port ($name) 已被占用,加 --force 自动清理或手动 kill 后重试" >&2
    lsof -nP -iTCP:"$port" -sTCP:LISTEN >&2
    exit 1
  fi
}
check_port 8080 "HTTP 代理"
check_port 1080 "SOCKS5 代理"
check_port 9090 "API/WS"
check_port 3000 "Web UI"

# 修改 Cursor settings.json:确保 proxy + disableHttp2 配置存在且启用
# 处理三种情况:行被 // 注释 → 去注释;行存在但值不对 → 替换;行完全不存在 → 注入
patch_cursor_settings() {
  if [[ ! -f "$CURSOR_SETTINGS" ]]; then
    echo "  跳过 Cursor 设置(未找到 $CURSOR_SETTINGS)"
    return
  fi
  cp "$CURSOR_SETTINGS" "$CURSOR_SETTINGS_BAK"

  # 第一轮:去注释(兼容之前 restore 留下的注释行)
  sed -i '' \
    -e 's|^\([[:space:]]*\)// \("http\.proxy":[[:space:]]*"http://127\.0\.0\.1:8080",\)|\1\2|' \
    -e 's|^\([[:space:]]*\)// \("http\.proxyStrictSSL":[[:space:]]*false,\)|\1\2|' \
    -e 's|^\([[:space:]]*\)// \("http\.proxySupport":[[:space:]]*"on",\)|\1\2|' \
    "$CURSOR_SETTINGS"

  # 第二轮:disableHttp2 已存在则改值
  sed -i '' \
    -e 's|"cursor\.general\.disableHttp2":[[:space:]]*false|"cursor.general.disableHttp2": true|' \
    "$CURSOR_SETTINGS"

  # 第三轮:如果某行仍不存在,用 python 注入到 JSON 末尾的 } 之前
  local need_inject=0
  grep -q '"http\.proxy"' "$CURSOR_SETTINGS" 2>/dev/null                    || need_inject=1
  grep -q '"http\.proxyStrictSSL"' "$CURSOR_SETTINGS" 2>/dev/null           || need_inject=1
  grep -q '"http\.proxySupport"' "$CURSOR_SETTINGS" 2>/dev/null             || need_inject=1
  grep -q '"cursor\.general\.disableHttp2"' "$CURSOR_SETTINGS" 2>/dev/null  || need_inject=1

  if [[ $need_inject -eq 1 ]]; then
    python3 - "$CURSOR_SETTINGS" << 'PYEOF'
import json, sys
path = sys.argv[1]
with open(path, "r") as f:
    data = json.load(f)
inject = {
    "http.proxy": "http://127.0.0.1:8080",
    "http.proxyStrictSSL": False,
    "http.proxySupport": "on",
    "cursor.general.disableHttp2": True,
}
changed = False
for k, v in inject.items():
    if k not in data:
        data[k] = v
        changed = True
    elif k == "cursor.general.disableHttp2" and data[k] is not True:
        data[k] = True
        changed = True
if changed:
    with open(path, "w") as f:
        json.dump(data, f, indent=4, ensure_ascii=False)
        f.write("\n")
PYEOF
  fi

  echo "  已修改 Cursor settings.json (备份: $CURSOR_SETTINGS_BAK)"
}

restore_cursor_settings() {
  if [[ -f "$CURSOR_SETTINGS_BAK" ]]; then
    mv "$CURSOR_SETTINGS_BAK" "$CURSOR_SETTINGS"
    echo "  已恢复 Cursor settings.json"
  fi
}

kill_cursor() {
  killall -9 Cursor "Cursor Helper" "Cursor Helper (Renderer)" \
    "Cursor Helper (GPU)" "Cursor Helper (Plugin)" 2>/dev/null || true
}

# 递归 kill 一棵进程树(macOS 没有 setsid,npm/next 这种父子结构必须挨个杀)
kill_tree() {
  local pid=$1 child
  for child in $(pgrep -P "$pid" 2>/dev/null); do
    kill_tree "$child"
  done
  kill -TERM "$pid" 2>/dev/null || true
}

setenv_launchctl() {
  launchctl setenv NODE_EXTRA_CA_CERTS "$CA_CERT"
  launchctl setenv HTTP_PROXY  "http://127.0.0.1:8080"
  launchctl setenv HTTPS_PROXY "http://127.0.0.1:8080"
}

unsetenv_launchctl() {
  launchctl unsetenv NODE_EXTRA_CA_CERTS 2>/dev/null || true
  launchctl unsetenv HTTP_PROXY  2>/dev/null || true
  launchctl unsetenv HTTPS_PROXY 2>/dev/null || true
}

# 等待 CA 证书生成(代理首启时由 cursor-hijack 自动写入)
wait_for_ca() {
  for _ in $(seq 1 30); do
    [[ -f "$CA_CERT" ]] && return 0
    sleep 0.5
  done
  echo "等待 CA 证书超时($CA_CERT)" >&2; return 1
}

CLEANING_UP=0
cleanup() {
  [[ $CLEANING_UP -eq 1 ]] && return
  CLEANING_UP=1
  echo
  echo "==> 停止服务..."
  if [[ $START_CURSOR -eq 1 ]]; then
    kill_cursor
    unsetenv_launchctl
    echo "  已关闭 Cursor 并清除 launchctl 环境变量"
  fi
  if [[ -f "$PID_FILE" ]]; then
    while read -r pid; do
      [[ -n "$pid" ]] && kill_tree "$pid"
    done < "$PID_FILE"
    rm -f "$PID_FILE"
  fi
  # 兜底:如果端口还占着,强杀监听者
  for port in 8080 1080 9090 3000; do
    pids=$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null || true)
    [[ -n "$pids" ]] && kill -9 $pids 2>/dev/null || true
  done
  if [[ $START_CURSOR -eq 1 ]]; then
    restore_cursor_settings
  fi
  exit 0
}
trap cleanup INT TERM

# 3) 关闭已运行的 Cursor(避免占用 settings.json 与未走代理的旧进程)
if [[ $START_CURSOR -eq 1 ]]; then
  echo "==> 关闭已运行的 Cursor"
  kill_cursor
  sleep 2
fi

# 4) 修改 Cursor 设置
if [[ $START_CURSOR -eq 1 ]]; then
  echo "==> 修改 Cursor settings.json"
  patch_cursor_settings
fi

# 5) 启动代理 — bash 3.2 + set -u 下空数组展开有 bug,改用字符串拼接
echo "==> 启动 cursor-hijack 代理 (日志: $PROXY_LOG)"
PROXY_FLAGS="--http-parse --http-log 3"
[[ $DO_RECORD -eq 1 ]]    && PROXY_FLAGS="$PROXY_FLAGS --http-record $ROOT_DIR/traffic.jsonl"
[[ -n "$UPSTREAM_ADDR" ]] && PROXY_FLAGS="$PROXY_FLAGS --upstream $UPSTREAM_ADDR"
# shellcheck disable=SC2086
"$BIN" start $PROXY_FLAGS >"$PROXY_LOG" 2>&1 &
PROXY_PID=$!
echo "$PROXY_PID" > "$PID_FILE"

# 6) 启动 Web UI(用 exec 让 WEB_PID 直接是 npm 进程,便于后续递归 kill)
echo "==> 启动 Web UI (日志: $WEB_LOG)"
( cd "$ROOT_DIR/web" && exec npm run dev ) >"$WEB_LOG" 2>&1 &
WEB_PID=$!
echo "$WEB_PID" >> "$PID_FILE"

sleep 2
# 健康检查
if ! kill -0 "$PROXY_PID" 2>/dev/null; then
  echo "代理启动失败,查看 $PROXY_LOG"; tail -n 30 "$PROXY_LOG"; cleanup
fi
if ! kill -0 "$WEB_PID" 2>/dev/null; then
  echo "Web UI 启动失败,查看 $WEB_LOG"; tail -n 30 "$WEB_LOG"; cleanup
fi

# 7) 注入 launchctl 环境变量并启动 Cursor
CURSOR_PID=""
if [[ $START_CURSOR -eq 1 ]]; then
  echo "==> 等待 CA 证书生成"
  wait_for_ca || cleanup

  echo "==> 注入 launchctl 环境变量(NODE_EXTRA_CA_CERTS / HTTP(S)_PROXY)"
  setenv_launchctl

  if [[ ! -x "$CURSOR_BIN" ]]; then
    echo "未找到 Cursor 可执行文件: $CURSOR_BIN" >&2; cleanup
  fi
  echo "==> 启动 Cursor → $PROJECT_PATH"
  "$CURSOR_BIN" --proxy-server="http://127.0.0.1:8080" "$PROJECT_PATH" \
    >"$LOG_DIR/cursor.log" 2>&1 &
  CURSOR_PID=$!
  echo "$CURSOR_PID" >> "$PID_FILE"
fi

cat <<EOF

╔══════════════════════════════════════════╗
║  cursor-hijack 已启动                     ║
╠══════════════════════════════════════════╣
║  HTTP Proxy:    127.0.0.1:8080           ║
║  SOCKS5 Proxy:  127.0.0.1:1080           ║
║  API Server:    127.0.0.1:9090           ║
║  Web UI:        http://localhost:3000    ║
╚══════════════════════════════════════════╝

代理 PID: $PROXY_PID    Web PID: $WEB_PID
日志:  tail -f $PROXY_LOG
       tail -f $WEB_LOG

已自动完成:
  ✓ 关闭旧 Cursor 进程
  ✓ settings.json: 打开 http.proxy/proxyStrictSSL/proxySupport, disableHttp2=true
  ✓ launchctl: NODE_EXTRA_CA_CERTS / HTTP_PROXY / HTTPS_PROXY
  ✓ 用 --proxy-server 启动 Cursor (PID: ${CURSOR_PID:-skipped})

按 Ctrl+C 停止服务:还原 settings.json、清除 launchctl 环境变量、关闭 Cursor。
EOF

# 任一关键进程退出则收尾(Cursor 退出不算)
# wait -n 需要 Bash 4.3+，macOS 自带 Bash 3.2 不支持，改用轮询
while kill -0 "$PROXY_PID" 2>/dev/null && kill -0 "$WEB_PID" 2>/dev/null; do
  sleep 2
done
cleanup
