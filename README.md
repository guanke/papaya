# Papaya Bot

简体中文 | [English](#english-version)

## 项目简介

Papaya 是一个使用 Go 编写的双平台（Telegram & Discord）机器人。它集成了 OpenAI (GPT-4 Vision) 能力，支持每日签到、积分系统、个性化聊天、长短期记忆自动总结，以及基于 Cloudflare R2 的媒体管理。

项目内置 Dockerfile 和 docker-compose，开箱即用。

### 核心功能

- **多平台支持**：同时支持 Telegram 和 Discord（只需配置对应 Token）。
- **智能聊天**：
  - **个性化人设**：用户可自定义机器人对自己的称呼和性格 (`/setpersona`)。
  - **长久记忆**：自动总结过往对话，保持长期记忆同时节省 Token。
  - **智能识图**：集成 GPT-4 Vision，支持图片内容分析与自动打标。
- **积分系统**：每日签到赚取积分，聊天消耗积分（可配置）。
- **媒体管理**：
  - 支持随机查看美图/视频 (`/image`)。
  - 管理员可通过 Cloudflare R2 上传、管理媒体文件。
  - 支持新媒体自动推送订阅。
- **系统特性**：
  - **结构化日志**：基于 `log/slog` 的 JSON 日志。
  - **持久化存储**：使用 BoltDB 存储用户数据、对话历史和配置。

### 命令列表 (Telegram)

| 命令 | 说明 | 权限 |
| --- | --- | --- |
| `/start` `/help` | 查看帮助 | 全员 |
| `/checkin` | 每日签到，获取积分 | 全员 |
| `/points` `/me` | 查看当前积分 | 全员 |
| `/setpersona <text>` | 设置只属于你的机器人人设 | 全员 |
| `/image` | 随机查看一张美图或视频 | 全员 |
| `/vision` | 开启/关闭智能识图功能 | 管理员 |
| `/users` | 查看用户列表 | 管理员 |
| `/addpoints <id> <val>` | 增减用户积分 | 管理员 |
| `/setpoints <id> <val>` | 设定用户积分 | 管理员 |
| `/setmodel <model>` | 切换 OpenAI 模型 | 管理员 |
| `/ratelimit` | 查看速率限制 | 管理员 |
| `/r2upload` | 回复图片上传至 R2 并自动打标 | 管理员 |
| `/r2list` `/r2del` | R2 文件管理 | 管理员 |
| `/sub` `/unsub` | 订阅/取消订阅新图推送 | 管理员 |

*注：Discord 目前支持 `/checkin`, `/points`, `/help` 及直接对话功能。*

## 快速开始

### 环境变量

| 变量名 | 必填 | 说明 |
| --- | --- | --- |
| `TG_BOT_SECRET` | 否 | Telegram Bot Token (若为空则不启动 TG 机器人) |
| `DISCORD_TOKEN` | 否 | Discord Bot Token (若为空则不启动 Discord 机器人) |
| `TG_ADMIN_IDS` | 否 | Telegram 管理员 ID 列表 (逗号分隔) |
| `OPENAI_API_KEY` | 是 | OpenAI API Key |
| `OPENAI_BASE_URL` | 否 | 自定义接口地址 |
| `OPENAI_MODEL` | 否 | 默认模型 (如 `gpt-4-vision-preview`) |
| `R2_ACCOUNT_ID` | 否 | Cloudflare R2 Account ID |
| `R2_ACCESS_KEY_ID` | 否 | Cloudflare R2 Access Key |
| `R2_SECRET_ACCESS_KEY` | 否 | Cloudflare R2 Secret Key |
| `R2_BUCKET_NAME` | 否 | R2 Bucket 名称 |
| `R2_PUBLIC_URL` | 否 | R2 公开访问域名 (用于图片链接) |

### Docker 运行 (推荐)

1. 创建 `compose.yml` (参考仓库中的 `compose.yml`)。
2. 配置环境变量。
3. 运行：

```bash
docker compose up --build -d
```

### 本地运行

```bash
go mod download
go run ./...
```

## 目录结构

- `main.go`: 程序入口，并发启动多平台机器人。
- `internal/telegram`: Telegram 机器人实现。
- `internal/discord`: Discord 机器人实现。
- `internal/chat`: 核心聊天逻辑 (OpenAI, 记忆, 总结, 识图)。
- `internal/store`: BoltDB 数据存储 (用户, 历史, 媒体)。
- `internal/r2`: Cloudflare R2 存储集成。
- `internal/config`: 配置加载。
- `internal/logger`: 结构化日志封装。

---

## English Version

### Overview

Papaya is a dual-platform (Telegram & Discord) bot written in Go. It integrates OpenAI (GPT-4 Vision) capabilities and features daily check-ins, a points system, personalized chat, auto-summarization of chat history, and media management based on Cloudflare R2.

### Key Features

- **Dual Platform**: Supports Telegram and Discord simultaneously.
- **Smart Chat**:
  - **Persona**: Customize how the bot talks to you (`/setpersona`).
  - **Long-term Memory**: Auto-summarizes past conversations.
  - **Vision**: Analyzes and tags images using GPT-4 Vision.
- **Points System**: Earn points via check-ins, spend them on chat.
- **Media Management**:
  - Random image/video viewing (`/image`).
  - Admin management via Cloudflare R2 (`/r2upload`).
  - Subscription system for new media alerts.
- **System**:
  - **Structured Logging**: JSON logs via `log/slog`.
  - **Persistence**: BoltDB for data storage.

### Commands (Telegram)

| Command | Description | Role |
| --- | --- | --- |
| `/checkin` | Daily check-in | User |
| `/points` | Check points | User |
| `/setpersona` | Customize bot personality | User |
| `/image` | View random media | User |
| `/vision` | Toggle AI Vision | Admin |
| `/r2upload` | Upload reply-media to R2 | Admin |
| ... | (See Help for more) | ... |

*Note: Discord currently supports `/checkin`, `/points`, `/help` and direct chat.*

### configuration

See the environment variable table above. Add `DISCORD_TOKEN` to enable Discord support.

### Run with Docker

```bash
docker compose up --build -d
```
