# 飞书适配器

[English](feishu-adapter.md) | [简体中文](feishu-adapter.zh-CN.md)

`feishu-adapter` 是连接飞书回调与 Paper Agent HTTP API 的独立 sidecar。飞书凭据、事件验签、卡片渲染和 `chat_id -> session_id` 映射留在适配器中；LLM Provider、工具权限和 Agent Loop 仍由 `paper-agent-server` 负责。

## 能力范围

- 接收 `/feishu/events` 消息事件与 `/feishu/card-callback` 审批事件
- 用 `tenant_key + chat_id` 维护持久 Session，同一会话可以跨消息继续
- 支持文本、富文本、回复/引用消息以及 JPEG、PNG、GIF、WebP 图片
- 异步提交任务、轮询状态、取消任务，并把最终结果写回飞书
- 遇到 `pending_approval` 时发送可交互审批卡片
- 支持 `/help`、`/new`、`/status`、`/cancel`
- 忽略只有 `@all` 的群消息，显式 `@机器人` 才创建任务

当前使用 HTTP callback；尚未实现飞书长连接和加密回调载荷。

消息与审批链路按 Verdent CLI 的飞书 sidecar 适配到 Paper Agent API；Verdent 的 scheduler notifier 不在本项目中，因为 Paper Agent 当前没有定时任务子系统。

## 构建

在仓库根目录执行：

```bash
make build
```

产物位于：

```text
dist/bin/paper-agent
dist/bin/paper-agent-server
dist/bin/feishu-adapter
```

## 启动 Paper Agent

API Key 只配置在 Agent Server 进程，不要传给飞书适配器：

```bash
export OPENAI_API_KEY="..."
export OPENAI_BASE_URL="https://your-openai-compatible-host/v1"
export OPENAI_MODEL="your-model-id"
export PAPER_AGENT_ACCESS_ID="replace-with-a-random-access-id"

dist/bin/paper-agent-server -provider openai
```

默认监听 `127.0.0.1:18080`。`PAPER_AGENT_ACCESS_ID` 启用登录换取 Bearer token；未设置时只适合本机受控环境。

## 启动适配器

```bash
export FEISHU_APP_ID="cli_xxx"
export FEISHU_APP_SECRET="..."
export FEISHU_VERIFICATION_TOKEN="..."
export PAPER_AGENT_BASE_URL="http://127.0.0.1:18080"
export PAPER_AGENT_ACCESS_ID="replace-with-the-same-access-id"

dist/bin/feishu-adapter --addr :18790 --mode agent
```

| Flag | Env | Default | 说明 |
| --- | --- | --- | --- |
| `--addr` | `FEISHU_ADAPTER_ADDR` | `:18790` | 回调监听地址 |
| `--feishu-app-id` | `FEISHU_APP_ID` | | 飞书应用 ID |
| `--feishu-app-secret` | `FEISHU_APP_SECRET` | | 飞书应用 Secret |
| `--feishu-verification-token` | `FEISHU_VERIFICATION_TOKEN` | | 回调验签 Token；配置后不匹配请求返回 401 |
| `--paper-agent-url` | `PAPER_AGENT_BASE_URL` | `http://127.0.0.1:18080` | Agent HTTP API |
| `--paper-agent-access-id` | `PAPER_AGENT_ACCESS_ID` | | Agent 登录凭据 |
| `--db` | `FEISHU_ADAPTER_DB` | `~/.config/paper-agent/feishu-adapter.db` | 会话映射 SQLite |
| `--poll-interval` | `FEISHU_ADAPTER_POLL_INTERVAL` | `2s` | 状态轮询间隔 |
| `--poll-timeout` | `FEISHU_ADAPTER_POLL_TIMEOUT` | `30m` | 单任务轮询上限 |
| `--auto-approve` | `PAPER_AGENT_FEISHU_AUTO_APPROVE` | `false` | 自动批准工具，仅限可信环境 |

## 飞书控制台

需要启用机器人，并配置：

- 事件回调：`https://<public-host>/feishu/events`
- 卡片回调：`https://<public-host>/feishu/card-callback`
- 事件：`im.message.receive_v1`
- 权限：接收消息、以机器人发送消息；使用审批卡时还需卡片写权限
- 读取引用和图片时，需要允许读取对应单聊或群聊消息及 message resource

适配器在内网运行时，应由 HTTPS 反向代理或 tunnel 暴露回调地址，并尽量让 Agent Server 继续绑定 loopback。

## Session 与图片

普通文本会以异步任务提交。同一飞书会话后续消息复用 Session，输入 `/new` 才生成新 Session。回复或引用消息时，适配器优先读取 `parent_id`，再读取 `root_id`，将引用正文与当前消息合并；引用卡片只保留可见标题、正文、字段和按钮标签，不传递按钮内部 payload。

图片通过飞书 message-resource API 下载，引用图片排在当前消息图片之前。单次最多 4 张、每张最多 8 MiB、合计最多 20 MiB。下载或格式校验失败时不会静默丢图提交，而是明确返回错误。

## 审批与边界

工具等待审批时，适配器发送 Approve/Reject 卡片；点击后调用 `/api/agent/approve`，成功后更新原卡片并禁用按钮。任务最终结果以卡片发回，失败时回退为纯文本。

生产环境保持 `--auto-approve=false`。飞书只是输入入口，工具 allowlist、风险等级、超时、输出预算和最终权限判断都在 Paper Agent Runtime 内完成。长输出会为飞书截断，完整历史仍保存在本地 Session SQLite 中。
