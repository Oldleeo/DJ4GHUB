# DJ4GHUB 0.6.7 · Windows 安装包公开预览

**普通用户只需下载 `DJ4GHUB-Windows-x64-0.6.7-Setup.exe`。不需要部署服务器，不需要从源码构建。**

## 下载哪个？

| 文件 | 用途 |
| --- | --- |
| `DJ4GHUB-Windows-x64-0.6.7-Setup.exe` | 推荐：双击安装，自动建立桌面图标和卸载入口 |
| `DJ4GHUB-Windows-x64-0.6.7-Portable.zip` | 可选免安装版：完整解压运行，电脑需已有 WebView2 |
| `SHA256SUMS.txt` | 对照下载文件的 SHA-256，检查文件完整性 |
| GitHub 自动生成的 `Source code` | 给开发者的源码，不是普通用户安装包 |

## 怎么用？

1. 下载并运行 Setup，阅读安装说明与非商业许可后安装。
2. 从桌面启动 DJ4GHUB，插入一块或两块兼容 DJI 4G 模块。
3. 选择模块，确认 AT / SIM / 网络状态，再使用短信、电话和卡片管理。

后台和 .NET 运行时已包含；缺少 WebView2 时会联网安装 Microsoft 官方组件。USB / AT 驱动需已正确安装，不会偷偷安装未签名驱动。

默认路径是 `%LOCALAPPDATA%\Programs\DJ4GHUB`，当前用户安装无需管理员。升级前从托盘退出旧版，不要同时运行不同位置的副本；卸载保留 `%LOCALAPPDATA%\DJ4Hub` 中的短信和配置。

## 云同步要不要部署？

本地使用完全不用。云同步默认关闭，不附带作者的云账号或私人服务地址。

只有手机远程控制才需设置自己的持续在线云中继；本地电脑、程序和模块也要保持在线。安装 EXE 不会自动创建公网服务器。[自建云端指南](SELF_HOSTING.md)是可选高级步骤。

## 发布边界

- 安装包暂未代码签名，Windows 可能显示未知发布者；仅从本仓库 Release 下载并核对校验和。
- Windows 10/11 x64；不是 macOS 或鸿蒙原生安装包。
- 双模块、短信、eSIM、通知与主要远程管理能力保留；实际硬件、固件、运营商、供电和双向音频仍需各环境验证。
- 这次打包测试不向真实卡发送短信、拨号或变更 Profile，也不覆盖作者当前运行中的安装。
- 完整许可、第三方版权与依赖许可证随包保留；项目继承 PolyForm Noncommercial 1.0.0，不是任意商业使用许可。

Copyright 2026 老李 Oldlee · [X / @oldleeoo](https://x.com/oldleeoo)

Required Notice: Copyright iniwex5 (https://github.com/iniwex5/vohive)
