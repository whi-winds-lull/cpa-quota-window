# 部署到 CPA（Linux 服务器）

从零到验证通过，按顺序做。假设你已经下载了 `window-anchor_0.1.0_linux_amd64.zip`。

---

## 第 0 步：先确认三件事

在服务器上执行，把结果记下来：

```bash
# 1. 架构（决定下哪个包）
uname -m
# x86_64 → linux_amd64；aarch64 → linux_arm64

# 2. CPA 是怎么跑的（决定怎么重启）
ps -ef | grep -i "cli-proxy\|cliproxy" | grep -v grep
docker ps --format '{{.Names}}\t{{.Image}}' 2>/dev/null | grep -i proxy
systemctl list-units --type=service | grep -i proxy

# 3. CPA 版本（低于 8.0 不支持原生插件）
curl -sI http://127.0.0.1:8317/v0/management/plugins | grep -i x-cpa-support-plugin
```

第 3 条必须返回 `x-cpa-support-plugin: 1`。返回 `0` 或没有这个头，说明当前二进制不支持插件，要先升级 CPA。

---

## 第 1 步：上传并解压

**本地 Windows**（PowerShell）：

```powershell
scp D:\Downloads\window-anchor_0.1.0_linux_amd64.zip root@你的服务器IP:/tmp/
```

也可以直接用 Xftp 拖到 `/tmp`。

**服务器上**：

```bash
cd /tmp
unzip -o window-anchor_0.1.0_linux_amd64.zip
ls -lh window-anchor.so
# 应该是 2 MB 左右
file window-anchor.so
# 应该显示 ELF 64-bit LSB shared object, x86-64
```

可选：核对校验和

```bash
sha256sum window-anchor.so
# 和 checksums.txt 里 window-anchor_0.1.0_linux_amd64.zip 那行比对
```

---

## 第 2 步：找到 CPA 的目录

### 情况 A：Docker 部署

```bash
docker ps --format '{{.Names}}' | grep -i proxy
# 假设容器名是 cli-proxy-api

docker inspect cli-proxy-api --format '{{range .Mounts}}{{.Source}} -> {{.Destination}}{{"\n"}}{{end}}'
```

会看到类似：

```
/opt/cliproxyapi/config.yaml -> /app/config.yaml
/opt/cliproxyapi/auth -> /app/auth
/opt/cliproxyapi/plugins -> /app/plugins
```

**宿主机上的 `/opt/cliproxyapi/` 就是你要改的地方**，插件放到 `/opt/cliproxyapi/plugins/`。

如果 plugins 目录没有挂载出来，需要先在 `docker-compose.yml` 或 `docker run` 里加一条挂载，否则重启容器后插件会丢。

### 情况 B：直接跑二进制 / systemd

```bash
systemctl cat cliproxyapi | grep ExecStart
# 或者
ps -ef | grep cli-proxy | grep -v grep
```

从启动命令或 `WorkingDirectory` 找到 CPA 的工作目录，通常就是 `config.yaml` 所在目录。插件放进该目录下的 `plugins/`。

---

## 第 3 步：放插件

```bash
# Docker 情况
cp /tmp/window-anchor.so /opt/cliproxyapi/plugins/
chmod 644 /opt/cliproxyapi/plugins/window-anchor.so

# 非 Docker 情况
cp /tmp/window-anchor.so /你的CPA目录/plugins/
```

确认 `plugins` 目录和 `config.yaml` 里的 `plugins.dir` 一致（默认就是 `plugins`）。

---

## 第 4 步：改 config.yaml

先备份：

```bash
cp /opt/cliproxyapi/config.yaml /opt/cliproxyapi/config.yaml.bak-$(date +%Y%m%d)
```

找到 `plugins:` 这一段。**只往里加 `window-anchor`，不要动已有的 `cpa-window-starter` 配置结构**。

```yaml
plugins:
  enabled: true          # 必须为 true，否则插件不会加载
  dir: "plugins"
  configs:
    # ↓ 已有的话保留，但建议把它的定时关掉，见第 5 步
    cpa-window-starter:
      enabled: false     # ← 建议改这里

    # ↓ 新增这一段
    window-anchor:
      enabled: true
      priority: 1
      timezone: "Asia/Shanghai"          # 不能留空，空值会被解析成 UTC
      state_file: "plugins/state/window-anchor.json"
      plans:
        - auth_id: "你的账号A的auth_id"
          model: "gpt-6-luna"
          times: ["05:30"]
        - auth_id: "你的账号B的auth_id"
          model: "gpt-6-luna"
          times: ["08:30"]
```

