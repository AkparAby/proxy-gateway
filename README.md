# Proxy Gateway · 本地代理网关

**给 Windows 和 macOS 应用一个固定代理入口，在多个本地代理客户端之间按优先级自动切换。**

[![Release](https://img.shields.io/github/v/release/AkparAby/proxy-gateway)](https://github.com/AkparAby/proxy-gateway/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Windows x64](https://img.shields.io/badge/Windows-x64-0078D4)](https://github.com/AkparAby/proxy-gateway/releases/latest)
[![macOS Apple Silicon](https://img.shields.io/badge/macOS-Apple_Silicon-333333)](https://github.com/AkparAby/proxy-gateway/releases/latest)

Proxy Gateway 使用 **Go + Fyne 桌面界面、独立 Go 管理程序和 Mihomo 内核**。双击程序即可打开中文桌面窗口，并在系统托盘常驻；浏览器管理页作为备用入口保留。支持添加 1–16 个本机 HTTP / SOCKS5 代理出口，自定义名称并调整优先级。应用始终连接同一地址，网关负责检测出口、故障切换和恢复回切。

[下载 Windows / macOS 版](https://github.com/AkparAby/proxy-gateway/releases/latest) · [报告问题](https://github.com/AkparAby/proxy-gateway/issues) · [许可证说明](THIRD_PARTY_NOTICES.md)

## 能做什么

- **统一入口**：HTTP、HTTPS CONNECT 和 SOCKS5 默认共用 `127.0.0.1:17890`，可在“网关设置”中自定义端口。
- **多个代理出口**：添加、删除、改名和上下调整优先级，可连接不同软件提供的本机 HTTP / SOCKS5 端口。
- **自动故障切换**：通过每个出口实际访问 HTTPS 地址，连续失败后按优先级选择健康备用，减少偶发波动导致的切换。
- **可选回切策略**：更高优先级出口稳定恢复后自动切回，或保持当前出口、手动切回首选；也支持固定任一出口。
- **按需接入**：Windows 支持系统代理、终端、Git、Docker 和 WSL；macOS 支持系统代理、Git 和登录自启动。
- **桌面管理**：概览、代理出口、网关设置、切换策略、应用接入和事件六个页面；中文字体内嵌，无需浏览器或 WebView2。
- **托盘运行**：关闭窗口继续代理，再次打开唤起已有窗口；从菜单明确恢复接入后退出。
- **下载即用**：Windows 提供 EXE 便携包；macOS 提供带图标的 `.app`。均附带内核，无需安装 Go、Node.js 或 Python。
- **可恢复设置**：接入功能保存原值；可逐项停用恢复，或恢复后停止网关。可选当前用户登录自启动。

这是主备故障切换工具，目前不做流量轮询、带宽叠加或已有连接的无缝迁移。

## 工作方式

```mermaid
flowchart LR
    Apps["浏览器 / 桌面应用 / Git / 终端"] --> Gateway["本地网关 127.0.0.1:17890"]
    Linux["Docker / WSL（可选接入）"] --> Gateway
    Gateway -->|优先级 1| A["代理应用 A"]
    Gateway -.->|优先级 2| B["代理应用 B"]
    Gateway -.->|优先级 3 … 16| C["更多代理应用"]
    A --> Internet["网络"]
    B --> Internet
    C --> Internet
```

网关本身不提供代理节点、账号或订阅。各个出口客户端需要分别安装、配置并保持连接；它们各自的节点选择和分流规则仍然生效。

## 快速开始

### 1. 下载并解压

在 [Releases](https://github.com/AkparAby/proxy-gateway/releases/latest) 选择对应平台的完整运行包：

| 平台 | 下载文件 | 要求 |
|---|---|---|
| Windows x64 | `proxy-gateway-vX.Y.Z-windows-amd64.zip` | Windows 10/11，系统自带 PowerShell 5.1 或 PowerShell 7 |
| macOS Apple 芯片 | `proxy-gateway-vX.Y.Z-macos-arm64.zip` | Apple Silicon（M 系列），macOS 12 或更新版本 |

**Windows：** 完整解压到用户可写的固定目录，例如 `D:\Apps\ProxyGateway`。不要在压缩包内运行或只复制 EXE。

**macOS：** 解压后将 **Proxy Gateway.app** 拖入“应用程序”文件夹，再双击打开。只提供 Apple 芯片版，不提供 Intel 版。应用已做本地 ad-hoc 签名，**没有 Apple Developer ID 签名和公证**；如果首次打开被阻止，确认来源为本仓库 Release 后，按 [Apple 官方说明](https://support.apple.com/en-us/102445) 到“系统设置 → 隐私与安全 → 仍要打开”允许这个应用。不要关闭整个系统的 Gatekeeper。

首次运行会在本机生成默认配置，所有接入项和登录自启动默认关闭。网关不会自动接管系统代理。

### 2. 准备代理客户端

选择能提供本机 **HTTP 或 SOCKS5 代理端口**的客户端，分别确认它们已连接且能正常代理上网。不同客户端应使用不同端口，并关闭客户端的系统代理、代理守护和 TUN，让网关统一管理应用接入。

首次启动提供两个可编辑的示例出口：

| 名称 | 协议 | 本机地址 | 端口 |
|---|---|---|---|
| 代理 1 | HTTP | `127.0.0.1` | `17891` |
| 代理 2 | HTTP | `127.0.0.1` | `7892` |

这些名称和端口不绑定任何软件。你可以重命名、修改协议和端口，删除不需要的出口，或点击“添加代理应用”增加更多。支持 1–16 个出口，所有出口均需使用本机回环 IP（例如 `127.0.0.1`、`::1`），上游暂不支持账号密码认证。

网关不会替你启动或连接客户端。例如使用 SpeedCat 时仍需处于“已连接”状态，使用其他客户端时也需要保持其代理服务运行。客户端上游不能指回管理端口 `17880`、`17881`、`17882` 或当前网关监听端口（默认 `17890`），否则会形成循环。

### 3. 启动网关

Windows 双击 **`proxy-gateway.exe`**（也可使用 `Start-Gateway.cmd`）；macOS 双击 **`Proxy Gateway.app`**。程序启动后直接显示 Fyne 桌面管理窗口：

- 桌面管理：双击程序，或点击托盘 / Mac 菜单栏中的网关图标。
- 备用浏览器管理页：<http://127.0.0.1:17880>
- 统一代理：默认 `127.0.0.1:17890`，当前地址显示在窗口顶部。

Windows 也可以在解压目录执行：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\Start-Gateway.ps1 -OpenDashboard
```

再次双击 EXE 或 `.app`，会唤起已有桌面窗口，不重复启动界面和内核。两种版本都内嵌网关图标。Windows 使用系统托盘，macOS 使用顶部菜单栏图标。**窗口的 × 只隐藏界面，代理继续工作。** 真正退出请使用窗口或托盘菜单的“恢复接入并退出”；恢复失败时会保留客户端并显示原因。

需要静默启动时使用：

```powershell
.\proxy-gateway.exe --background
```

macOS 在终端静默启动（应用放在默认安装位置时）：

```sh
"/Applications/Proxy Gateway.app/Contents/MacOS/ProxyGateway" --background
```

### 自定义网关端口

在“网关设置 → 监听端口”填写 **1024–65535** 内的端口，然后点击“保存全部设置”。HTTP、HTTPS CONNECT 和 SOCKS5 共用该端口；只监听本机回环地址。管理端口 `17880 / 17881 / 17882` 保持固定，监听端口不能与它们或任一代理出口冲突。

保存前检查 TCP / UDP 端口占用，保存后重新监听，并同步**已启用**的接入：Windows 系统代理、用户环境变量、Git、Docker 配置和 WSL；macOS 系统代理、Git。保留原始恢复记录与登录自启动设置。应用失败时尝试回退原端口与接入配置，并报告回退结果。

更换端口时现有连接可能中断；已打开的终端、应用需重开，已创建的容器需重建。应用中手动填写的代理地址需自行更新。macOS 修改系统代理会请求管理员授权。以下示例使用默认 `17890`，自定义后请替换为实际端口。

### 4. 添加出口、调整优先级并接入应用

在桌面的“代理出口”页设置各客户端的名称、协议、地址和端口，在“切换策略”页设置自动回切、固定出口和检测参数。使用“上移 / 下移”调整优先级，**列表第一个是首选出口**。点击“保存全部设置”后重新检测，并按新配置选择出口。删除固定模式使用的出口时，界面会将模式改为自动，保存后生效。

等管理页显示出口可用后，在“应用接入”中启用需要的项目；也可在应用自己的代理选项中手动填写：

```text
HTTP / HTTPS 代理：http://127.0.0.1:17890
SOCKS5 代理：socks5h://127.0.0.1:17890
```

`HTTPS_PROXY` 使用 `http://` 是正常的：HTTPS 目标通过 HTTP CONNECT 建立隧道。部分应用的 SOCKS5 输入框需要分别填写主机、端口和“代理解析 DNS”。

### 5. 可选：登录自启动

在“应用接入”中启用“登录后启动网关”，之后当前系统用户登录时在后台自动启动并显示托盘图标，不弹出管理窗口。各出口客户端的自启动和自动连接需要分别设置。Windows 使用当前用户的登录启动项；macOS 使用用户级 LaunchAgent，下次登录时生效。启用后请保留程序路径；移动目录或应用前先停用自启动，再在新位置重新启用。macOS 如提示后台项目被禁用，请在系统的登录项设置中允许 Proxy Gateway。

## 默认策略与边界

| 项目 | 默认值 / 行为 |
|---|---|
| 出口优先级 | 列表从上到下；第一个为首选，其余为备用 |
| 检测间隔 | 每轮检测完成后等待 10 秒 |
| 单个请求超时 | 3 秒 |
| 故障阈值 | 已知健康出口连续失败 3 轮 |
| 恢复回切 | 优先级更高的出口连续健康 60 秒后自动回切 |
| 检测目标 | `https://www.gstatic.com/generate_204`、`https://cp.cloudflare.com/generate_204`；任一返回 2xx 即成功 |
| 所有出口都不可用 | 拒绝需要代理的请求，不自动改为直连 |
| 直连规则 | 本机、私有 IP，以及配置的域名后缀；默认域名仅 `localhost`、`local` |

首次启动时，出口需要检测成功后才会被采用。当前出口故障时按优先级寻找健康出口，可以连续跳过多个故障出口。启用自动回切时，会在更高优先级的出口中选择已满足稳定时间的最高优先级出口；保持模式则继续使用当前健康出口，直到它故障或手动切回首选。故障判定时间包含检测耗时，并非固定 30 秒。检测地址应能代表你的实际使用需求，也可在管理页替换。

切换影响**新建连接**。已经建立的下载、上传、WebSocket、SSH 和流式请求可能中断，需要应用自行重连；网关不会自动重放业务请求。固定出口模式只使用指定出口，不使用其他出口兜底。

当前面向 TCP 代理，不支持通过网关转发 UDP / QUIC，也不提供全机 TUN 或任意进程的强制代理。

## 各类应用如何接入

| 接入项 | 修改范围 | 生效条件 / 限制 |
|---|---|---|
| Windows 系统代理（Windows） | 当前用户的 Windows 系统代理 | 适用于遵循系统代理的浏览器和桌面应用；不是所有应用都会使用它 |
| 终端与子进程（Windows） | 用户级 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY`、`NO_PROXY` | 重新打开终端或应用，且程序本身支持这些变量 |
| macOS 系统代理（macOS） | 当前启用网络服务的 HTTP / HTTPS / SOCKS 代理，以及 PAC / 自动发现开关 | macOS 会请求管理员授权；绕过列表和 PAC 地址保持原值。新增网络服务需停用再启用；带认证的原代理请手动接入 |
| Git HTTPS | 当前用户 Git 的 `http.proxy` | HTTP(S) 仓库生效；Git SSH 需自行配置 |
| Docker Desktop（Windows 按钮） | Docker CLI 的 `proxies.default` 及 Desktop 代理模式 | 需要已安装并初始化 Docker Desktop，同时启用网关的系统代理接入；修改后重启 Desktop |
| WSL Ubuntu（仅 Windows） | Ubuntu 的 shell 环境文件与加载行 | 面向名为 `Ubuntu` 的发行版和 NAT 网络；新 shell 生效 |

浏览器或 Codex 等桌面应用是否走网关，还取决于应用自己的代理选项和环境。系统代理不是“所有没有独立配置的应用一定使用”的强制路由机制。

**Windows Docker 按钮：** 新构建和新建容器使用 `host.docker.internal:17890`；已运行容器不自动更新，容器内程序也要支持代理环境变量。网关不会自动重启 Docker Desktop。自定义网络、构建器或 Linux Docker Engine 需要另行验证。

**Windows WSL：** 启用后通过默认路由寻找 Windows 主机，并在 WSL 虚拟网卡建立受子网限制的转发入口；不绑定物理网卡。镜像网络和其他发行版不在当前接入脚本的支持范围内。现有 Ubuntu shell 可执行：

```sh
. ~/.config/local-proxy-gateway/env.sh
```

**macOS 终端：** 在需要代理的终端会话中执行以下命令。可按需添加到自己的 `~/.zshrc`，网关不会修改 shell 启动文件。

```sh
export HTTP_PROXY=http://127.0.0.1:17890
export HTTPS_PROXY=http://127.0.0.1:17890
export ALL_PROXY=socks5h://127.0.0.1:17890
export NO_PROXY=localhost,127.0.0.1,::1
```

撤销当前会话设置：`unset HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY`。如果之前已有这些变量，恢复原值即可。GUI 应用不一定继承终端变量，可使用系统代理或应用自己的代理设置。

**macOS Docker Desktop：** 可在 Desktop 的代理设置中选择系统代理（网关已启用 macOS 系统代理时），或手动填写 `http://127.0.0.1:17890`；构建器、镜像拉取和容器环境是不同配置范围，需要分别验证。Mac 版暂不自动配置容器代理，也不开放局域网监听。

## 文件、停止与升级

Windows 便携包包含：

```text
proxy-gateway-vX.Y.Z-windows-amd64/
├── proxy-gateway.exe           # Fyne 桌面程序（默认打开窗口）
├── runtime/gateway-manager.exe # Go 后台管理程序，内嵌备用管理页
├── Start-Gateway.cmd / .ps1    # 启动入口
├── Restore-Proxy.cmd / .ps1    # 应急恢复接入设置
├── scripts/                   # Windows / WSL 接入脚本
├── runtime/mihomo.exe          # 校验过的官方 Mihomo 内核
├── licenses/                  # 第三方许可证
├── release-manifest.json       # 版本、源码提交、依赖来源和文件哈希
├── README.md
├── LICENSE
└── THIRD_PARTY_NOTICES.md
```

macOS 包包含 `Proxy Gateway.app`、`Restore-Proxy.command`、README、许可证和发布清单；管理程序、Mihomo 与图标均封装在应用内。**Mac 的配置与日志保存在 `~/Library/Application Support/Proxy Gateway/`**，不写入应用包：

- `config.json`：你的出口与切换策略。
- `runtime/`：接入恢复记录、日志和内核临时文件。
- `~/Library/LaunchAgents/io.github.AkparAby.proxy-gateway.plist`：仅启用登录自启动后创建。

Windows 的配置保存在解压目录。运行后才会生成 `config.json`、`runtime/*.json`、日志和内核缓存。Release **不包含发布者的本机配置、日志、状态、备份、订阅或凭据**。需要核对下载完整性时，可将以下结果与 Release 附带的 `SHA256SUMS.txt` 比较：

```powershell
Get-FileHash .\proxy-gateway-vX.Y.Z-windows-amd64.zip -Algorithm SHA256
```

- **正常停止：** 在桌面窗口或托盘菜单点击“恢复接入并退出”；备用浏览器页使用“恢复接入设置并停止”。这也会撤销网关管理的登录自启动；下次需要时重新启用。
- **Windows 应急恢复：** 管理页无法打开时，双击 `Restore-Proxy.cmd`。它恢复此前由此目录中的网关接管的设置，本身不终止进程。恢复需要保留 `runtime/integrations-state.json`，不要提前删除运行目录。
- **Windows 升级：** 在旧版管理页恢复接入设置并停止 → 备份自己的 `config.json` → 将新版解压到新目录 → 复制自己的 `config.json` 到新目录 → 启动并重新启用所需接入项。不要复制旧版的日志、内核临时配置或接入状态到新包。

- **macOS 应急恢复：** 双击包里的 `Restore-Proxy.command`，或在终端运行 `"/Applications/Proxy Gateway.app/Contents/Resources/proxy-gateway" --restore`。程序运行中也可恢复；恢复本身不终止网关。若网络服务原本没有代理地址，恢复时会关闭该代理，输入框可能仍保留网关地址，不影响直连。
- **macOS 升级：** 先在管理页恢复接入设置并停止 → 备份用户目录的 `config.json` → 替换应用程序文件夹中的 `Proxy Gateway.app` → 打开并重新启用接入项。已有出口配置会保留。卸载前同样先恢复；需要保留设置时不要删除用户数据目录。
- **macOS 校验：** `shasum -a 256 proxy-gateway-vX.Y.Z-macos-arm64.zip`，与 Release 的 `SHA256SUMS.txt` 比较。

从 v0.1.x 升级时，程序会自动将原有 SpeedCat / Clash 配置迁移为可编辑的出口列表，保留端口、协议、主出口顺序、固定模式和检测策略，并将原始配置备份到本机 `backups/config-v1-*.json`。备份不进入 Release。迁移后的配置不能直接给旧版读取；回退时请恢复旧配置备份。

恢复只涉及网关管理的配置项，不会替你恢复各代理客户端的端口或 TUN 设置。关闭或卸载前应先恢复接入，避免应用继续指向已停止的网关。

## 常见问题

**启动后没有窗口？**

v0.4.0 起默认显示 Fyne 桌面窗口。登录自启动和 `--background` 模式仅显示托盘 / 菜单栏图标，点击该图标的“打开管理窗口”即可；再次双击程序也会唤起窗口。后台未连接时可点击“启动网关”。备用管理页仍为 <http://127.0.0.1:17880>。

**启动失败或管理页打不开？**

确认完整解压且目录可写，检查管理端口 `17880`、`17881`、`17882` 和当前网关端口（默认 `17890`）是否被其他程序占用。查看 `runtime/gateway.log` 和 `runtime/mihomo.log`；文件缺失时重新解压完整包。每台机器只运行一套网关。Mac 的日志在上述用户数据目录中，另有 `runtime/desktop.log`。

**所有出口都显示不可用？**

先分别验证客户端已连接、本地端口与网关设置一致；关闭客户端的系统代理、代理守护和 TUN，再检查 HTTPS 检测地址是否可达。只打开客户端窗口不代表代理已连接。

**开启系统代理后有些应用仍不走网关？**

检查应用是否遵循系统代理、是否另有代理设置，或改用终端环境变量和应用自带的代理选项。已有进程可能仍保留旧环境，需要重新启动。

**内核退出后会自动恢复吗？**

管理程序会重启异常退出的 Mihomo，重新检测出口后恢复转发。整个管理程序若异常退出，需要重新启动；“登录自启动”不等于持续的系统服务监控。

## 开发与打包

开发需要 **Go 1.26、Git**。桌面界面使用 **Fyne 2.8.2**，由 Go 编写；Windows 还需要 PowerShell 7、MinGW-w64 C 编译器（本项目使用 w64devkit 2.11.0），macOS 需要 Apple 芯片 Mac、Xcode Command Line Tools 和系统打包工具。中文字体已随源码提供。备用浏览器页继续保留原生 HTML/CSS/JavaScript。

```powershell
git clone https://github.com/AkparAby/proxy-gateway.git
cd proxy-gateway
go test ./...
go vet ./...
go test -tags "desktop,ci" ./internal/desktop ./cmd/desktop
pwsh -NoProfile -File .\scripts\package-release.ps1 -Version v0.4.0
```

macOS 本机构建：

```sh
go test -race ./...
go vet ./...
CGO_ENABLED=0 go test -tags desktop,ci ./internal/desktop ./cmd/desktop
go run scripts/package-macos.go -version v0.3.0
```

也可在 GitHub Actions 手动运行 **macOS Apple Silicon** 工作流，输入版本号。工作流在 `macos-15` Apple 芯片运行器上构建、执行原生测试、解压验证 `.app`，并演练 HTTP / SOCKS5 转发与多出口故障切换；下载其 `macos-arm64-release` Artifact 中的 ZIP。

打包脚本要求 Git 工作区干净，输出到 `dist/<版本>/`。它按 `scripts/mihomo.lock.json` 下载固定版本内核和对应源码，校验 SHA-256，构建对应平台的可执行文件，按显式文件列表生成 ZIP、源码附件和 `SHA256SUMS.txt`。已有打包目录不会被覆盖；Mac ZIP 保留可执行权限，并做 ad-hoc 签名验证。

Windows 在解压的开发包中运行。修改 Go 或 `web/` 后，可重新构建并替换已停止的管理程序：

```powershell
go build -trimpath -ldflags '-H=windowsgui' -o runtime/gateway-manager.exe .
$env:CGO_ENABLED = "1"
go build -tags desktop -trimpath -ldflags '-H=windowsgui -extldflags=-static' -o proxy-gateway.exe ./cmd/desktop
```

Windows 后台和桌面程序的 `.syso` 图标资源均已包含在源码仓库中，构建会自动嵌入；更换图标时参见 [图标源文件与生成说明](winres/README.md)。

桌面界面与网关是独立进程，通过本机 API 通信；桌面单实例唤起使用 `127.0.0.1:17882` 和当前用户缓存目录中的随机令牌。后台可独立运行，界面关闭或异常退出不会自动终止代理。

桌面代码使用 `desktop` 构建标签，常规 `go test ./...` 检查后台与共享协议；`go test -tags desktop,ci ./internal/desktop` 使用内存图形驱动验证编辑、排序、配置刷新和平台接入控件。

源码仓库不附带内核。若直接运行源码构建的 EXE，需将打包结果中的 `runtime/mihomo.exe` 复制到项目的同名位置，并保持启动与接入脚本完整。

Windows 现有系统接入脚本继续随包提供；桌面界面和网关管理逻辑均为 Go。

单元测试覆盖多出口故障切换、优先级恢复、保持当前出口、固定模式、删除出口、旧配置迁移、回环配置和管理接口校验。`scripts/verify-failover.mjs` 可进一步演练真实主备切换；它需要 Node.js、至少两个健康出口及运行中的网关，会暂时修改策略并可能中断连接，仅适合在可中断的测试环境执行。

## 贡献与许可证

欢迎提交 [Issue](https://github.com/AkparAby/proxy-gateway/issues) 或 Pull Request。报告问题时请提供软件版本、操作系统版本和芯片架构、复现步骤和脱敏日志，不要提交订阅、令牌、账号信息、`config.json` 或完整 `runtime/` 目录。

项目管理程序、页面和脚本采用 [MIT License](LICENSE)。便携包中的独立 [Mihomo](https://github.com/MetaCubeX/mihomo) 内核采用 GPL-3.0，保留其原许可证，并在同一 Release 提供对应版本的源码附件；MIT 许可证不替代第三方许可证。详见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
