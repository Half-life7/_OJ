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
    printf '用法：bash deploy-debian.sh\n支持 Debian 12/13 和 Ubuntu 22.04。直接运行并按提示选择部署方式。\n--offline 可用于自动化，使用预装 Docker 和已导入的成品镜像。\n'
    exit 0
    ;;
  --offline) offline=true ;;
  '') ;;
  *) fail '请使用 --help 查看用法。' ;;
esac
[[ $# -eq 0 || ( $# -eq 1 && $1 == --offline ) ]] || fail '请使用 --help 查看用法。'

if [[ $EUID -ne 0 ]]; then
  command -v sudo >/dev/null || fail '请使用 root 账号运行，或安装 sudo。'
  exec sudo bash -- "${BASH_SOURCE[0]}" "$@"
fi

[[ -r /etc/os-release ]] || fail '无法识别系统版本。'
source /etc/os-release
distribution=${ID:-}
codename=${VERSION_CODENAME:-${UBUNTU_CODENAME:-}}
case "$distribution:$codename" in
  debian:bookworm|debian:trixie|ubuntu:jammy) ;;
  *) fail '需要 Debian 12（bookworm）、Debian 13（trixie）或 Ubuntu 22.04（jammy）。' ;;
esac
[[ -d /run/systemd/system ]] || fail '需要由 systemd 启动的 Linux 主机。'
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
launch_dir=$PWD
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)

interactive=false
if [[ $# -eq 0 && -t 0 ]]; then
  interactive=true
  printf '%s\n' '请选择部署方式' \
    '  1) 国内软件源（需要能拉取 Docker Hub 镜像）' \
    '  2) 使用现有配置或官方默认源' \
    '  3) 国内软件源 + 自定义基础镜像仓库' \
    '  4) 离线部署（使用现成的 Docker 镜像归档）'
  while true; do
    read -r -p '请输入编号 [1]：' mode || fail '未选择部署方式。'
    case "${mode:-1}" in
      1|2|3|4) mode=${mode:-1}; break ;;
      *) printf '请输入 1 到 4 之间的编号。\n' ;;
    esac
  done
  if [[ $mode == 1 || $mode == 3 ]]; then
    export DOCKER_APT_URL="${DOCKER_APT_URL:-https://mirrors.tuna.tsinghua.edu.cn/docker-ce/linux/$distribution}"
    export DEBIAN_MIRROR_BASE="${DEBIAN_MIRROR_BASE:-http://mirrors.tuna.tsinghua.edu.cn}"
    export GOPROXY="${GOPROXY:-https://goproxy.cn}"
  fi
  if [[ $mode == 3 ]]; then
    printf '请输入仓库中已同步的完整镜像地址（含标签或 digest）。\n'
    read -r -p 'Go 构建镜像（golang:1.23-bookworm）：' GO_IMAGE || fail '未输入 Go 镜像地址。'
    read -r -p 'Debian 运行镜像（debian:bookworm-slim）：' DEBIAN_IMAGE || fail '未输入 Debian 镜像地址。'
    read -r -p 'go-judge 沙箱镜像（criyle/go-judge:latest）：' GO_JUDGE_IMAGE || fail '未输入 go-judge 镜像地址。'
    [[ -n $GO_IMAGE && -n $DEBIAN_IMAGE && -n $GO_JUDGE_IMAGE ]] || fail '自定义模式需要三个镜像地址。'
    export GO_IMAGE DEBIAN_IMAGE GO_JUDGE_IMAGE
  fi
  if [[ $mode == 4 ]]; then
    offline=true
  fi
fi

is_project() {
  [[ -f $1/go.mod && -f $1/deploy/docker-compose.yml && -f $1/deploy/.env.example ]]
}

