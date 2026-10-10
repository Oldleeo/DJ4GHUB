# 技术细节

## 结构与技术栈

| 层 | 位置 | 责任 |
| --- | --- | --- |
| Windows 壳 | `apps/hub-windows/src/DJ4Hub.Windows/` | WPF 窗口、托盘、自启、设备发现与后端监督 |
| 硬件与本机 HTTP 服务 | `cmd/dj4ghub-macos/` | 历史目录名；当前同时包含 Windows 后端代码，不代表随包提供 macOS 客户端 |
| 设备通道 | `internal/modem/` 等 | AT 请求、状态、串口生命周期与仲裁 |
| eSIM | `internal/esim/`、`third_party/euicc-go/` 等 | logical channel、APDU、LPA 与 Profile 操作 |
| 本地前端 | `cmd/dj4ghub-macos/web/` | HTML/CSS/JS，编译时嵌入 Go 后端 |
| 共用视觉层 | `internal/uiassets/` | 本地与云端的空间层叠样式、动效、主题 |
| 云中继 | `cmd/dj4ghub-relay/` | 注册、租约、快照、命令、配对、权限与加密持久化 |
| 云控制台 | `cmd/dj4ghub-relay/panel/` | 单管理员 Web UI、TOTP 登录与远程操作 |
| 外部推送 | 主机通知实现 | Bark / Telegram 由主机发出，不嵌入作者凭据 |

主要构建依赖：Go 版本由 `go.mod` 固定，.NET 8 WPF、Microsoft WebView2。前端使用原生 JS，没有必须先 npm 安装的运行依赖。二维码识别使用随包 jsQR；图标来自 Tabler 并保留 MIT 许可。

Go module 路径仍保留历史 `github.com/WongLoki/DJ4Hub`，它是内部 import 身份，不是本次发布仓库地址；fork 后不必为了能构建就重命名。如果要改需同步所有 import 和 replace。

## 启动与双模块

Windows 壳通过单实例互斥避免重复桌面进程。发现支持的 AT 设备后，为各模块创建 `BackendProcessService`；每实例传入自己的端口、数据目录、身份和父进程 PID。主机分别监听回环地址，多个主机不能共用同一个监听端口。

