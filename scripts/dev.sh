#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

missing=0
for command_name in go g++ python3 javac java go-judge; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "缺少依赖：$command_name" >&2
    missing=1
  fi
done
if (( missing )); then
  echo "请安装依赖后重试；Debian/Ubuntu 可安装 build-essential python3 default-jdk-headless golang。" >&2
  exit 1
fi

if [[ ! -f /sys/fs/cgroup/cgroup.controllers ]]; then
  echo "警告：当前不是 cgroup v2；go-judge 的资源限制可能不生效。" >&2
fi
if command -v systemctl >/dev/null 2>&1; then
  systemctl is-system-running >/dev/null 2>&1 || echo "警告：systemd 未正常运行；WSL2 请启用 systemd 与 cgroup v2。" >&2
fi
if [[ -z "${JUDGE_CPUSET:-}" ]]; then
  echo "警告：JUDGE_CPUSET 为空，判题计时可能不稳定。" >&2
fi
if [[ "$(id -u)" -ne 0 ]]; then
  echo "提示：非 root 启动时，请确认当前用户获得 systemd/cgroup 委派；否则 go-judge 会拒绝无 cgroup 的执行路径。" >&2
fi

mkdir -p .cache/data
SANDBOX_PORT="${SANDBOX_PORT:-5050}"
SANDBOX_URL="http://127.0.0.1:${SANDBOX_PORT}"
export SANDBOX_TOKEN="${SANDBOX_TOKEN:-$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')}"
if [[ -z "$SANDBOX_TOKEN" || "$SANDBOX_TOKEN" == *$'\n'* ]]; then
  echo "SANDBOX_TOKEN 无效。" >&2
  exit 1
fi
if (( ${#SANDBOX_TOKEN} < 32 )); then
  echo "SANDBOX_TOKEN 至少需要 32 个字符。" >&2
  exit 1
fi

worker_count="${JUDGE_WORKERS:-1}"
if [[ -n "${JUDGE_CPUSET:-}" ]]; then
  worker_count="$(awk -F',' '{print NF}' <<<"$JUDGE_CPUSET")"
fi

sandbox_pid=""
cleanup() {
  if [[ -n "$sandbox_pid" ]] && kill -0 "$sandbox_pid" 2>/dev/null; then
    kill -TERM "$sandbox_pid" 2>/dev/null || true
    wait "$sandbox_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

if curl --silent --fail --max-time 2 -H "Authorization: Bearer $SANDBOX_TOKEN" "$SANDBOX_URL/version" >/dev/null 2>&1; then
  echo "复用已在 $SANDBOX_URL 运行的 go-judge。"
else
  go-judge \
    -http-addr="127.0.0.1:${SANDBOX_PORT}" \
    -auth-token="$SANDBOX_TOKEN" \
    -no-fallback \
    -parallelism="$worker_count" \
    -time-limit-checker-interval=10ms \
    -file-timeout=30m \
    >.cache/go-judge.log 2>&1 &
  sandbox_pid=$!
  sandbox_ready=0
  for _ in $(seq 1 60); do
    if curl --silent --fail --max-time 1 -H "Authorization: Bearer $SANDBOX_TOKEN" "$SANDBOX_URL/version" >/dev/null 2>&1; then
      sandbox_ready=1
      break
    fi
    if ! kill -0 "$sandbox_pid" 2>/dev/null; then
      echo "go-judge 启动失败，日志：.cache/go-judge.log" >&2
      exit 1
    fi
    sleep 0.25
  done
  if (( ! sandbox_ready )); then
    echo "go-judge 未在限定时间内就绪，请查看 .cache/go-judge.log。" >&2
    exit 1
  fi
fi

export PORT="${PORT:-8080}"
export DB_PATH="${DB_PATH:-./.cache/data/oj.db}"
export SANDBOX=gojudge
export SANDBOX_URL
export JUDGE_WORKERS="$worker_count"
export RERUN_BORDERLINE="${RERUN_BORDERLINE:-true}"
export ALLOW_REGISTER="${ALLOW_REGISTER:-true}"
export COOKIE_SECURE="${COOKIE_SECURE:-auto}"

echo "开发服务启动：$SANDBOX_URL，Web：http://127.0.0.1:$PORT"
echo "未配置 JUDGE_CPUSET 时结果仅用于功能联调；Linux 计时基准请使用独占核对 cmd/bench 复测。"
go run ./cmd/oj -dev
