#!/usr/bin/env bash
# 剧院服装换装路线与场次清点系统 —— 一条命令本地启动。
# 用法：
#   ./run.sh                 构建前端+后端并启动（默认 :8080）
#   ./run.sh --dev           前端热更新开发模式（Vite :5173 + API :8080）
#   ADDR=:9090 ./run.sh      自定义端口
set -euo pipefail
cd "$(dirname "$0")"

ADDR="${ADDR:-:8080}"
DB_PATH="${DB_PATH:-data/theater.db}"

# 让脚本能找到安装在用户目录下的 Go（如 ~/.local/go/bin/go）。
if ! command -v go >/dev/null 2>&1; then
  for d in "$HOME/.local/go/bin" "$HOME/go/bin" /usr/local/go/bin; do
    [ -x "$d/go" ] && PATH="$d:$PATH" && break
  done
fi

if [ "${1:-}" = "--dev" ]; then
  echo ">> 启动后端 API :8080 ..."
  go run . -addr "$ADDR" -db "$DB_PATH" &
  API_PID=$!
  trap 'kill $API_PID 2>/dev/null || true' EXIT
  echo ">> 启动前端开发服务器 :5173 ..."
  (cd web && npm run dev)
  exit 0
fi

# 生产模式：构建前端 → 内嵌进 Go 二进制 → 单进程启动。
if [ ! -f web/dist/index.html ]; then
  echo ">> 首次运行，构建前端 ..."
  (cd web && npm install --registry=https://registry.npmmirror.com && npm run build)
fi

echo ">> 编译后端（内嵌前端静态资源）..."
go build -o theater-costume .

echo ">> 启动系统：http://localhost${ADDR}"
exec ./theater-costume -addr "$ADDR" -db "$DB_PATH"
