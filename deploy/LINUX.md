# Linux 判题机部署与性能调优

本文件描述计时稳定性和隔离所需的 Linux 配置。当前工作环境没有 Linux 实机、WSL 或 Docker daemon，因此本目录中的 systemd 与 Compose 文件均是交付配置，**未实测**。

## 推荐基线

- 独占 Linux 实机或 KVM 主机，不使用 WSL2、桌面虚拟化或共享 CI runner 作为卡常题基准。
- 内核 5.19 或更新版本，启用 cgroup v2、`cpu`、`cpuset`、`memory`、`pids` controller。
- systemd 正常运行，go-judge 使用 root 或拥有经 systemd `Delegate=yes` 委派的 cgroup 子树。
- 关闭 swap，或在 cgroup 中确认 `memory.swap.max=0`；go-judge 在 kernel/controller 不支持 swap accounting 时不能保证这项限制。
- 每个 worker 固定独占一个逻辑核，建议每个物理核只使用一个逻辑核。
- Web、SQLite、反向代理和监控与判题核分离。
- 判题机不运行编译缓存清理、备份、扫描、监控抓取或批处理等重负载。

## cgroup v2 检查

```bash
mount | grep cgroup
test -f /sys/fs/cgroup/cgroup.controllers
cat /sys/fs/cgroup/cgroup.controllers
cat /sys/fs/cgroup/cgroup.subtree_control
systemd --version
systemctl is-system-running
```

应看到：

- 挂载点为 `cgroup2`。
- `cgroup.controllers` 至少包含 `cpu cpuset memory pids`。
- `systemctl is-system-running` 返回 `running` 或可解释的 `degraded`。

Ubuntu 22.04+ 默认启用 cgroup v2。升级发行版后若仍有 cgroup v1 混合挂载，先修正内核启动参数和 systemd 配置，再部署 go-judge。

## CPU 与 NUMA 规划

先确认物理核和 SMT 兄弟线程：

```bash
lscpu -e
cat /sys/devices/system/cpu/cpu*/topology/thread_siblings_list
numactl --hardware
```

选择互不共享物理核的逻辑 CPU，例如某机器的第 9–16 号逻辑核均为不同物理核：

```bash
export JUDGE_CPUSET=8,10,12,14
export JUDGE_WORKERS=4
```

`JUDGE_CPUSET` 中每个条目对应一个 worker。worker 数不得超过该列表长度。go-judge 请求中 `cpuSetLimit` 使用单个 CPU 号；主程序会把编译、每个测试点、自测和验证都限制在同一个 worker 的逻辑核上。

### systemd CPUAffinity

若只使用 systemd 统一限制进程可见 CPU，可在独立部署调整中给服务增加 `AllowedCPUs=` 或 `CPUAffinity=`。注意 go-judge 仍需看到 `JUDGE_CPUSET` 内的所有 CPU；不要用服务级 `CPUAffinity=` 错误地把 go-judge 只绑到一个核，否则它会失去在其允许范围内逐任务分配 cpuset 的能力。

### isolcpus

希望减少内核调度干扰时，可考虑 `isolcpus=managed_irq,domain,8,10,12,14`（编号按实际机器替换）并在 GRUB 后重启。不同内核版本对 `isolcpus` 行为有差异，应通过 `taskset -pc $$`、`/proc/cmdline` 和实际 bench 验证。使用 isolcpus 时还要把中断、systemd 管理任务和 Web 服务移出判题核。

`isolcpus`/`CPUAffinity` 不能替代 cpuset 隔离；最终必须检查实际进程的 `Cpus_allowed_list`。

## 频率、Turbo 与 SMT

### Governor

```bash
cpupower frequency-info
sudo cpupower frequency-set -g performance
```

也可检查：

```bash
cat /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor
```

所有判题核应使用 `performance`；若硬件/内核不允许，至少在 README 和题目基准中标注。

### Turbo / Boost

```bash
cat /sys/devices/system/cpu/intel_pstate/no_turbo
cat /sys/devices/system/cpu/cpufreq/boost
```

卡常题需要固定频率和低方差时，应关闭 Turbo/Boost，或在 BIOS、云厂商规格和内核参数允许的方式下锁频。仅设置 governor 为 `powersave` 不能替代锁频。

### SMT

首选 BIOS 关闭 SMT；若必须保留，则为每个 worker 分配不同物理核上的单一逻辑线程，避免兄弟线程同时运行其他负载。`cmd/bench` 会尝试根据 `thread_siblings_list` 给出 SMT 警告。

