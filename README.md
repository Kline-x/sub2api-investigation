# Sub2API（定制版）

> 本项目基于 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 修改，遵循其 [LGPL-3.0 许可证](LICENSE)。
> 原项目：AI API 网关平台，将 AI 订阅配额分发和管理。功能介绍、架构说明等请阅读[上游 README](https://github.com/Wei-Shaw/sub2api#readme)。
> 本仓库相对上游的全部定制改动见 [CUSTOM_CHANGES.md](CUSTOM_CHANGES.md)。

## ⚠️ 重要声明（沿承上游）

- **服务条款风险**：使用本项目可能违反 Anthropic 等上游服务商的服务条款，使用前请自行确认，风险自负
- **合规使用**：仅限在符合所在国家/地区法律法规的前提下使用，严禁任何非法用途
- **免责**：本项目仅供技术学习与研究，因使用导致的封号、服务中断、数据丢失等一切直接或间接损失，作者不承担责任
- **无商业授权**：上游开发者从未授权任何个人或组织基于该项目开展商业运营；本定制版同样仅供自用

## 与上游的关系

- 定期合并上游正式版本，版本号 `vX.Y.Z-custom.N`（`X.Y.Z` = 上游基线，`N` = 定制序号）
- 管理台的「检查更新 / 立即更新 / 版本回滚」跟踪**本仓库**的 Releases，不再跟踪上游
- 定制功能：grok 免费额度耗尽封禁 24h、grok 裸 429 指数递增封禁、账号批量修改到期时间、筛选账号全选与 ID 列表 API 等（完整清单见 [CUSTOM_CHANGES.md](CUSTOM_CHANGES.md)）

## 部署

### 镜像

```
ghcr.io/kline-x/sub2api:<版本>    # 如 0.1.156-custom.2
ghcr.io/kline-x/sub2api:latest```

公开镜像，`docker pull` 免登录。仅 linux/amd64。

### Docker Compose（推荐）

```yaml
services:
  sub2api:
    image: ghcr.io/kline-x/sub2api:latest
    container_name: sub2api
    restart: unless-stopped        # 必须保留:面板在线更新/回滚依赖它自动拉起进程
    ports:
      - "8080:8080"
    environment:
      - AUTO_SETUP=true
      - SERVER_HOST=0.0.0.0
      - SERVER_PORT=8080
      - SERVER_MODE=release
      - DATABASE_HOST=db
      - DATABASE_PORT=5432
      - DATABASE_USER=sub2api
      - DATABASE_PASSWORD=<改成强密码>
      - DATABASE_DBNAME=sub2api
      - DATABASE_SSLMODE=disable
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - ADMIN_EMAIL=<管理员邮箱>
      - ADMIN_PASSWORD=<管理员密码>
      - JWT_SECRET=<至少32字节的随机串>   # 不足32字节启动直接失败
      - TZ=Asia/Shanghai
    depends_on:
      db:
        condition: service_healthy
      redis:
        condition: service_started

  db:
    image: postgres:16
    restart: unless-stopped
    environment:
      - POSTGRES_USER=sub2api
      - POSTGRES_PASSWORD=<与上面一致>
      - POSTGRES_DB=sub2api
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U sub2api"]
      interval: 3s
      timeout: 3s
      retries: 30

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    volumes:
      - redis_data:/data

volumes:
  postgres_data:
  redis_data:
```

```bash
docker compose up -d
# 首次启动自动建表并创建管理员,浏览器访问 http://<主机>:8080 登录
```

更多环境变量（代理、OAuth client、图片并发等）参考 [`deploy/docker-compose.standalone.yml`](deploy/docker-compose.standalone.yml) 的注释。

### 升级 / 回滚（日常，面板一键）

登录管理台 → 左上角**版本徽章**：

- **升级**：有新版本时点「立即更新」→ 完成后点「立即重启」→ 十几秒后自动恢复即新版本
- **回滚**：在「已是最新版本」状态下，面板底部「版本回滚」→ 选目标版本 → 回退 → 重启

原理是容器内二进制自替换 + 进程退出由 `restart: unless-stopped` 自动拉起，全程无需登录服务器。

两个边界（详见 [RELEASE_PROCESS.md](RELEASE_PROCESS.md)）：

- **重建容器会回到镜像版本**（在线更新发生在容器可写层）。大版本升级建议改 compose 里的镜像 tag 后 `docker compose up -d` 重建
- **从旧版（更新源仍指向上游官方的版本）升级到本仓库版本，必须手动拉镜像重建**，不能用面板更新——旧版的「立即更新」会更成上游官方版

### 已有数据升级注意

新版本首次启动会对数据库执行 **schema 迁移（不可逆）**，迁移后旧版本程序不保证兼容。接有数据的库之前先备份：

```bash
docker exec <postgres容器> pg_dump -U sub2api -d sub2api -F c -f /tmp/backup.dump
docker cp <postgres容器>:/tmp/backup.dump ./
```

## 发布新版本（维护者）

见 [RELEASE_PROCESS.md](RELEASE_PROCESS.md)。一句话版：验证通过后推附注标签 `vX.Y.Z-custom.N`，CI 自动构建 Release 资产与 GHCR 镜像；有问题删标签即撤版。

## 文档索引

| 文档 | 内容 |
|---|---|
| [RELEASE_PROCESS.md](RELEASE_PROCESS.md) | 发布规范：版本号、发版/撤版、合并上游、部署边界 |
| [CUSTOM_CHANGES.md](CUSTOM_CHANGES.md) | 定制改动记录 + 合并上游时必须保留的功能清单 |
| [AGENTS.md](AGENTS.md) | AI agent 工作指南（构建测试命令、硬性规则） |

## 许可证

[LGPL-3.0](LICENSE)，与上游一致。Copyright 归原作者 [Wei-Shaw](https://github.com/Wei-Shaw) 及贡献者所有；本仓库的修改部分同样以 LGPL-3.0 发布。