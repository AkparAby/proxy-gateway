# 本地代理网关

Windows 上统一接入 SpeedCat 和 Clash Verge，默认 SpeedCat 主用、Clash 备用。HTTP 和 SOCKS5 应用都连接 `127.0.0.1:17890`。

管理页面：<http://127.0.0.1:17880>

## 使用

1. 保持 SpeedCat 和 Clash Verge 的代理服务运行。
2. 启动 `Start-Gateway.ps1`，或直接运行 `proxy-gateway.exe`。当前已开启网关的用户登录自启动。
3. 在管理页查看出口状态、调整策略，按需启用或停用应用接入。

| 组件 | 地址 | 用途 |
|---|---|---|
| 应用统一代理 | `127.0.0.1:17890` | HTTP、HTTP CONNECT、SOCKS5 |
| 管理页面 | `127.0.0.1:17880` | 设置、状态、事件、恢复 |
| SpeedCat | `127.0.0.1:17891` | 默认主出口 |
| Clash Verge | `127.0.0.1:7892` | 默认备用出口 |
| 内核控制接口 | `127.0.0.1:17881` | 随机令牌保护，仅管理程序使用 |
| WSL 接入口 | WSL 虚拟网卡 IP 的 `17890` | 只在启用 WSL 接入时监听；仅允许该虚拟子网 |

两个客户端的系统代理、TUN、代理守护均保持关闭，由网关统一管理 Windows 系统代理。客户端内部的节点选择和分流规则继续生效。不要把客户端自身的上游地址指回网关，否则会形成循环。

## 主备策略

- 默认每轮完成后等待 10 秒再检测，每个出口通过两个 HTTPS 测试地址检测，任一返回 2xx 即为本轮成功；每个请求超时 3 秒。
- 已知健康的出口连续失败 3 轮才判定不可用；首次启动的未知出口必须先检测成功。
- 主出口故障且备用健康时，新的连接切到备用。
- 默认主出口连续健康 60 秒后自动切回；中间一次检测失败会重新计时。
- 可以选择“保持备用，手动切回”。“现在切回主出口”保留自动故障切换模式；“固定出口”则不使用另一出口兜底。
- 两个出口均故障时，外网代理请求走 REJECT；本机、私有 IP 和显式直连域名仍按直连规则处理。
- 不重放业务请求，不迁移已经建立的 TCP 连接。失败的上传、下载、SSH、WebSocket 或流式回答需要应用自行重连。
- 目前实现 TCP 代理；没有承诺 UDP/QUIC、游戏、全机 TUN 或按任意进程强制接管。

## 应用接入与范围

### 浏览器和 Codex

Windows 系统代理指向 `127.0.0.1:17890`，适用于遵循该设置的应用。网关中的连接记录可用于核实实际走向。系统代理不是逐个进程的勾选列表，不遵循它的程序应使用自己的代理选项或启动环境。

Codex 已运行的子进程可能保留旧环境；重新打开终端或重启应用后，新的进程才能继承用户级环境变量。本次没有强制退出用户的 Codex 会话。

### Git 和终端

管理页分别设置 Git 的 HTTP 代理和用户级 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY`、`NO_PROXY`。HTTPS_PROXY 的值仍为 `http://127.0.0.1:17890`，HTTPS 通过 CONNECT 隧道传输，不是给本地代理启用 TLS。

Git SSH 不受 `http.proxy` 控制；已有 SSH 配置未修改。私有 IP 请求在网关中直连。使用私有域名的内网服务，可以将域名后缀加入管理页的直连列表。

### Docker Desktop

- Desktop 已使用 Windows 系统代理；镜像拉取由 Desktop 自己的代理层处理。
- 本次修正了 Desktop 的容器/镜像拉取代理模式为 `system`，并在确认没有运行容器后重启验证。以后在管理页切换 Docker 接入，Desktop 代理模式需要重启 Desktop 才生效；管理页不会自动中断运行中的容器。
- 管理页的 Docker 开关修改当前用户 `~/.docker/config.json` 中的 `proxies.default`，地址为 `http://host.docker.internal:17890`。保留凭据及其他无关设置。
- 该配置影响后续构建和新建容器，不改变已经运行的容器。
- 容器内程序需要支持 HTTP(S)_PROXY。Linux Docker Engine 与 Docker Desktop 的配置机制不同；本项目针对本机 Docker Desktop。
- Docker 自定义网络或构建器可能需要额外的 host-gateway 映射，应以该构建器中的实际连通性为准。

### WSL Ubuntu

当前为 NAT 网络模式。启用后，在 Ubuntu 的 `~/.config/local-proxy-gateway/` 中安装环境文件，并在 `.profile`、`.bashrc` 添加可禁用的加载行。通过默认路由自动查找 Windows 地址，不要求修改 `.wslconfig` 或重启 Docker/WSL。

新登录/交互 shell 自动生效。现有 shell 可执行：