read_archive() {
  local archive
  read -r -p "$1" archive || fail '未输入归档路径。'
  [[ -n $archive ]] || fail '归档路径不能为空。'
  [[ $archive == /* ]] || archive="$launch_dir/$archive"
  [[ -f $archive && -r $archive ]] || fail "无法读取归档 $archive。"
  printf '%s\n' "$archive"
}

project_dir="$launch_dir/_OJ"
for candidate in "$script_dir/.." "$script_dir" "$launch_dir" "$project_dir"; do
  if is_project "$candidate"; then
    project_dir=$candidate
    break
  fi
done
if ! is_project "$project_dir"; then
  [[ ! -e $project_dir && ! -L $project_dir ]] || fail "$project_dir 已存在，请在新的目录重新运行。"
  source_mode=1
  if $interactive && ! $offline; then
    printf '%s\n' '请选择项目源码来源' '  1) 从 GitHub 下载' '  2) 使用本地源码 tar.gz 归档'
    while true; do
      read -r -p '请输入编号 [1]：' source_mode || fail '未选择源码来源。'
      case "${source_mode:-1}" in
        1|2) source_mode=${source_mode:-1}; break ;;
        *) printf '请输入 1 或 2。\n' ;;
      esac
    done
  fi
  if $offline; then
    $interactive || fail '离线部署需要在项目目录运行，或直接运行脚本并选择本地归档。'
    source_mode=2
  fi

  source_tmp=$(mktemp -d -- "$launch_dir/.oj-source.XXXXXX")
  trap 'rm -rf -- "$source_tmp"' EXIT
  if [[ $source_mode == 2 ]]; then
    source_archive=$(read_archive '请输入源码 tar.gz 路径：')
  else
    if ! command -v curl >/dev/null; then
      apt-get update
      apt-get install -y --no-install-recommends ca-certificates curl
    fi
    source_archive="$source_tmp/source.tar.gz"
    printf '正在从 GitHub 下载项目。\n'
    curl -fsSL --retry 2 --connect-timeout 10 --max-time 60 \
      https://codeload.github.com/Half-life7/_OJ/tar.gz/refs/heads/main -o "$source_archive" ||
      fail 'GitHub 下载失败。请准备源码 tar.gz 后重试，并选择使用本地归档。'
  fi
  (
    umask 022
    mkdir -- "$source_tmp/project" &&
      tar -xzf "$source_archive" --strip-components=1 --no-same-owner --no-same-permissions -C "$source_tmp/project"
  ) || fail '源码归档无法解压。'
  is_project "$source_tmp/project" || fail '归档中未找到完整的 OJ 项目。'
  mv -T -n -- "$source_tmp/project" "$project_dir"
  [[ ! -e $source_tmp/project ]] || fail "$project_dir 已存在，未覆盖该目录。"
  rm -rf -- "$source_tmp"
  trap - EXIT
fi
cd -P -- "$project_dir"

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
       ! grep -rqsE "/linux/$distribution/?([[:space:]]|$)" /etc/apt/sources.list /etc/apt/sources.list.d; }; then
    docker_apt_url=${DOCKER_APT_URL:-https://download.docker.com/linux/$distribution}
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
Suites: $codename
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
    if [[ $distribution == ubuntu || $codename == trixie ]] &&
       dpkg-query -W -f='${db:Status-Status}' docker.io 2>/dev/null | grep -qx installed; then
      case "$distribution" in
        ubuntu) packages+=(docker-compose-v2 docker-buildx) ;;
        debian) packages+=(docker-compose docker-buildx) ;;
      esac
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
  missing_images=()
  while IFS= read -r image; do
    "${docker_cmd[@]}" image inspect "$image" >/dev/null 2>&1 || missing_images+=("$image")
  done <<<"$images"
  if (( ${#missing_images[@]} )); then
    $interactive || fail "缺少镜像 ${missing_images[*]}，请先将成品镜像通过 docker load 导入。"
    printf '需导入镜像：%s\n' "${missing_images[*]}"
    image_archive=$(read_archive '请输入 Docker 镜像归档路径：')
    "${docker_cmd[@]}" image load -i "$image_archive" || fail '镜像归档导入失败。'
    for image in "${missing_images[@]}"; do
      "${docker_cmd[@]}" image inspect "$image" >/dev/null 2>&1 || fail "镜像归档缺少 $image。"
    done
  fi
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
printf '部署完成。服务器访问地址：http://%s\n健康状态：%s\n配置及初始管理员密码：%s/deploy/.env\n' "$address" "$health" "$PWD"
