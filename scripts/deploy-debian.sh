#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() {
  printf '部署失败：%s\n' "$*" >&2
  exit 1
}

offline=false
case "${1:-}" in
  -h|--help)
    printf '用法：sudo bash scripts/deploy-debian.sh [--offline]\n支持 Debian 12/13，站点设置保存在 deploy/.env。\n--offline 使用预装 Docker 和已导入的成品镜像，仅启动服务。\n'
    exit 0
    ;;
  --offline) offline=true; shift ;;
esac
[[ $# -eq 0 ]] || fail '请使用 --help 查看用法。'
[[ $EUID -eq 0 ]] || fail '请使用 sudo 运行此脚本。'

cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
[[ -r /etc/os-release ]] || fail '无法识别系统版本。'
source /etc/os-release
case "${ID:-}:${VERSION_CODENAME:-}" in
  debian:bookworm|debian:trixie) ;;
  *) fail '需要 Debian 12（bookworm）或 Debian 13（trixie）。' ;;
esac
[[ -d /run/systemd/system ]] || fail '需要由 systemd 启动的 Debian 主机。'
cgroup_controllers=/sys/fs/cgroup/cgroup.controllers
[[ -r $cgroup_controllers ]] || fail '需要启用 cgroup v2。'
controllers=$(<"$cgroup_controllers")
for controller in cpu cpuset memory pids; do
  [[ " $controllers " == *" $controller "* ]] || fail "cgroup v2 缺少 $controller 控制器。"
done

exec 9>/run/lock/oj-deploy.lock
flock -n 9 || fail '已有 OJ 部署正在运行。'

