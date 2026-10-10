# 首次上传 GitHub 清单

当前目录是独立脱敏导出，没有复制私有仓库 `.git` 历史；首次发布使用独立新仓库 [Oldleeo/DJ4GHUB](https://github.com/Oldleeo/DJ4GHUB)。以下步骤用于复核首次发布或准备自己的 fork。不要将原开发目录直接 push；`.gitignore` 无法删除已经进历史的秘密。

## 上传前

1. 阅读许可说明，确认使用“源码公开 / 非商业许可”，保留上游 Required Notice。
2. 运行 `./scripts/audit-public.ps1`，人工复核 README、图像与新增文件。
3. 运行 Go 测试、Windows 构建和相关烟测，记录通过/失败/未验证，不把配置好的 CI 当已通过。
4. 确认 `.env`、运行数据、日志、旧截图、发布产物均未在待提交列表。
5. 本次文件夹可直接作为新仓库起点，**不要导入原私有仓库历史**。
6. 在自己账号创建新仓库后，核对真实仓库地址；此包不预填假仓库链接。

```powershell
git init -b main
git add .
git status --short
# 仔细检查暂存文件，再创建首次提交
git commit -m "Prepare sanitized public source and documentation"
# 此时再按 GitHub 显示的真实地址设置 remote，并 push
```

初始化和上传应在本公开目录执行，不是在私有开发目录。上传前可以启用 GitHub 的秘密扫描/推送保护，但可用范围依账号/仓库设置，不能代替人工审查。[GitHub 秘密扫描说明](https://docs.github.com/en/code-security/concepts/secret-security/secret-scanning)

## 发布二进制

在干净 Windows 构建环境依次运行 `./scripts/build-windows.ps1`、`./scripts/build-installer.ps1`。安装工具固定版本和上游 SHA-256；WebView2 安装组件校验 Microsoft Authenticode 签名。产物在 `dist/releases/`：安装 EXE、完整便携 ZIP、SHA256SUMS.txt。CI 只在全部构建及安装烟测成功后上传 `windows-installers` artifact；发布前再核对产物校验和。

`test-installer.ps1` 只允许在一次性 GitHub-hosted runner 上运行，不能在用户当前电脑运行安装/卸载测试。它验证安装、快捷方式、原位升级、演示后台、云默认关闭、卸载和数据保留；不访问真实模块。

从干净 checkout 构建，不复制自己正在运行的安装目录。完整便携目录压成 ZIP，附 SHA-256 校验和、版本、系统要求与已知限制。代码和二进制都要保留 LICENSE、THIRD_PARTY_NOTICES 与实际分发的依赖许可。

不要把用户目录、预绑定 Bark/Telegram 或作者私有中继混进便携包。当前 app 图标/品牌保留是有意的版权展示，不是秘密。签名尚未提供时不要声称微软或 DJI 认证。

## 发布后

补真实 Release 链接与对应校验和；检查 GitHub 渲染、PNG/SVG、文档链接和 Actions。新增改动仍需再扫描，确保 CI 日志不打印凭据。不要把当前文件删除当作清除了曾经公开的密钥，需要轮换。
