# Papaya Telegram Bot

简体中文 | [English](#english-version)

## 项目简介

Papaya 是一个使用 Go 编写的 Telegram 机器人，支持每日签到赚取积分、与 OpenAI 格式接口聊天，以及管理员对积分与模型的管理。项目内置 Dockerfile 和 docker-compose，开箱即用。

### 功能

- **每日签到**：东八区（CST+8）每天可签到一次，获取积分。
- **积分查询**：随时查看自己的积分余额。
- **聊天**：与机器人聊天会消耗积分，后台对接 OpenAI 格式 API（可自定义 BaseURL、Model）。
- **管理员面板**：查看用户列表及积分，调整积分，设置模型，赋予管理员权限。

### 命令

| 命令 | 说明 |
| --- | --- |
| `/start` `/help` | 查看帮助 |
| `/checkin` | 每日签到，获取积分 |
| `/points` `/me` | 查看当前积分 |
| *(非命令文本)* | 与机器人聊天（消耗积分） |
| `/users` | 管理员：查看用户列表及签到信息 |
| `/addpoints <user_id> <delta>` | 管理员：增减用户积分 |
| `/setpoints <user_id> <points>` | 管理员：设定用户积分 |
| `/setmodel <model>` | 管理员：设置调用的模型 |
| `/ratelimit` | 管理员：查看当前聊天速率限制 |
| `/setratelimit <per_minute>` | 管理员：设置每分钟聊天次数上限（0 表示不限） |
| `/setadmin <user_id>` | 管理员：赋予管理员权限 |

## 快速开始

### 环境变量

| 变量名 | 必填 | 说明 |
| --- | --- | --- |
| `TG_BOT_SECRET` | 是 | Telegram Bot Token |
| `TG_ADMIN_IDS` | 否 | 预设管理员 ID，逗号或空格分隔 |
| `OPENAI_API_KEY` | 否 | OpenAI 风格接口的 API Key |
| `OPENAI_BASE_URL` | 否 | OpenAI 接口 Base URL |
| `OPENAI_MODEL` | 否 | 默认模型名，未设置则为 `gpt-3.5-turbo` |
| `DATA_FILE` | 否 | 数据库存储路径，默认 `data.db` |

### 本地运行

```bash
export TG_BOT_SECRET=your_token
go run ./...
```

### Docker 运行

```bash
docker compose up --build -d
```

容器会在 `/app/data/papaya.db` 保存数据，可通过挂载卷 `papaya-data` 持久化。

## 目录结构

- `main.go`：入口，加载配置并启动机器人。
- `internal/config`：环境变量配置加载。
- `internal/store`：基于 BoltDB 的用户与设置存储。
- `internal/chat`：OpenAI 风格聊天管理与上下文维护。
- `internal/bot`：Telegram 指令与聊天逻辑。
- `Dockerfile` / `docker-compose.yml`：容器化支持。

---

## English Version

### Overview

Papaya is a Telegram bot written in Go. It offers daily check-ins for points, OpenAI-style chat, and admin controls for points and model selection. Dockerfile and docker-compose are included for an out-of-the-box experience.

### Features

- **Daily check-in**: Once per day in UTC+8 timezone to earn points.
- **Points query**: Check your current balance at any time.
- **Chat**: Talk to the bot; each reply costs points. Works with OpenAI-compatible APIs (custom base URL/model supported).
- **Admin tools**: List users with points, adjust balances, set the model, and grant admin rights.

### Commands

| Command | Description |
| --- | --- |
| `/start` `/help` | Show help |
| `/checkin` | Daily check-in for points |
| `/points` `/me` | Show current points |
| *(non-command text)* | Chat with the bot (spends points) |
| `/users` | Admin: list users and check-ins |
| `/addpoints <user_id> <delta>` | Admin: add/subtract points |
| `/setpoints <user_id> <points>` | Admin: set points |
| `/setmodel <model>` | Admin: configure the model |
| `/ratelimit` | Admin: view current chat rate limit |
| `/setratelimit <per_minute>` | Admin: set chat limit per minute (0 to disable) |
| `/setadmin <user_id>` | Admin: grant admin role |

### Environment Variables

See the table above; the same names apply.

### Run Locally

```bash
export TG_BOT_SECRET=your_token
go run ./...
```

### Docker

```bash
docker compose up --build -d
```

Data persists under `/app/data/papaya.db` via the `papaya-data` volume.