模块身份优先使用设备 IMEI，而不是 COM 号。主模块沿用 `%LOCALAPPDATA%\DJ4Hub\`，其他模块隔离到 `Devices/<key>/`，主模块选择保存到 `multi-device.json`。新模块的通知设置只在首次初始化复制，不持续镜像。

自动扫描约 30 秒，健康监督约 10 秒。连续故障达到阈值或主机退出后，按模块独立恢复并退避；恢复不重放发送短信或拨号。所属父进程退出后后台会终止，减少残留串口占用。

## 串口所有权与状态一致性

发现阶段限制 VID/PID 与 AT 角色，不把所有 COM 口轮流试一遍。常规读取 Windows 设备列表；需要身份探测时执行限时、只读操作。探测进程与运行主机使用同一所有权机制，避免自己抢自己。

AT 请求通过管理器串行组织；通道重建时旧句柄不可继续当可用。eSIM 启用可能引发 SIM refresh、通道失效和网络重新注册，操作后要重新读取卡片/网络，并使概览最终对应当前 Profile。

短暂读取失败保留未知语义，不将无法解析的 SIM 响应直接写成“未插卡”。这改善展示的一致性，但不是断网期间仍能获得实时状态的保证；界面需要区分最后有效值、读取失败和离线。

## 本地 HTTP 与原生桥

后端默认回环监听，UI 静态资源编译嵌入，不从远端加载应用逻辑。本地 API 承担状态、短信、电话、eSIM、通知、网络、保号与远程配置。

WebView2 用于渲染；页面负责一致主题与自定义窗口按钮，WPF 负责真实最小化/最大化/关闭。桥消息使用白名单动作，并核验消息来源是否为已注册的本机回环主机；不把任意网页变成原生窗口控制来源。透明拖动区域保留，滚动条隐藏不取消滚动。

## 云端命令链

浏览器选择设备 → 中继验证会话与权限 → 命令排队 → 持有租约的设备主机领取 → 本机执行 → 结果回传 → UI 读取结果/新快照。

“提交成功”仅表示中继接受命令，不等于模块已执行。状态需按设备身份和 revision 更新；切换设备时旧响应不能覆盖新选择。关键变更命令在执行前记录去重状态，异常恢复后不盲目重复执行。

无法做到硬件操作与云端结果落盘的分布式原子提交。因此若执行期间断电，结果可能不明确，需要人工读取设备核对。不能宣称所有命令严格 exactly-once。

## 中继 API 概览

| 路径 | 用途 |
| --- | --- |
| `POST /panel/api/login` | 密码 + TOTP 登录 |
| `GET /panel/api/session` / `POST /panel/api/logout` | 会话状态与退出 |
| `GET /panel/api/devices` | 管理员设备列表 |
| `GET /panel/api/devices/{device}` | 读取设备快照 |
| `POST /panel/api/devices/{device}/commands` | 提交已允许的命令 |
| `GET /panel/api/devices/{device}/commands/{command}` | 读取执行结果 |
| `POST /panel/api/devices/{device}/host-pairing` | 已有设备主机转移配对 |
| `GET /panel/api/audit` | 管理操作审计 |

更底层的 host/client API 见 `main.go` 中的 `routes()`，具体 payload 以处理器和测试为准，不把此概览当冻结的外部 SDK。主机租约约 45 秒，配对码约 10 分钟，有限命令队列与请求大小限制降低无界堆积风险。

## 数据、安全与威胁边界

中继状态使用 AES-GCM 加密后落盘，主密钥单独保存。这是**静态数据加密**，不是让中继管理员完全读不到短信的端到端加密：服务器拥有解密密钥。

控制台密码和 TOTP 保护管理员会话，cookie 安全属性、CSRF/权限检查、命令白名单与配对限时保护远程操作。原生 TLS 模式最低 TLS 1.2；`-http-only` 仅供可信 HTTPS 反向代理后面使用。Compose 不发布明文中继端口。

快照/命令可能含手机号、IMEI、短信内容与卡片数据。元数据审计不能替代对所有状态的隐私评估。Windows 本机 JSON 和数据库不等于全盘加密；应使用受保护账号、磁盘加密和妥善备份。

当前是单管理员实例，不具备租户隔离、公共注册、计费或大规模 SaaS 运维体系。外部串口工具、被攻陷的主机/服务器、操作系统管理员与运营商不在“绝对阻止”的范围内。

## 音频

本机音频使用模块 USB 音频输入/输出；云端通过 WebRTC 建立音频桥，HTTPS 中继负责相关控制与信令。NAT 穿透依赖网络环境，必要时通过 TURN 转发媒体。浏览器麦克风权限、正确的设备、固件语音能力、SIM 服务必须都满足。

## 构建与回归

```powershell
./scripts/audit-public.ps1
./scripts/build-windows.ps1
dotnet run --project apps/hub-windows/tests/DeviceSwitchSmoke -c Release -- dist/windows/dj4ghub-service.exe
```

`DeviceSwitchSmoke` 需要交互式 Windows 桌面/WebView2，使用隔离演示主机而不是插着的真实模块。`SupervisionSmoke` 验证双主机隔离、恢复和父进程退出。Go 测试覆盖中继权限/持久化、状态、eSIM 与设备逻辑；单元测试不替代真实短信、运营商语音、断电或公网验收。

Linux 中继构建：`CGO_ENABLED=0 go build -trimpath -o dj4ghub-relay ./cmd/dj4ghub-relay`。Docker 模板仅构建中继，不把 Windows USB 驱动搬到容器里。
