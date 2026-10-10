# 参与开发

先阅读 [LICENSE](LICENSE)、[技术细节](docs/TECHNICAL_DETAILS.md) 与 [隐私说明](docs/PRIVACY.md)。本项目有非商业限制，贡献不能移除上游必需通知。

## 开发入口

Windows 使用 Go（`go.mod`）、.NET 8、WebView2 Runtime；运行 `scripts/build-windows.ps1`。非硬件 UI 测试使用 `-demo` 和独立数据目录，不使用 `%LOCALAPPDATA%` 中的真实资料。

Go 修改运行 `go test ./...`；桌面监督运行 `SupervisionSmoke`，需要交互桌面的桥/模块切换运行 `DeviceSwitchSmoke`。云改动检查中继测试，并在隔离服务器完成 HTTPS/登录/注册/命令回归。

## 提交要求

- 一个改动解决一个明确问题，写明重现和验证结果。
- 不提交个人配置、日志、数据库、服务器路径、截图中真实短信和旧构建产物。
- 不在测试中使用实际 IMEI/ICCID/手机号；用固定虚构身份。
- 调整主题和布局至少检查浅/深色、100%/115%/130%、窄窗口、双模块、长文本和滚动。
- 保留 reduced-motion 行为，隐藏滚动条不能取消滚动；移除装饰不能破坏窗口拖动。
- 短信/电话/eSIM 测试可能收费或损伤状态，应先用 demo，再经设备主人许可做实测。
- 截图脚本见 `scripts/render-docs.cjs`；它使用演示后端及人工数据，不连接私人云服务器。

公开仓库：[Oldleeo/DJ4GHUB](https://github.com/Oldleeo/DJ4GHUB)。普通问题可提交 [Issue](https://github.com/Oldleeo/DJ4GHUB/issues)，代码改动可通过 fork 发起 Pull Request；安全问题请先阅读 SECURITY.md，不要贴公开凭据。尚未发布正式二进制 Release。