export DEBIAN_FRONTEND=noninteractive
docker_cmd=(docker --host unix:///var/run/docker.sock)

if $offline; then
  for dependency in docker curl openssl; do
    command -v "$dependency" >/dev/null || fail "离线部署需要预装 $dependency。"
  done
elif ! command -v docker >/dev/null; then
  printf '安装 Docker Engine、Buildx 和 Compose。\n'
  docker_source=/etc/apt/sources.list.d/oj-docker.sources
  # Reuse existing sources. Only an explicit override replaces an OJ-managed source.
  if [[ -f $docker_source && -n ${DOCKER_APT_URL:-} ]] ||
     { [[ ! -f $docker_source ]] &&
       ! grep -rqsE '/linux/debian/?([[:space:]]|$)' /etc/apt/sources.list /etc/apt/sources.list.d; }; then
    docker_apt_url=${DOCKER_APT_URL:-https://download.docker.com/linux/debian}
    docker_apt_url=${docker_apt_url%/}
    [[ $docker_apt_url =~ ^https://[[:alnum:].:/_-]+$ ]] || fail 'DOCKER_APT_URL 需要是 HTTPS 仓库地址。'
    if [[ ! -s /etc/ssl/certs/ca-certificates.crt ]] ||
       ! command -v curl >/dev/null || ! command -v gpg >/dev/null || ! command -v openssl >/dev/null; then
      apt-get update
      apt-get install -y --no-install-recommends ca-certificates curl gpg openssl
    fi
    install -m 0755 -d /etc/apt/keyrings
    key_dir=$(mktemp -d)
    trap 'rm -r -- "$key_dir"' EXIT
    curl -fsSL --retry 3 --connect-timeout 10 --max-time 30 "$docker_apt_url/gpg" -o "$key_dir/docker.asc" ||
      fail 'Docker APT 公钥下载失败。请检查网络或设置 DOCKER_APT_URL。'
    fingerprint=$(gpg --batch --no-options --homedir "$key_dir" --with-colons --show-keys "$key_dir/docker.asc" |
      awk -F: '$1 == "pub" { primary = 1 } primary && $1 == "fpr" { print $10; primary = 0 }')
    [[ $fingerprint == 9DC858229FC7DD38854AE2D88D81803C0EBFCD88 ]] || fail 'Docker APT 公钥指纹校验失败。'
    install -m 0644 "$key_dir/docker.asc" /etc/apt/keyrings/oj-docker.asc
    rm -r -- "$key_dir"
    trap - EXIT
    cat >"$docker_source" <<EOF
Types: deb
URIs: $docker_apt_url
Suites: $VERSION_CODENAME
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/oj-docker.asc
EOF
    chmod 0644 "$docker_source"
  fi
  apt-get update
  apt-get install -y --no-install-recommends --no-remove \
    ca-certificates curl openssl docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
else
  packages=()
  command -v curl >/dev/null || packages+=(curl ca-certificates)
  command -v openssl >/dev/null || packages+=(openssl)
  if ! "${docker_cmd[@]}" compose version >/dev/null 2>&1 ||
     ! "${docker_cmd[@]}" buildx version >/dev/null 2>&1; then
    if [[ $VERSION_CODENAME == trixie ]] && dpkg-query -W -f='${db:Status-Status}' docker.io 2>/dev/null | grep -qx installed; then
      packages+=(docker-compose docker-buildx)
    else
      packages+=(docker-compose-plugin docker-buildx-plugin)
    fi
  fi
  if (( ${#packages[@]} )); then
    apt-get update
    apt-get install -y --no-install-recommends --no-remove "${packages[@]}" ||
      fail '请检查现有 Docker 的 APT 软件源和 Compose v2 / Buildx 软件包。'
  fi
fi

systemctl enable --now docker.service
"${docker_cmd[@]}" info >/dev/null || fail '无法连接本机 Docker Engine。'
compose=("${docker_cmd[@]}" compose --env-file deploy/.env -f deploy/docker-compose.yml)
compose_help=$("${docker_cmd[@]}" compose up --help) || fail '需要 Docker Compose v2 或更新版本。'
[[ $compose_help == *--wait-timeout* ]] || fail '请升级 Docker Compose，使其支持 up --wait-timeout。'

if [[ ! -f deploy/.env ]]; then
  env_tmp=$(mktemp deploy/.env.XXXXXX)
  trap 'rm -f -- "$env_tmp"' EXIT
  sandbox_token=$(openssl rand -hex 32)
  admin_password=$(openssl rand -hex 24)
  sed -e "s/^SANDBOX_TOKEN=.*/SANDBOX_TOKEN=$sandbox_token/" \
      -e "s/^ADMIN_PASSWORD=.*/ADMIN_PASSWORD=$admin_password/" \
      deploy/.env.example >"$env_tmp"
  ln -T -- "$env_tmp" deploy/.env || fail '无法创建 deploy/.env，已有配置时请重试。'
  rm -f -- "$env_tmp"
  trap - EXIT
  printf '已生成 deploy/.env。初始管理员密码保存在该文件中。\n'
fi
chmod 0600 deploy/.env
if grep -Eq '^(SANDBOX_TOKEN|ADMIN_PASSWORD)=replace-with-' deploy/.env; then
  fail '请先把 deploy/.env 中的示例 token 和管理员密码换成随机值。'
fi

"${compose[@]}" config --quiet
if $offline; then
  images=$("${compose[@]}" config --images)
  while IFS= read -r image; do
    "${docker_cmd[@]}" image inspect "$image" >/dev/null 2>&1 ||
      fail "缺少镜像 $image，请先将成品镜像通过 docker load 导入。"
  done <<<"$images"
  printf '使用本地镜像启动 OJ 和 go-judge，等待健康检查。\n'
else
  printf '构建 OJ 和 go-judge 镜像。\n'
  "${compose[@]}" build || fail '镜像构建失败。若下载超时，请参阅 README 中的镜像源配置或离线部署。'
  printf '启动 OJ 和 go-judge，等待健康检查。\n'
fi
if ! "${compose[@]}" up --no-build --pull never --wait --wait-timeout 240; then
  "${compose[@]}" ps --all >&2 || true
  fail '容器未就绪。请在 deploy 目录中运行 sudo docker compose logs --tail=100。'
fi

address=$("${compose[@]}" port oj 8080)
case "$address" in
  0.0.0.0:*) address="127.0.0.1:${address##*:}" ;;
  '[::]:'*) address="[::1]:${address##*:}" ;;
esac
health=$(curl --noproxy '*' --fail --silent --show-error --max-time 5 "http://$address/api/health") ||
  fail "容器已启动，但无法通过宿主机端口 $address 访问 OJ。"
[[ $health == *'"sandbox_available":true'* ]] || fail 'OJ 已响应，但判题沙箱暂不可用。'
printf '部署完成。服务器访问地址：http://%s\n健康状态：%s\n' "$address" "$health"
