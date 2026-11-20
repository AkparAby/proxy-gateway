# MyGo 桌面版

使用 MyGo v0.10.0 重写的 Windows / macOS 正式界面，沿用现有网关和配置格式。

- 左侧导航：概览、代理出口、网关设置、切换策略、应用接入、事件。
- 在线时显示“服务在线”，不再显示“启动网关”；离线时可启动，启动中禁止重复点击。
- 内容区域独立滚动，左侧导航、顶部保存操作和底部网关状态固定，切换页面保留滚动位置。
- 手动检查网络时，各代理延迟位置显示转圈动画，按各自的实际检测结果结束；自动检测保留已有延迟，结果返回后静默更新。延迟仅对成功的检测地址取平均，失败或超时地址不计入。操作提示会自动消失，不占用底栏。
- 保留低延迟自动选择、自动识别应用并指定固定客户端、代理排序、接入恢复和托盘功能。
- 定时读取状态不会覆盖未保存的编辑；保存前检查其他窗口是否已更新配置。
- 内置中文字体。桌面使用纯 Go 构建，不需要 Fyne 或 WebView2。
- 窗口标题栏、任务栏和托盘使用同一套网关图标；窗口通过 MyGo 的 `SetIcon` 显式设置。

## 运行

先在旧版选择“恢复接入并退出”，再双击新版目录中的 `proxy-gateway.exe`。
关闭窗口会留在托盘运行，彻底退出请使用“恢复接入并退出”。
Mac 配置位于 `~/Library/Application Support/Proxy Gateway/`，程序与内核位于 `.app/Contents/`；替换应用不会覆盖用户配置。
迁移设置时仅复制 `config.json`，不要复制旧目录的 `runtime/integrations-state.json`。
移到其他电脑时保留整个运行包；应用身份会由后台在新电脑重新识别，代理客户端的本地端口仍需与实际设置一致。

## 构建完整 Windows 运行目录

此目录是独立 Go module，使用 Go 1.27.4 工具链，避免改变旧 Fyne 构建的依赖。
`GOTOOLCHAIN=auto` 时 Go 会自动下载工具链。MyGo 依赖固定在本目录的 `go.mod` / `go.sum`。

在仓库根目录运行（PowerShell 7）：

```powershell
pwsh -NoProfile -File scripts/package-mygo.ps1 -BasePackage dist/mygo-preview -Output dist/mygo-local
```

`-BasePackage` 指向完整解压的既有 Windows 运行包，用于取得 Mihomo 和其许可证。
输出默认是 `dist/mygo-preview/`，可用 `-Output` 指定与输入不同的目录（上述示例为 `dist/mygo-local/`）。脚本不启动网关，不复制系统代理恢复状态。
可选 `-Config <config.json 路径>` 只在目标尚无配置时复制配置；重建前请退出目标目录中的程序。
此脚本仅用于本地便携预览。正式发布使用仓库根目录的 `scripts/package-release.ps1 -Version v0.5.0`（Windows）或 `go run scripts/package-macos.go -version v0.5.0`（Apple 芯片 Mac）；两者均构建此 MyGo 模块。

## 验证

在本目录运行：

```powershell
go test ./...
go vet ./...
# race 检查需要 CGO_ENABLED=1 和可用的 GCC
go test -race ./...
```

`proxy-gateway.exe --ui-preview` 打开可交互的示例界面，不连接网关，不修改系统接入。
`proxy-gateway.exe --ui-smoke <截图目录>` 在原生窗口渲染六个页面和应用选择弹窗，检查刷新并自动关闭，结果写入 `native-result.txt`。
测试覆盖左侧导航、启动状态、策略切换、滚轮从页底返回、草稿保留、应用选择及保存失败/冲突。