### auth_id 从哪来

在 CPA 管理页 → 账号/凭证列表，每一行的 ID 列，形如 `11111111-1111-4111-8111-111111111111`。

或者先只填一个占位值，启动后打开插件页面，用"添加账号"下拉勾选，会自动填上正确的 auth_id。

### model 填什么

必须是该账号真实支持的最便宜模型。填错的表现是每次触发都返回 400/404。

不确定的话，先在管理页看该账号可用模型列表，或者临时填一个小模型试手动触发。

---

## 第 5 步：停掉旧插件的定时

`cpa-window-starter` 和 `window-anchor` 会同时给同一个账号发请求。

旧插件现在的配置是"一个账号 + 每天 05:30/08:30 两个时间点"，**08:30 那次落在 05:30 开出的 5h 窗口内，只会烧额度不会开新窗**。

所以要么把它的 `enabled` 设为 `false`，要么把 `schedule_enabled` 设为 `false`。二选一，别两个插件同时跑同一个账号。

---

## 第 6 步：重启并确认加载

```bash
# Docker
docker restart cli-proxy-api

# 非 Docker
systemctl restart cliproxyapi
```

等 5 秒，确认插件注册成功：

```bash
K="你的management密钥"   # config.yaml 里的 remote-management.secret-key

curl -s http://127.0.0.1:8317/v0/management/plugins \
  -H "Authorization: Bearer $K" | python3 -m json.tool | grep -A8 window-anchor
```

要看到：

- `registered: true`
- `effective_enabled: true`

### 如果 registered 是 false

按顺序排查：

```bash
# 1. 二进制是否支持插件
curl -sI http://127.0.0.1:8317/v0/management/plugins | grep -i support

# 2. 架构对不对
file /opt/cliproxyapi/plugins/window-anchor.so

# 3. glibc 版本（包是 2.17 目标，正常系统都满足）
ldd --version | head -1

# 4. CPA 日志里有没有加载失败的记录
docker logs --tail 100 cli-proxy-api 2>&1 | grep -i plugin
```

---

## 第 7 步：打开页面确认计划

浏览器访问：

```
http://你的服务器:8317/v0/resource/plugins/window-anchor/
```

应该看到：

- 时区 `Asia/Shanghai`
- 每个账号一行，显示时间、模型、**下次触发时间**
- 下方是执行记录

如果提示"只读模式"，说明浏览器没读到 CPA 的登录态。此时可以在 CPA 原生管理页登录并勾"记住登录"，再刷新；或者直接用 `config.yaml` 改计划，**只读模式不影响后台调度**。

---

## 第 8 步：手动验证一次

先手动打一枪，确认真的能开窗：

```bash
curl -X POST http://127.0.0.1:8317/v0/management/plugins/window-anchor/run \
  -H "Authorization: Bearer $K" \
  -H "Content-Type: application/json" \
  -d '{"auth_id":"你的账号A的auth_id"}'
```

或者在页面上点该账号行的"触发"按钮。

**判据只有一个**：去 CPA 的账号配额页，看这个账号的 5h 重置时间有没有前移到「当前时间 + 5 小时」。

如果结果里显示 `429` 或"额度不足"，说明该账号当前本来就耗尽着，等它自然重置后再试。

---

## 第 9 步：确认明天的自动触发

页面上的"下次触发"应该显示明天 05:30（或你配置的时间）。

到时间后，执行记录里会出现一条 `success`，并带上游返回的 5h 重置时间。这条记录就是你判断锚定是否生效的唯一依据。

---

## 常见问题

**改了 config.yaml 但页面没变化**

CPA 对插件配置是热重载的，但部分版本需要重启。改完先等 3 秒刷新，不行就重启。

**state 文件写不进去**

`state_file` 的目录必须可写。Docker 部署时注意这个路径在容器内，要和挂载点对应：

```bash
# 确认容器内可写
docker exec cli-proxy-api sh -c 'mkdir -p /app/plugins/state && touch /app/plugins/state/.t && echo ok'
```

**同一账号配了两条计划**

会被配置校验拒绝（`账号 xxx 重复配置`）。一个账号只能有一条 plan，但一条 plan 里可以有多个时间点。

**一条 plan 里多个时间点**

间隔必须 ≥ 5 小时。否则第二次请求落在已有窗口内，不开新窗，纯烧额度。

**想临时关掉某个账号的定时**

把该 plan 的 `enabled` 设为 `false`，不用删掉整条。
