#!/usr/bin/env bash
# 剧院服装换装路线与场次清点系统 —— 一条命令启动
# 用法: ./run.sh            启动服务（默认 :8080）
#       ./run.sh test       只跑验证测试
#       ADDR=:9090 ./run.sh 指定端口
set -euo pipefail
cd "$(dirname "$0")"

# 若 PATH 中没有 go，则尝试使用 $HOME/sdk/go（本仓库环境的安装位置）
if ! command -v go >/dev/null 2>&1; then
  if [ -x "$HOME/sdk/go/bin/go" ]; then
    export PATH="$HOME/sdk/go/bin:$PATH"
  else
    echo "未找到 Go（>=1.21），请先安装: https://go.dev/dl/" >&2
    exit 1
  fi
fi
# 国内网络可走 goproxy.cn；已配置过则不覆盖
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

if [ "${1:-}" = "test" ]; then
  go test ./...
  exit $?
fi

ADDR="${ADDR:-:8080}"
export ADDR
export DB_PATH="${DB_PATH:-data/theater.db}"

echo ">> 编译后端..."
go build -o bin/theater ./cmd/server
echo ">> 启动服务 $ADDR （首次启动自动写入演示剧目《夜宴》）"
echo ">> 浏览器打开 http://localhost${ADDR}"
exec ./bin/theater
