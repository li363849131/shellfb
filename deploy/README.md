# deploy/

部署相关文件。

## 从这个仓库部署（推荐）

已经 clone 了仓库，直接用当前源码，不要用 `agy/install.sh` 里内嵌的那份：

```bash
cd /path/to/shellfb
go build -o /usr/local/bin/shellfb ./cmd/shellfb
```

配置示例见仓库根目录的 `config.example.yaml`。

## whisper_server.py

语音输入的「本机 Whisper」引擎后端。Flask 服务，监听 `127.0.0.1:30022`，shellfb 把 `/api/voice/` 反代给它。

```bash
pip3 install faster-whisper flask flask-cors
python3 deploy/whisper_server.py
```

环境变量：`WHISPER_MODEL`（默认 `tiny`）、`WHISPER_LANG`（默认 `zh`）、`WHISPER_PROMPT`。

不跑它也不影响其他功能，浏览器原生语音引擎仍然可用。

## agy/

全套服务的一键部署包（Web 终端 + 网盘 + VS Code IDE + Cloudflare 隧道 + 语音 + AI）。给**全新服务器**用，不是给已经 clone 了仓库的机器用。

- `install.sh` — 单文件部署脚本，尾部内嵌了一份 base64 源码快照。拷到新机器 `chmod +x` 直接跑。
- `shellfb-src.tar.gz` — 同一份源码快照的独立压缩包。
- `README.md` — 部署与运维说明，**先读它**。
- `shellfb_srcipt.txt` — 早期脚本明文存档，**已过时，不要执行**。留着仅作历史参考。

`install.sh` 里内嵌的源码是打包那一刻的快照，不会随仓库自动更新。改了代码要重新打包：

```bash
MARKER=$(grep -a -n '^__SHELLFB_SRC_ARCHIVE__' deploy/agy/install.sh | cut -d: -f1)
head -n "$MARKER" deploy/agy/install.sh > /tmp/head.sh
tar czf /tmp/src.tar.gz --exclude='.git' --exclude='transfers' \
    --exclude='recordings' --exclude='bin' --exclude='config.yaml' \
    --exclude='deploy/agy' \
    -C "$(dirname "$PWD")" "$(basename "$PWD")"
cp /tmp/head.sh deploy/agy/install.sh
base64 -w0 /tmp/src.tar.gz >> deploy/agy/install.sh
echo >> deploy/agy/install.sh
```

两个坑：

1. **`--exclude='deploy/agy'` 不能漏**。这个目录本身就存着上一份 `install.sh` 和 `tar.gz`，不排除的话每打一次包就把上一份嵌进去，体积翻倍（实测漏掉一次：556KB → 1.7MB）。
2. 打包时源码目录名需为 `shellfb-src`，脚本解压后按这个路径编译。

打完包验证三件事，别只看体积：

```bash
tail -n +$(( MARKER + 1 )) deploy/agy/install.sh | base64 -d | tar -xz -C /tmp/check
tar tzf /tmp/src.tar.gz | grep -c 'deploy/agy'   # 必须是 0
cd /tmp/check/shellfb-src && go build ./...
```
