# 自建云端：属于你自己的控制台

云端是可选项。本地 EXE 可以独立工作；启用云端后仍由电脑执行硬件操作，云服务器不替代设备主机。

![连接关系](images/architecture.png)

## 前提与验证范围

准备 Linux 服务器、自己可控制的域名、Docker Engine + Compose 插件。域名 A/AAAA 记录指向服务器；公网 TCP 80/443 可访问，已有网站占用时需要自行整合反向代理，不能盲目覆盖。

此模板使用 Caddy 终止 HTTPS，内部中继使用仅 Docker 网络可见的 HTTP。中继没有发布 18444 端口。不要给它额外加公网端口映射，也不要把 Windows 回环 API 暴露到公网。

本次准备验证了 Go 中继测试和构建；本机没有 Docker，**尚未验证此 Compose 的真实容器启动、证书签发及公网接入**。提供 CI 配置检查与 Docker 构建任务，不代表 GitHub CI 已经执行成功。自行部署后完成本页验收再使用真实卡片。

## 1. 配置并启动

在服务器下载源码，进入仓库根目录：

```bash
cd deploy/selfhost
cp .env.example .env
```

编辑 `.env`，只填域名（不带 `https://` 或路径）和自己接收证书通知的邮箱：

```dotenv
RELAY_DOMAIN=relay.example.com
ACME_EMAIL=admin@example.com
```

示例地址不可直接使用。`.env` 已被 Git 忽略，但仍应保护服务器权限。

```bash
docker compose config --quiet
docker compose up -d --build
docker compose ps
docker compose logs --tail=80 caddy relay
```

Caddy 自动申请和续期证书。访问 `https://自己的域名/panel/`，应看到登录界面和有效 HTTPS。Caddy 当前跟随 `caddy:2`；生产环境建议验证后锁定镜像 digest，并计划更新安全补丁。

## 2. 取得自己的凭据

首次启动由中继在数据卷内生成：

| 文件 | 用途 | 应给谁 |
| --- | --- | --- |
| `panel.password` | 云控制台登录密码 | 只给控制台管理员 |
| `panel.totp` | TOTP base32 密钥 | 导入自己的验证器；不能发到 GitHub |
| `owner.token` | 初次注册 Windows 设备主机 | 只填自己可信的主机，不是登录密码 |
| `master.key` | 加密状态的主密钥 | 只做受保护备份，不填界面 |

以下命令会显示**自己的秘密**，只在私有终端执行；不要截图、发 Issue 或粘贴聊天：

```bash
docker compose exec relay cat /data/panel.password
docker compose exec relay cat /data/panel.totp
docker compose exec relay cat /data/owner.token
```

验证器手动添加 TOTP：名称 `DJ4GHUB`，密钥填 `panel.totp`，6 位、30 秒周期、SHA-1。服务器与手机时间应正确同步。登录需要密码和当前验证码。

这三项不是硬编码默认值。持久化数据卷保留时，密码不会每次启动自动改变；泄露后必须轮换。密码变更可修改数据卷中对应文件并重启；owner token 轮换前规划已有主机重新注册。不要为“换密码”删整个数据卷。

## 3. 注册一块或两块 Windows 模块

本地选中 A → 云同步 → 填 `https://自己的域名` → 保存主机设置 → 在所有者令牌区域填写自己的 `owner.token` → 启用主机同步。

确认本地显示已连接，刷新云控制台，A 出现在设备列表。对 B **重新选择模块后分别重复**；不能只绑定一次然后假设两块已注册。

首次注册要用 owner token。控制台“接管/配对”码用于已存在设备的主机转移，约 10 分钟有效、单次使用，不是首次添加全新设备的按钮。

## 4. 验收顺序

1. HTTPS 有效，登录页面与工作区互斥显示。
2. 密码 + TOTP 登录成功，退出后不能继续发命令。
3. A/B 分别在线，设备标识、更新时间和当前选择正确。
4. 只读刷新返回目标模块数据，再测试自己的短信。
5. 配置自己的通知服务并用新短信确认转发。
6. eUICC 卡再测试 Profile；普通 SIM 不测试下载。
7. 通话指令和音频分别验收，不把“已提交”当作执行成功。
8. 电脑退出/断网后云端应最终显示离线，不误报可执行。

## 5. 备份、升级与停机

备份 `relay_data` 卷，**加密状态与 master.key 必须成对保留**；只备份 `state.enc` 无法恢复。备份含密码、TOTP 和设备资料，不能放进公共仓库。还应备份配置与 Caddy 卷，并在隔离环境验证恢复。

更新先备份，再拉取已核验版本，执行：

```bash
docker compose up -d --build
docker compose logs --tail=80 relay caddy
```

停服务用 `docker compose stop`。不要轻易执行 `docker compose down -v`，它会删除持久化卷和秘密。回滚涉及数据格式时需使用对应备份，不保证跨格式回滚兼容。

## 6. 远程音频 / TURN

WebRTC 在对称 NAT 或严格网络中可能不能直接建立媒体连接。中继支持 `-turn-urls` 和 `-turn-secret`：后者是 coturn 共享密钥文件路径，用于生成短期凭据。

需要自行部署与保护 TURN、配置端口/防火墙/带宽并挂载密钥；本模板**不包含一键 TURN 服务**。不要把长期 TURN 密钥写入 Compose 或浏览器 JS。短信等 HTTPS 命令与 WebRTC 音频是不同通路，音频失败不代表短信链路也失败。

参考：[Caddy reverse_proxy](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy)、[Docker Compose secrets](https://docs.docker.com/compose/how-tos/use-secrets/)。本模板凭据保存在应用数据卷内，不是预置 Compose secret；另行挂载自定义凭据时请使用文件与合适权限。