```sh
. ~/.config/local-proxy-gateway/env.sh
```

停用后重新打开 shell，已有进程的环境不会被强制修改。WSL 接入口仅绑定 WSL 虚拟网卡，不在物理网卡或 Tailscale 网卡监听；网卡地址变化会在下一轮检测时更新。若将来切换镜像网络模式，应重新验证接入地址。

## 恢复

管理页可单独“停用并恢复”，或使用“恢复接入设置并停止”。若网关程序无法打开，运行 `Restore-Proxy.ps1` 恢复由接入功能管理的设置。

- `runtime/integrations-state.json`：每项首次启用前的原值；恢复只涉及网关管理的配置项。
- `backups/20261119-104736/`：实施前的 SpeedCat、Clash Verge、Docker Desktop、Windows 系统代理和 Git 配置备份。
- 停用 WSL 留下惰性的加载文件，使用标记文件关闭它；不覆盖之后用户对 shell 配置的其他编辑。
- 恢复接入设置不会把 SpeedCat 端口改回与 Clash 冲突的 7892。如需完全撤销客户端修改，可在客户端退出后按备份恢复；完整文件恢复会同时覆盖后续配置变更。

不要上传 `backups/` 或 `runtime/`：它们可能包含原有客户端配置和本地控制令牌。项目的 `.gitignore` 已排除这些目录。

## 构建与验证

依赖：Windows、Go 1.26、PowerShell、已安装的 Mihomo 内核。优先使用本机 PowerShell 7，兼容脚本需以 UTF-8 BOM 保存才能由 Windows PowerShell 5 正确读取中文。

Git 仓库只保存源码、脚本和文档，不包含本机运行状态、配置备份或编译产物。首次克隆后，需要准备独立内核文件；若本机已安装 Clash Verge，可在项目根目录执行：

```powershell
New-Item -ItemType Directory -Path runtime -Force
Copy-Item -LiteralPath 'C:\Program Files\Clash Verge\verge-mihomo.exe' -Destination runtime\mihomo.exe
```

安装路径不同时，使用实际内核路径。随后构建并运行启动脚本，首次运行会生成本机配置文件。

```powershell
go test ./...
go vet ./...
go build -ldflags '-H=windowsgui' -o proxy-gateway.exe .
```

更新正在运行的版本时，先在管理页恢复并停止，再替换程序。不要直接覆盖正在使用的可执行文件。

`scripts/verify-failover.mjs` 使用临时 TCP 转发端口 17892/17893，模拟出口可监听但无法联网的故障，验证失败阈值、备用访问、恢复等待、保持备用、手动回切和双出口拒绝。它会暂时修改正在运行的网关配置并在 finally 中恢复，可能打断活跃连接，适合维护时执行。结果保存在 `runtime/failover-results.json`。

```powershell
node scripts/verify-failover.mjs
wsl -d Ubuntu -- sh /mnt/d/code/tools/proxy-gateway/scripts/smoke-wsl.sh
```

管理接口限制 Host、Origin 和写请求令牌。内核异常退出会由管理程序重启，重新检查出口后恢复转发。管理程序本身退出时，用户级登录自启动并不等于持续服务监控；如果整个管理程序异常终止，需要重新运行启动脚本。

### 本机验收（2026-11-19）

- `go test ./...` 和 `go vet ./...` 通过。
- 受控故障演练覆盖主备切换、防抖、自动恢复、保持备用、手动回切和双出口失败拒绝，结果见 `runtime/failover-results.json`。
- HTTP、SOCKS5、WSL、Docker 容器以及 Docker 构建的 HTTPS 检测均返回 204；Docker 镜像拉取和 Git `ls-remote` 成功。
- 已按本机连接端口关联到 ChatGPT 桌面进程，确认其 OpenAI/ChatGPT 连接经过 `SpeedCat > Gateway`。
- 终止独立 Mihomo 内核后，管理程序自动启动新内核并恢复健康状态；没有终止两个客户端或当前 Codex 会话来进行该项测试。
- 管理页的策略保存、状态显示和接入开关状态已核对，截图见 `runtime/dashboard.jpg`。恢复流程已在接入阶段实际执行；随后重新启用了各项接入。

## 内核来源

`runtime/mihomo.exe` 复制自本机 Clash Verge 的 `verge-mihomo.exe`，版本 `Mihomo Meta v1.19.33 windows amd64`。独立配置目录确保不覆盖 Clash Verge 的配置。

Mihomo 是 GPL-3.0 项目：<https://github.com/MetaCubeX/mihomo>。如分发本地运行包，需履行内核许可证与对应源代码提供义务；本项目没有下载或转换用户订阅。

技术依据：[Mihomo API](https://wiki.metacubex.one/api/)、[WSL 网络](https://learn.microsoft.com/en-us/windows/wsl/networking)、[Docker Desktop 代理设置](https://docs.docker.com/desktop/settings-and-maintenance/settings/)。
