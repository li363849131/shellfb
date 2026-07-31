# Web AI 编程工作站 — 一键部署指南

本目录是 ShellFromBrowser 全套服务的部署包。

| 服务 | 端口 | 说明 |
|---|---|---|
| Web 终端 (shellfb) | 30009 | 交互式命令行 + Tmux 多 session 绑定 |
| Web 网盘 (filebrowser) | 30010 | 可视化文件管理（可访问 `/root` 及隐藏文件） |
| Web VS Code (code-server) | 30011 | 浏览器端 IDE |
| 语音识别后端 (whisper-server) | 30022 | 仅监听 127.0.0.1，由 shellfb 反代 |

其他功能：

- **语音输入**：两个引擎可选。浏览器原生 Web Speech API（低延迟，需联网、需 HTTPS），或本机 faster-whisper（离线，CPU 推理，模型默认 `tiny`）。面板里可切换，默认「自动」优先原生。
- **AI 说人话转命令**：**界面默认关闭**（转换质量不理想，2026-07-31 隐藏了入口）。后端接口保留可用。想开回来：把 `web/static/js/terminal.js` 里 AI 段开头的 `AI_UI_ENABLED` 改成 `true`，重新编译。开启后是 `🤖 AI` 按钮或 `Ctrl+Shift+A`，**命令只填入终端，不会自动执行**。
- **富文本预览**：DOCX 排版、XLSX 多表、文本行号与快捷保存、文件删除。

---

## 目录文件

- `install.sh` — 单文件全自动部署脚本，内嵌了完整源码，拷到新服务器直接跑。
- `shellfb-src.tar.gz` — 源码压缩包（与 `install.sh` 内嵌的是同一份）。
- `shellfb_srcipt.txt` — 早期部署脚本明文，仅作参考，**不是当前版本**。
- `README.md` — 本文件。

---

## 在新服务器上部署

```bash
chmod +x install.sh
./install.sh
```

脚本会依次完成：

1. 安装基础依赖（`chafa`、`libreoffice`、`xclip`、`tmux`、`ffmpeg` 等）
2. 解压内置源码到 `/root/shellfb-src`，安装最新 Go 并编译 `/usr/local/bin/shellfb`
3. 部署语音后端：`pip3 install faster-whisper flask flask-cors`，写入 `whisper-server.service`
4. 部署 FileBrowser（30010）与 code-server（30011）
5. 拉起 `cf-tunnel`、`cf-filebrowser`、`cf-vscode` 三条 Cloudflare 隧道，输出 HTTPS 链接
6. 安装 `agy`（Google Antigravity）、`claude`（Claude Code）、`codex`（Codex CLI）
7. 写入 `restart-all` 快捷命令

### 部署后：启用 AI 功能

AI 转命令需要一个 Anthropic 凭据，脚本不会替你填。自己写入：

```bash
mkdir -p /root/.config/secrets
printf 'export ANTHROPIC_AUTH_TOKEN=%s\n' '你的key' >> /root/.config/secrets/env
chmod 600 /root/.config/secrets/env
systemctl restart shellfb
```

如果用中转服务，再加一行 `export ANTHROPIC_BASE_URL=https://你的中转地址`。
换模型：`export SHELLFB_AI_MODEL=claude-sonnet-5`（默认 `claude-haiku-4-5-20251001`）。

没配 key 时 AI 按钮不会出现，其他功能不受影响。验证：

```bash
curl -s "http://127.0.0.1:30009/api/ai/status?token=<登录token>"
```

### 首次跑 whisper 的注意事项

首次调用会下载模型（`tiny` 约 75MB），第一句会慢。想换模型：

```bash
systemctl edit whisper-server   # 加 Environment="WHISPER_MODEL=small"
```

`tiny` 在噪音下会产生幻觉文本（「谢谢观看」这类），`whisper_server.py` 里已有过滤名单。

---

## 默认登录凭据

- 账号 `admin`，密码 `gjp`
- **部署后请立即改密码**，见下文。

---

## 维护与故障排查

### 级别 1：一键重启所有服务

```bash
restart-all
```

重启 shellfb、whisper-server、filebrowser、code-server 和三条隧道，然后打印各服务状态与最新的隧道链接。

### 级别 2：单个服务排查

```bash
systemctl status shellfb          # Web 终端
systemctl status whisper-server   # 语音后端
systemctl status filebrowser      # 网盘
systemctl status code-server      # IDE
systemctl restart cf-tunnel cf-filebrowser cf-vscode   # 三条隧道
```

看日志：`journalctl -u shellfb -n 100 --no-pager`

隧道链接会变，从日志里取：

```bash
journalctl -u cf-tunnel -n 50 --no-pager | grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' | tail -1
```

### 级别 3：全量重装

```bash
cd /root/agy && ./install.sh
```

脚本开头会 stop/disable 旧服务。注意：`/root/shellfb-src/go.mod` 存在时**不会**覆盖源码，想强制用内嵌版本先 `rm -rf /root/shellfb-src`。

### 级别 4：系统级重启

```bash
reboot
```

命令行完全无响应时，去云厂商控制台强制重启。

### 网页端常见问题

**页面能打开但敲键盘没反应** — 这个 bug 已修（服务端每 25 秒发 WebSocket ping，前端断线自动重连，状态栏会显示 `Reconnecting…`）。若仍出现，先确认部署的前端和源码一致：

```bash
curl -s http://127.0.0.1:30009/static/js/terminal.js | diff - /root/shellfb-src/web/static/js/terminal.js
```

有差异说明二进制是旧的，需要重新编译（前端资源用 `go:embed` 编进二进制，改文件不重编不生效）：

```bash
cd /root/shellfb-src && go build -o /usr/local/bin/shellfb ./cmd/shellfb && systemctl restart shellfb
```

**语音按钮点了没反应** — 先看浏览器控制台。多半是缓存或前端启动时抛异常，不是语音代码本身的问题。

**语音能开面板但录不了音** — `getUserMedia` 要求安全上下文。用 HTTPS 的隧道地址，不要用 `http://<ip>:30009`。

---

## 修改密码

**Web 网盘：**

```bash
systemctl stop filebrowser
filebrowser users update admin -p <新密码> -d /etc/filebrowser/filebrowser.db
systemctl start filebrowser
```

**Web 终端：**

```bash
python3 -c 'import bcrypt; print(bcrypt.hashpw(b"新密码", bcrypt.gensalt()).decode())'
# 把输出填进 /etc/shellfb/config.yaml 的 password_hash
systemctl restart shellfb
```

`/etc/shellfb/config.yaml` 含 `jwt_secret` 和密码 hash，权限应为 600。

---

## 安全说明

- 三个服务只监听 `127.0.0.1`（终端是 `*:30009`），公网访问走 Cloudflare 隧道。
- 隧道链接是 `trycloudflare.com` 随机域名，**知道链接的人就能访问登录页**，所以密码必须改。
- 终端以 root 运行，网盘根目录是 `/`。这是设计如此，也意味着一旦登录凭据泄露，整台机器就暴露了。
