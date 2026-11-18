# 知序 OJ

知序 OJ 是一套第一期可运行的在线判题系统：Go 主服务负责账号、题库、提交记录和判题调度，独立 go-judge 服务负责所有用户代码的编译与执行。前端是零依赖、可离线运行的原生 HTML/CSS/ES Modules，由 Go 二进制嵌入。

本期包含：

- 用户名密码注册、登录、退出、改密码、会话撤销和首个管理员初始化。
- 管理员题目 CRUD、测试数据、样例、标签、分语言限制、模板和参考答案验证。
- 用户题库筛选、做题工作台、自测、提交、结果详情、提交历史和草稿恢复。
- C++17、Python 3、Java 三语言配置；判题使用 CPU 时间，支持 worker 独占逻辑核。
- SQLite 纯 Go 驱动、Linux systemd 与 Docker Compose 部署模板、计时基准工具。
- 五道示例题，每题 8 个用例（含 2 个样例）和三种语言的参考答案。

## 目录

```text
cmd/oj/                 Web 主程序
cmd/bench/              真实判题路径的 CPU 计时稳定性工具
internal/auth/          密码、会话、CSRF、权限、登录限流、用户管理
internal/config/        环境变量与校验
internal/store/         SQLite 访问层和内嵌 migration
internal/judge/         队列、worker、比较器、语言配置、结果映射
internal/judge/sandbox/ Sandbox 接口、go-judge REST 客户端、isolate 占位
internal/server/        net/http 路由、API 校验、静态文件
web/                    原生前端
seed/                   内嵌示例题和 15 份参考答案
scripts/                Windows / Linux 开发脚本和 Debian 部署脚本
deploy/                 systemd、Docker Compose、Linux 调优说明
```

主程序只通过 HTTP 调用 go-judge，不执行 `g++`、Python、Java 或任何用户进程。Windows 开发机发起的编译器探测也发生在 go-judge 中。