## swap 与内存

```bash
swapon --show
cat /proc/sys/vm/swappiness
```

生产判题机建议不启用 swap。go-judge 会尽量在 cgroup 中设置 swap 限制；如果内核不暴露 swap accounting，`-no-fallback` 会避免悄悄退回到无法保证 swap 限制的路径。部署后应使用 MLE 集成测试确认实际状态。

## go-judge 权限

Linux 下 go-judge 需要创建 namespace、mount 和 cgroup。官方建议 root 或具有等效 cgroup 权限的运行方式。`deploy/go-judge.service` 以 root 运行并设置：

```ini
Delegate=yes
CPUAccounting=yes
MemoryAccounting=yes
TasksMax=4096
LimitNOFILE=65536
```

不要给服务添加 `ProtectControlGroups=yes`、`PrivateUsers=yes` 或 `NoNewPrivileges=yes`，它们会阻止沙箱所需操作。反之，OJ Web 服务应使用普通用户、只读根文件系统、空 capability set，并只允许写入自己的数据目录。

go-judge 必须只监听本机或内部 Docker 网络，启用 `-auth-token`，并使用 `-no-fallback`，防止 cgroup 不可用时静默退化。

## 启动顺序

1. 配置 `/etc/oj/go-judge.env`：

   ```bash
   SANDBOX_TOKEN=至少32位随机token
   JUDGE_WORKERS=4
   ```

2. 配置 `/etc/oj/oj.env`：

   ```bash
   JUDGE_CPUSET=8,10,12,14
   JUDGE_WORKERS=4
   SANDBOX_TOKEN=与go-judge相同的token
   RERUN_BORDERLINE=true
   ```

3. 启动并检查：

   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable --now go-judge.service
   sudo systemctl enable --now oj.service
   systemctl status go-judge --no-pager
   systemctl status oj --no-pager
   journalctl -u go-judge -u oj -b --no-pager
   curl -fsS http://127.0.0.1:8080/api/health
   ```

`/api/health` 应显示 `sandbox_available:true` 和可用的语言数。若语言探测失败，先在同一个 go-judge 配置下验证 `/usr/bin/g++ --version`、`/usr/bin/python3 --version`、`/usr/bin/javac -version` 和 `/usr/bin/java -version`。

## 验证与基准

```bash
cd /opt/oj
export SANDBOX_URL=http://127.0.0.1:5050
export SANDBOX_TOKEN=...
export JUDGE_CPUSET=8,10,12,14
export JUDGE_WORKERS=4

go run ./cmd/bench -lang cpp17 -n 30
go run ./cmd/bench -lang python3 -n 30
go run ./cmd/bench -lang java -n 30
```

参考目标：

- 单独运行时 CPU 时间 CV 小于 3%。
- worker 并发运行时，并发均值与单独均值差小于 5%。

达不到时检查：

- `JUDGE_CPUSET` 是否实际进入 go-judge 请求的 `cpuSetLimit`。
- 逻辑 CPU 是否来自同一物理核的 SMT 兄弟线程。
- governor、Turbo、温度墙和云主机频率策略。
- WSL2、虚拟机、容器 CPU quota 和宿主机邻居。
- SQLite、备份、监控或反向代理是否占用判题核。
- swap、内存回收和 NUMA 跨节点访问。

基准结果只对本机当前内核、频率、温度和负载有意义。生产题目应使用生产判题机的 bench 和“验证测试数据”结果制定时间限制。

## Docker Compose

`deploy/docker-compose.yml` 中沙箱不暴露端口，只连接内部网络；OJ 默认只发布到 `127.0.0.1`。生产环境需在前方部署 HTTPS 反向代理，并按实际代理 IP 设置 `TRUSTED_PROXY` 与 `COOKIE_SECURE=true`。

```bash
cd deploy
cp .env.example .env
docker compose up -d --build
docker compose ps
docker compose logs --tail=200 sandbox oj
```

该 Compose 文件需要 host Docker daemon、privileged 容器和可委派 cgroup；当前交付环境无法验证。建议生产固定 go-judge digest，并评估把沙箱部署到独立判题机而不是与 Web 共用主机。

## 备份

- 备份 SQLite 时使用 SQLite 在线备份机制，或在短暂停写窗口复制数据库及 WAL/SHM。
- 不备份 go-judge 内存文件缓存；提交结束后会主动释放，服务重启也可以安全丢弃。
- systemd 的 `StateDirectory=oj` 将数据库放在 `/var/lib/oj`；Compose 使用命名卷 `oj-data`。
