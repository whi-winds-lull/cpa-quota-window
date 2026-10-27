# cpa-quota-window

CLIProxyAPI（CPA）原生插件。按账号维护每日触发计划，用一次最小请求把该账号的 5 小时额度窗口起点固定到指定时刻。

支持 Codex 与 Gemini / Antigravity 两类上游账号。

- 仓库：<https://github.com/whi-winds-lull/cpa-quota-window>
- 预编译产物：<https://github.com/whi-winds-lull/cpa-quota-window/releases>
- 许可：MIT

---

## 与 cpa-window-starter 的区别

本项目在 [cpa-window-starter](https://github.com/ZenGeekLabs/cpa-window-starter) 的调度与 ABI 骨架基础上重构，主要差异如下。

| 维度 | cpa-window-starter | cpa-quota-window |
| --- | --- | --- |
| 计划作用域 | 每个 provider 一个计划；该计划下的账号共享一组 `times` 与一个 `model` | 每个账号一条计划，各自持有 `times` 与 `model` |
| 触发行为 | 到达触发时刻后，遍历该 provider 下所有已选账号逐个发送 | 到达触发时刻后，仅向该条计划对应的账号发送 |
| 配置结构 | 扁平的 `times` / `accounts` / `model` | `plans[]`，元素为 `{provider, auth_id, model, times}` |
| 账号发现 | 需在页面勾选账号，`auth_id` 由勾选写入 | 自动列出 CPA 中全部账号；未配置的账号以空计划行呈现，填写时间即可 |
| 只读视图 | 未取得管理密钥时仅显示连接提示 | 额外提供 `status.json` 与 `accounts.json` 两个免鉴权资源，未取得密钥时仍可查看计划与执行记录 |
| 历史记录 | 支持按日分页、勾选删除 | 同左 |

相同的部分：ABI 实现方式、`host.auth.list` / `host.model.execute` 调用路径、状态文件去重策略、5 小时窗口重置时间的读取方式。

---

## 功能

- **按账号独立计划**：每个账号持有各自的时间列表与模型，互不影响。
- **自动补全账号**：页面按 CPA 的账号列表渲染，未配置的账号自动出现，无需手填 `auth_id`。
- **多上游支持**：Codex 与 Gemini / Antigravity 各自独立计划，互不覆盖。
- **执行记录**：记录每次触发的计划时刻、实际执行时刻、结果、上游返回的窗口重置时间。
- **历史管理**：按日期筛选、分页、勾选删除。
- **界面适配**：跟随 CPA 的语言（中 / 英）与明暗主题。
- **时区**：IANA 时区，支持夏令时。
- **后台执行**：保存后关闭浏览器仍然生效，前提是 CPA 进程持续运行。

---

## 安装

### 前提

- CPA 需支持原生动态库插件，管理接口响应头包含 `X-CPA-SUPPORT-PLUGIN: 1`。
- 需要 `host.auth.list`、`host.model.execute` 两个宿主回调。
- 配置中 `plugins.enabled` 必须为 `true`。

### 步骤

1. 从 Releases 下载对应平台的压缩包，核对 `checksums.txt` 中的 SHA-256。

   | CPA 所在平台 | 安装包 |
   | --- | --- |
   | Linux x86_64 | `cpa-quota-window_<version>_linux_amd64.zip` |
   | Linux ARM64 | `cpa-quota-window_<version>_linux_arm64.zip` |
   | macOS Intel | `cpa-quota-window_<version>_darwin_amd64.zip` |
   | macOS Apple Silicon | `cpa-quota-window_<version>_darwin_arm64.zip` |
   | Windows x86_64 | `cpa-quota-window_<version>_windows_amd64.zip` |

2. 将动态库放入 CPA 的插件目录，遵循宿主的路径约定：

   ```
   plugins/<GOOS>/<GOARCH>/cpa-quota-window.so     # Linux
   plugins/<GOOS>/<GOARCH>/cpa-quota-window.dylib  # macOS
   plugins/<GOOS>/<GOARCH>/cpa-quota-window.dll    # Windows
   ```

3. 在 `config.yaml` 的 `plugins.configs` 下增加 `cpa-quota-window` 配置块（见下节）。

4. 重启 CPA，或在管理页重新加载插件。

> Docker 部署时注意 `plugins` 目录是否已挂载到宿主机，否则重建容器会丢失动态库。

---

## 配置

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    cpa-quota-window:
      enabled: true
      priority: 1
      timezone: "Asia/Shanghai"
      state_file: "plugins/state/cpa-quota-window.json"
      plans:
        - provider: "codex"
          auth_id: "codex-11111111-alice@example.com-plus.json"
          model: "gpt-6-luna"
          times: ["05:30"]

        - provider: "antigravity"
          auth_id: "antigravity-22222222-bob@example.com.json"
          model: "gemini-2.5-pro"
          times: ["07:00"]
```

### 字段说明

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `timezone` | 是 | IANA 时区名。**留空会被解析为 UTC**，导致定时整体偏移。 |
| `state_file` | 否 | 执行记录与去重状态的存放路径，所属目录需可写。默认 `plugins/state/cpa-quota-window.json`。 |
| `plans[].provider` | 否 | `codex` 或 `antigravity`；省略时按 `codex` 处理。 |
| `plans[].auth_id` | 是 | CPA 中的账号 ID，取值为认证文件名，例如 `codex-xxxx@example.com-plus.json`。不是邮箱，也不是内部索引。 |
| `plans[].model` | 是 | 该账号实际支持的模型名。填写不支持的值会在触发时返回 400 / 404。 |
| `plans[].times` | 是 | 每日触发时刻，`HH:MM`，最多 24 个。同一账号内不得重复。 |
| `plans[].enabled` | 否 | 省略视为启用。 |

### 时间点的间隔约束

上游的 5 小时窗口在窗口期内不会重置。同一账号配置的多个时间点若间隔不足 5 小时，后一个时间点的请求落在已打开的窗口内，只会消耗额度，不会产生新的窗口。

---

## 使用

打开资源页：

```
http://<CPA 地址>:<端口>/v0/resource/plugins/cpa-quota-window/status
```

页面由三部分组成：

1. **时区与总开关**：全局时区与定时开关。
2. **选择账号**：按套餐分组，每个账号一行，含启用勾选、时间列表、模型、下次触发时刻。
3. **触发记录**：按日期筛选的执行结果，包含计划时刻、执行结果、上游返回的窗口重置时间。

操作流程：填写时间 → 点击「保存计划」→ 确认顶部「下次自动触发」显示预期时刻。

「立即触发一次」用于手动验证，会向所有已配置时间的账号各发送一次请求。

---

## 管理接口

除资源页外，插件注册了以下管理接口（位于 `/v0/management`，需管理密钥）：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/plugins/cpa-quota-window/status` | 计划列表、下次触发时刻、执行记录 |
| GET | `/plugins/cpa-quota-window/accounts` | CPA 账号列表 |
| POST | `/plugins/cpa-quota-window/run` | 立即触发；`{"auth_id": ""}` 表示全部 |
| POST | `/plugins/cpa-quota-window/history/delete` | 按 ID 删除执行记录；`{"ids": [...]}` |

免鉴权资源（位于 `/v0/resource/plugins/cpa-quota-window/`）：

| 路径 | 内容 |
| --- | --- |
| `/status` | 管理页面 |
| `/status.json` | 当前状态与执行记录 |
| `/accounts.json` | CPA 账号列表 |

---

## 安全说明

**`status.json` 与 `accounts.json` 不经过管理鉴权。** 这是为「未持有管理密钥时仍可查看计划」而设计的，代价是这两个路径可被任何能访问 CPA 端口的客户端读取。

其中包含：

- 账号 ID（认证文件名，通常含邮箱）
- 账号显示名（通常为邮箱）
- 各账号的模型名、触发时刻
- 执行记录（时间、结果、窗口重置时间）

不包含：OAuth token、refresh token、API Key、管理密钥。

部署建议：

- 不要将 CPA 端口直接暴露到公网。
- 如必须外网访问，在反向代理层为该前缀增加访问控制。
- 管理密钥与本插件的配置无关，插件不读取、不存储凭证文件。

---

## 限制

- **不增加额度**。每次触发是一次真实模型请求，会消耗 token 与请求次数；插件只改变窗口起点，不改变窗口容量。
- **不保证上游行为**。5 小时窗口的重置规则由上游决定。Codex 的窗口长度与重置时刻可从响应头读取（`x-codex-primary-window-minutes` 等），记录中显示为实际值；Gemini / Antigravity 未公开对应机制，记录中的重置时间通常为「未确认」。
- **不补发**。超过 2 分钟的宽限期后，错过的触发标记为 `missed`，不会延后执行。
- **依赖 CPA 进程**。定时器运行在 CPA 进程内，进程停止期间不会触发。
- **周额度独立**。插件不感知周额度，高强度使用时可能先触及周上限。

---

## 开发

构建需要 Go 1.24+ 与 C 编译器（CGO）。

```bash
# Linux
CGO_ENABLED=1 go build -buildmode=c-shared -o dist/linux_amd64/cpa-quota-window.so .

# macOS
CGO_ENABLED=1 go build -buildmode=c-shared -o dist/darwin_arm64/cpa-quota-window.dylib .

# Windows
CGO_ENABLED=1 go build -buildmode=c-shared -o dist/windows_amd64/cpa-quota-window.dll .
```

跨平台构建与打包：

```bash
sh scripts/build.sh linux arm64      # 指定目标平台
python3 scripts/package.py           # 生成 release/ 下的 zip 与 checksums.txt
```

### 测试

`tests/host_sim.py` 是一个宿主模拟器：通过 ctypes 加载动态库，构造 CPA 的 C ABI 与回调，完整执行注册、配置解析、计划渲染、账号列举、手动触发、负向校验等 42 项断言。

```bash
python3 tests/host_sim.py dist/windows_amd64/cpa-quota-window.dll
```

`tests/mock_cpa.py` 启动一个模拟 CPA 的 HTTP 服务，用于在浏览器中验证页面渲染，管理接口固定返回 401，因此它只能验证只读降级路径。

```bash
python3 tests/mock_cpa.py 18765
```

### 发布

推送 `v*` 标签会触发 GitHub Actions，构建五个平台的动态库、打包并创建 Release。

```bash
git tag -a v0.4.3 -m "cpa-quota-window v0.4.3"
git push origin v0.4.3
```