沙箱请求字段已按 2026-11 的官方 [REST API 文档](https://docs.goj.ac/api) 和 [示例](https://docs.goj.ac/example) 核对；发布包基线参考 `criyle/go-judge v1.14.1`。核心字段包括 `cmd`、`cpuLimit`、`clockLimit`、`memoryLimit`、`stackLimit`、`procLimit`、`cpuSetLimit`、`copyIn`、`copyOutCached`、`max`、`files`、`fileIds` 与 `fileError`。

## 快速启动

### Debian 12 / 13 一键部署

把 [deploy-debian.sh](https://raw.githubusercontent.com/Half-life7/_OJ/main/scripts/deploy-debian.sh) 下载或传给部署人员。在启用 systemd 和 cgroup v2 的 Debian 主机上，进入脚本所在目录执行：

```bash
bash deploy-debian.sh
```

按菜单选择部署方式。中国大陆服务器可以直接回车使用国内软件源。有项目源码时可直接在项目根目录运行 `bash scripts/deploy-debian.sh`。只有脚本时，可以让它从 GitHub 下载源码，或提供 GitHub 下载的源码 `tar.gz` 归档，脚本会将源码解压到当前目录下的 `_OJ/`。

脚本会按需请求 sudo 权限、安装 Docker Engine、Buildx 与 Compose，然后构建 OJ、go-judge 和 C++17、Python 3、Java 工具链。首次运行生成 `deploy/.env`，保存随机沙箱 token 和初始管理员密码。脚本等待两个服务就绪，再从宿主机访问 `/api/health` 确认判题可用。

默认站点地址是服务器上的 `http://127.0.0.1:8080`。在自己的电脑上执行 `ssh -N -L 8080:127.0.0.1:8080 用户名@服务器地址`，即可在本地浏览器访问 `http://127.0.0.1:8080`。管理员默认用户名为 `admin`，初始密码可在服务器上查看：

```bash
sudo sed -n 's/^ADMIN_PASSWORD=//p' deploy/.env
```

初始密码仅在数据库首次创建管理员时使用。公网访问可在主机上配置 HTTPS 反向代理，将请求转发到 `127.0.0.1:8080`，并在 `deploy/.env` 设置 `COOKIE_SECURE=true` 和实际代理来源 IP 对应的 `TRUSTED_PROXY`。如果要改宿主机端口，在该文件修改 `PORT` 后重跑部署脚本。

更新代码后重跑同一脚本，现有 `deploy/.env` 和 `oj-data` 数据卷会继续使用。以下命令分别用于查看状态、日志和停止服务，均从项目根目录执行：

```bash
sudo docker compose --env-file deploy/.env -f deploy/docker-compose.yml ps
sudo docker compose --env-file deploy/.env -f deploy/docker-compose.yml logs --tail=100
sudo docker compose --env-file deploy/.env -f deploy/docker-compose.yml stop
```

#### 中国大陆服务器的下载源

Docker 安装包、Docker 基础镜像和构建依赖使用不同的下载源。服务器能访问 Docker Hub 或已配置可用的镜像加速时，在部署菜单选择 **1 国内软件源** 即可。清华镜像提供 Docker CE 和 Debian 软件包，Go 模块由 Goproxy.cn 下载，校验保持开启。[Docker CE 镜像](https://mirrors.tuna.tsinghua.edu.cn/help/docker-ce/) · [Debian 镜像](https://mirrors.tuna.tsinghua.edu.cn/help/debian/) · [Goproxy.cn](https://goproxy.cn/)

`DOCKER_APT_URL` 用于脚本配置的 Docker CE 软件源。宿主机的 Debian 软件源和系统管理员已有的 Docker 源由宿主机管理。`DEBIAN_MIRROR_BASE` 只在构建镜像时替换容器内的 Debian 和 Debian Security 源，APT 仍校验软件包的签名索引。

Docker Hub 连不通时，可以在 Docker daemon 配置自己的可信镜像缓存，或在菜单选择 **3 自定义基础镜像仓库** 并输入三个完整镜像地址。下面的地址是示例，需替换为已经同步这三个镜像的仓库。[Docker Hub 镜像缓存文档](https://docs.docker.com/docker-hub/image-library/mirror/)

```dotenv
GO_IMAGE=registry.example.com/library/golang:1.23-bookworm
DEBIAN_IMAGE=registry.example.com/library/debian:bookworm-slim
GO_JUDGE_IMAGE=registry.example.com/criyle/go-judge:latest
```

如需保存自定义地址以便后续部署，可以在 `deploy/.env` 写入上述三个变量和 `GOPROXY`、`DEBIAN_MIRROR_BASE`。下次运行选择 **2 使用现有配置或官方默认源**。

#### 离线部署

在能正常下载依赖、与目标服务器架构相同的机器上，从项目根目录构建并导出成品镜像：

```bash
docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml build
docker image save -o ../oj-images.tar oj-oj:latest oj-sandbox:latest
```

将 `deploy-debian.sh`、`oj-images.tar` 和同版本的源码 `tar.gz` 传到目标服务器。目标服务器需预装 Docker Engine、Compose、`curl` 和 `openssl`。执行 `bash deploy-debian.sh`，选择 **4 离线部署**，按提示输入源码归档和镜像归档路径。脚本检测到已有源码或镜像时会复用。部署仍会生成或复用 `deploy/.env` 并等待健康检查。[Docker 镜像导出](https://docs.docker.com/reference/cli/docker/image/save/) · [Docker 镜像导入](https://docs.docker.com/reference/cli/docker/image/load/)

### Windows + WSL2 推荐路径

1. 安装 Ubuntu 22.04 或更新版本的 WSL2。
2. 在 WSL 的 `/etc/wsl.conf` 启用 systemd，然后执行 `wsl --shutdown` 并重新进入：

   ```ini
   [boot]
   systemd=true
   ```

3. 在 Ubuntu 中安装依赖：

   ```bash
   sudo apt update
   sudo apt install -y build-essential python3 default-jdk golang-go curl
   # 从 https://github.com/criyle/go-judge/releases 下载 Linux amd64 二进制，
   # 命名为 go-judge，赋予可执行权限并放入 PATH。
   ```

4. 在仓库根目录启动：

   ```powershell
   $env:ADMIN_PASSWORD = "请替换为至少 8 位的强密码"
   .\scripts\dev.ps1 -SandboxMode WSL -WslDistribution Ubuntu
   ```

5. 打开 `http://127.0.0.1:8080`。数据库默认写入 `./data/oj.db`，示例题在空题库时自动导入。

`dev.ps1` 会检查 WSL 中的 g++、python3、javac、java、go-judge、cgroup v2 和 systemd，生成随机沙箱 token，启动 go-judge 与 `go run ./cmd/oj -dev`。脚本退出时停止由它启动的 go-judge。

Windows 原生版 go-judge 只适合快速冒烟。它不能用于 Linux 计时、cgroup 内存统计或隔离结论，且通常需要把 `languages.yaml` 中的 `/usr/bin/...` 改成 Windows 可执行文件路径。

### Linux / WSL Ubuntu

```bash
chmod +x scripts/dev.sh
export ADMIN_PASSWORD='请替换为至少 8 位的强密码'
./scripts/dev.sh
```

脚本会验证工具链和 cgroup v2，在 `127.0.0.1:5050` 启动带 token 的 go-judge，然后在 `127.0.0.1:8080` 启动开发服务。日志位于 `.cache/go-judge.log`。

### 手工启动

```bash
go-judge \
  -http-addr=127.0.0.1:5050 \
  -auth-token="$SANDBOX_TOKEN" \
  -no-fallback \
  -parallelism=2 \
  -time-limit-checker-interval=10ms \
  -file-timeout=30m
```

另一个终端：

```bash
export PORT=8080
export DB_PATH=./data/oj.db
export SANDBOX=gojudge
export SANDBOX_URL=http://127.0.0.1:5050
export SANDBOX_TOKEN='与 go-judge 相同的 token'
export ADMIN_USERNAME=admin
export ADMIN_PASSWORD='至少 8 位的强密码'
export JUDGE_CPUSET=2,3,4,5
go run ./cmd/oj -dev
```

生产构建：

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o oj ./cmd/oj
```

前端、默认 SQLite migration 和默认语言配置均嵌入二进制。默认语言配置仅在 `LANGUAGES_FILE` 指向的文件不存在时使用；在仓库开发时仍建议保留根目录 `languages.yaml`，这样无需重新编译即可调整语言。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8080` | HTTP 监听端口。 |
| `DB_PATH` | `./data/oj.db` | SQLite 文件；启动时自动创建目录和 migration。 |
| `SANDBOX` | `gojudge` | `gojudge` 或 `isolate`；本期 `isolate` 明确失败关闭。 |
| `SANDBOX_URL` | `http://127.0.0.1:5050` | go-judge REST 地址，仅允许 HTTP(S) URL。 |
| `SANDBOX_TOKEN` | 空 | 与 go-judge `-auth-token` 相同；生产环境必须设置。 |
| `JUDGE_WORKERS` | cpuset 核数；否则 CPU 核数的一半，最少 1 | 判题槽位数。不得多于 cpuset 核数。 |
| `JUDGE_CPUSET` | 空 | 如 `2,3,4,5`。每个 worker 固定一个逻辑核；为空时启动日志警告计时可能不稳定。 |
| `RERUN_BORDERLINE` | `true` | TLE 且 CPU 时间不超过时限 1.5 倍时，每用例最多重测一次并取较小值。 |
| `LANGUAGES_FILE` | `./languages.yaml` | 外部语言配置；不存在时使用嵌入默认配置。 |
| `MAX_CODE_BYTES` | `65536` | 提交和自测源码字节上限。 |
| `ALLOW_REGISTER` | `true` | 是否开放注册。 |
| `ADMIN_USERNAME` | `admin` | 库中没有管理员时用于创建首个管理员。 |
| `ADMIN_PASSWORD` | 空 | 首个管理员密码。为空时生成随机密码，并只在首次创建成功后的日志中打印一次。 |
| `COOKIE_SECURE` | `auto` | `auto`、`true` 或 `false`；`auto` 在 HTTPS 或可信代理声明 HTTPS 时开启 Secure。 |
| `TRUSTED_PROXY` | 空 | 单个反向代理 IP。只有来自该 IP 的请求才信任 `X-Forwarded-For` / `X-Forwarded-Proto`。 |
| `ARGON_MEMORY_KB` | `19456` | Argon2id 内存，KiB；最低 19 MiB。 |
| `ARGON_ITERATIONS` | `2` | Argon2id 迭代次数。 |
| `ARGON_PARALLELISM` | `1` | Argon2id 并行度。 |
| `DEV` | `false` | 等价于也可用 `-dev` 开启的前端磁盘模式。 |

开发模式 `-dev` 每次请求从 `web/` 读取静态资源，修改后刷新浏览器即可。生产构建使用 `go:embed`。

## 账号、权限与首个管理员

用户名是大小写不敏感的 3–20 位 ASCII 字母、数字或下划线。密码为 8–128 个字符。密码使用 Argon2id PHC 字符串存储，参数来自上表；登录成功发现参数旧于当前配置时会自动重新哈希。

会话保存在 SQLite，Cookie 中只放随机会话 token，数据库只存它的 SHA-256。Cookie 为 HttpOnly、SameSite=Lax、默认 30 天。改密码会删除当前用户的其他会话，封禁会立即删除该用户所有会话。登录失败按“IP + 规范化用户名”在 10 分钟内计数，第 5 次失败后返回 429。

所有非 GET/HEAD 请求都必须带 `X-Requested-With: oj`；有 `Origin` 时还必须与 Host 和推导出的协议一致。前端 `api.js` 统一添加请求头。

`-dev` / `DEV=true` 时额外允许来自 `localhost`、`127.0.0.1` 或 `[::1]` 其他端口的静态开发服务器，并响应 CORS 预检；生产模式只接受同一 Origin。

如果数据库中完全没有管理员，服务会使用 `ADMIN_USERNAME` / `ADMIN_PASSWORD` 创建一位。创建后才提交的随机密码只会打印一次。请先保存日志中的密码，再登录修改。

管理员用户管理页位于 `/admin/users`，可搜索、改角色、封禁和解封。系统拒绝封禁自己，也拒绝降级或封禁最后一名有效管理员。

## 题目与语言

管理员在 `/admin/problems/new` 创建题目：

- 基准时间限制范围 1–30000 ms，基准内存 16–2048 MB。
- 每题 3–200 个测试数据、至少 1 个样例、单用例输入/输出各不超过 1 MB、总量不超过 16 MB。
- 支持 `.in` / `.out` 或 `.in` / `.ans` 多文件按文件名主干配对导入。
- 判题方式为 `exact`、`trim`（默认）或 `float`。
- 分语言覆盖时间和内存均为可选，留空使用语言倍率。
- 可保存最多 3 份参考答案；“验证测试数据”会用它们依次运行全部用例。

### 新增或修改语言

编辑 `languages.yaml` 的 `languages` 列表，或在 `LANGUAGES_FILE` 指向的外部文件中修改。主要字段：

| 字段 | 用途 |
| --- | --- |
| `id` / `display` | 稳定的 API ID 和界面名称。 |
| `source` / `artifact` | 沙箱内源码文件名和编译产物文件名。 |
| `compile` | 编译 argv；空数组表示解释型语言，由 `/file` 上传源码缓存。 |
| `run` | 运行 argv；`{memory_mb}` 会替换为有效内存限制。 |
| `probe` | 启动时和恢复探测时在沙箱内执行的命令。 |
| `time_factor` / `time_extra_ms` | 有效 CPU 时限 = 题目基准 × 倍率 + 附加值。 |
| `memory_factor` / `memory_extra_mb` | 有效内存限制采用同样的倍率与附加值。 |
| `proc_limit` | 沙箱进程/线程数限制。 |
| `template` | 该语言默认起始模板。 |
| `highlight` | 前端原生高亮类型。 |

新增语言后要确保 go-judge 容器中实际存在对应工具链，并重启 OJ 以重新加载配置。命令只作为 argv 数据传给 go-judge，不经过 shell；Java 编译配置使用固定、管理员维护的 shell 脚本同时打包内部类。

默认倍率用于抵消解释器与 JVM 开销：C++17 为 `1.0 × + 0ms`，Python 3 为 `3.0 ×`，Java 为 `2.0 × + 64MB`。卡常题应按目标生产判题机调整。修改倍率后执行：

1. 用管理员页面的“验证测试数据”观察参考答案耗时。
2. 在目标 Linux 实机运行 `go run ./cmd/bench -lang cpp17 -n 30`，并对 Python、Java 重复。
3. 根据单机 CPU 时间分布重新制定题目基准，不要直接沿用其他机器的时间限制。

### 新增导航项

只修改 `web/nav.config.js`：

```js
{
  id: 'contests',
  label: '比赛',
  icon: 'trophy',
  path: '/contests',
  enabled: true,
  inNavbar: true,
  requireAuth: false,
  roles: [],
  children: [
    { id: 'contests-all', label: '全部比赛', path: '/contests' },
  ],
}
```

`enabled:false` 不渲染；`roles` 控制角色；`inNavbar:false` 只进入侧栏体系；当前子项按“路径 + query”精确匹配。图标优先使用 `web/ui.js` 内联 SVG 集合，未知名称会回退为代码图标。

## 判题流程与计时

提交写入 SQLite 后进入有界队列，worker 按顺序编译并运行。每个 worker 独占 `JUDGE_CPUSET` 中的一个逻辑核；编译、自测、验证和正式判题共用同一个槽位，因此同一核同一时刻只有一个任务。

有效 CPU 时限和内存限制按题目覆盖或语言倍率计算。go-judge 的 `clockLimit` 固定为有效 CPU 时限的 2 倍再加 1000 ms，用于阻止 sleep、阻塞和死锁绕过 CPU 时限。比赛记录使用 go-judge `time` 字段的用户态与内核态 CPU 时间；wall time 只作兜底诊断。数据库和 API 内部使用微秒，同时返回向上取整的毫秒值。

正式提交采用 ACM 顺序停止：首次失败后不再运行后续用例。总体耗时和内存取已执行用例最大值。`case_results` 只对公开样例保留输入、期望和实际输出，且各截断到 2KB；隐藏用例在存储层和 API 层各清除一次数据。编译输出上限 4KB 并替换沙箱绝对路径；运行输出 collector 上限 1MB，超出映射为 OLE。

队列容量为 128，每个用户同时处于 `pending` / `judging` 的提交最多 3 个。服务重启会把遗留的 `pending` / `judging` 重新入队。关闭服务时先停止接收新提交，并让已接收任务在 90 秒宽限窗口内完成；超时后才取消后端请求。

## 性能基准

```bash
go run ./cmd/bench -lang cpp17 -n 30
go run ./cmd/bench -lang python3 -n 30
go run ./cmd/bench -lang java -n 30
```

工具默认读取与 OJ 相同的 `SANDBOX_URL`、`SANDBOX_TOKEN`、`JUDGE_CPUSET`、`JUDGE_WORKERS` 和 `languages.yaml`。它会先预热，然后分别运行单任务序列和 `JUDGE_WORKERS` 并发序列，输出 CPU 时间的 min、mean、max、总体标准差和变异系数 CV，并比较并发均值偏移。

在独占核 Linux 实机上，参考目标是单独运行 CV 小于 3%，并发与单独均值差异小于 5%。Windows、WSL2 或检测到虚拟化、未绑核、非 performance governor、Turbo 或 SMT 干扰时，工具会给出警告；这些环境不能作为卡常题最终依据。

详细的 Linux 内核、cgroup、cpuset、SMT、Turbo、swap 与隔离检查见 [deploy/LINUX.md](deploy/LINUX.md)。

## 测试

```bash
go test ./...
go vet ./...
```

默认测试使用内存 SQLite 和 fake sandbox，不需要 go-judge。真实沙箱测试显式开启：

```bash
export SANDBOX_URL=http://127.0.0.1:5050
export SANDBOX_TOKEN='...'
export OJ_SANDBOX_INTEGRATION=1
go test -count=1 ./internal/judge/sandbox ./seed
```

该组测试会覆盖三语言 AC、错误答案候选、编译失败、运行失败、CPU 死循环 TLE、sleep TLE、MLE、OLE，以及五题十五份参考答案全部 AC。Python 没有独立编译阶段，语法错误按运行时错误处理。

Linux 独占核计时测试：

```bash
export OJ_TIMING_TEST=1
export OJ_TIMING_CV_PERCENT=5
export JUDGE_CPUSET=2,3,4,5
go test -count=1 -run TestCPUTimingStabilityOnLinux ./internal/judge
```

浏览器冒烟脚本（本仓库已准备，但当前交付环境没有 go-judge）：

```powershell
$env:BROWSER_PATH='C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe'
$env:OJ_TEST_URL='http://127.0.0.1:8088'
$env:OJ_TEST_ADMIN_PASSWORD='...'
node tools/check_browser.cjs
```

## Linux、Docker 与 systemd

手工运行 Docker Compose（Debian 可直接使用上文的一键部署脚本）：

```bash
cd deploy
cp .env.example .env
# 修改 SANDBOX_TOKEN 与 ADMIN_PASSWORD，并按机器设置 JUDGE_CPUSET
docker compose up -d --build
```

生产环境应固定 go-judge 镜像 digest、只通过内部网络连接沙箱、在 HTTPS 反向代理后设置 `COOKIE_SECURE=true` 和 `TRUSTED_PROXY`。Compose 没有暴露沙箱端口；`privileged: true` 是 go-judge 使用 namespace、mount 和 cgroup 的条件，也意味着沙箱容器本身需要严格主机隔离。

systemd：

```bash
sudo install -d /opt/oj /opt/go-judge /etc/oj
sudo useradd --system --home /nonexistent --shell /usr/sbin/nologin oj 2>/dev/null || true
sudo install -d -o oj -g oj -m 0750 /var/lib/oj
sudo install -m 0755 oj /opt/oj/oj
sudo install -m 0644 languages.yaml /opt/oj/languages.yaml
sudo install -m 0755 go-judge /opt/go-judge/go-judge
sudo install -m 0644 deploy/oj.service /etc/systemd/system/oj.service
sudo install -m 0644 deploy/go-judge.service /etc/systemd/system/go-judge.service
sudo install -m 0600 deploy/oj.env.example /etc/oj/oj.env
sudo install -m 0600 deploy/go-judge.env.example /etc/oj/go-judge.env
# 编辑 token、管理员密码和 cpuset；确认 go-judge 容器/主机内已安装三语言工具链
sudo systemctl daemon-reload
sudo systemctl enable --now go-judge oj
```

`go-judge.service` 以 root 运行并设置 `Delegate=yes`，让 systemd 创建可委派的 cgroup v2 子树。不要给 `oj.service` 增加任何执行用户代码的能力。

## 数据与安全边界

- SQLite 使用 WAL、foreign keys、busy timeout 和串行单连接；判题结果与 `user_problem_status` 在同一事务提交。
- 删除题目会级联删除测试数据、模板、限制和参考答案；历史提交保留题目编号与当时的标题。
- 所有用户内容均通过 DOM 文本节点渲染。Markdown 不解析原始 HTML，链接只允许 `http`、`https` 和 `mailto`。
- 请求体、源码、自测输入、单用例和单题总量均有上限；错误响应不返回堆栈、沙箱地址或 token。
- 生产 go-judge 只监听 `127.0.0.1` 或 Docker 内部网络，并必须启用 `-auth-token`。
- 最终安全性与资源限制必须在 Linux cgroup v2 实机验证。仅启动成功不能证明 cgroup 生效。

## 当前验证状态

本交付环境有 Go、g++、Python、Java 与 Node，但没有可用的 WSL、go-judge 二进制或 Docker daemon。因此：

- 已实测通过：`go test -count=1 ./...`。
- 已实测通过：`go vet ./...`。
- 已实测通过：auth、judge、store、server 的 `go test -race`。
- 已实测通过：Windows 原生构建和 `linux/amd64`、`CGO_ENABLED=0` 交叉构建。
- 已实测通过：所有前端模块以 ES Module 语法解析；Edge 无头浏览器在 1440×1000 和 390×844 下走通公开页、管理员页面、管理员建题并清理、普通用户注册、普通用户管理权限拦截、导航 query/角色状态和横向溢出检查，页面无 JavaScript 异常。
- 已实测通过：`dev.ps1`、`dev.sh` 语法；`docker compose config` 成功解析正式和开发 Compose 文件。没有启动 Docker daemon。
- 未实测：真实 go-judge 的三语言执行、Linux cgroup v2、CPU cpuset、内存峰值、TLE/MLE/OLE 的最终内核行为。
- 未实测：systemd 单元启动、Docker 镜像构建与运行、Linux 实机计时稳定性。
- Windows / WSL2 即使功能通过，也不能作为计时精度或安全隔离结论。

### 验收对照

| # | 验收项 | 当前结果 |
| --- | --- | --- |
| 1 | README 启动、示例题导入、首个管理员 | OJ、空库导入、管理员登录和建题已实测；真实 go-judge 启动未实测。 |
| 2 | 注册、登录、退出、改密码、权限、封禁 | 通过自动测试；浏览器实测注册、自动登录、权限拦截和管理员登录。 |
| 3 | 导航高亮、侧栏切换、配置化、个人菜单 | 通过 Edge 1440/390 浏览器检查，包括 query 子项和 `/admin/users` 分组高亮。 |
| 4 | 管理员题目 CRUD 与验证 | API 测试覆盖 CRUD/公开状态/验证；浏览器实测创建并清理。真实三语言验证数据运行未实测。 |
| 5 | 桌面分栏、手机 Tab | 桌面与 390px 布局实测通过；分隔条拖拽代码已实现，但未做像素级拖拽自动断言。 |
| 6 | 三种参考答案全部 AC | 示例和集成测试已准备；真实 go-judge 环境不可用，未实测。 |
| 7 | WA/CE/RE/两种 TLE/OLE/MLE | 状态映射、比较器和集成测试已准备；真实沙箱未实测。 |
| 8 | Linux bench 稳定性报告 | 工具已交叉编译；独占核 Linux 实机报告未实测。 |
| 9 | 草稿、主题、分栏、侧栏、登录恢复 | localStorage 与登录恢复已实现；登录态和布局在浏览器实测，全部交互组合未逐项自动化。 |
| 10 | 隐藏数据和沙箱本机 token | 隐藏用例 API 测试通过；go-judge 监听/token 配置已写入脚本与部署文件，但真实网络暴露面未实测。 |
| 11 | `go vet`、`go test`、前端控制台 | 全部通过；Edge 冒烟无 JavaScript 异常。 |

## 实现假设

1. 用户名使用 ASCII 字母、数字和下划线；不根据 Unicode 大小写折叠规则扩展用户名规则。
2. Python 3 不设置独立编译阶段；语法错误会作为运行错误返回，这是解释型语言配置的直接结果。
3. Java 公共类名固定为 `Main`；Java 用固定管理员配置将 `*.class` 打包为 `main.jar`，以保留 helper、anonymous 和 nested class。
4. 题目编辑页允许暂无参考答案，但“验证测试数据”要求至少一份参考答案。
5. 样例要求至少一个；创建页默认提供三个测试位置。示例题按更严格的每题至少六个用例、两个样例交付。
6. 删除题目后，提交详情仍可被原提交者和管理员读取；普通题库不再能访问该题。
7. 公开接口返回的 `testcase_count` 是总数，但测试内容只包含样例；隐藏输入、输出和解释不会进入响应。
8. 判题状态采用服务端 snake_case 枚举；前端只展示固定中文映射。
9. “通过率”按题目总提交次数中的 `accepted` 次数计算，而不是去重用户数。
10. 本期只实现单文件程序；Java 的额外顶层类可通过其自身源码限制内的多个类工作，但不支持上传多个源文件。
