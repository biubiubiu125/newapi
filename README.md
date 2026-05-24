# newapi

`newapi` 是基于上游 `QuantumNous/new-api` 二次维护的多模型网关与 AI 资产管理系统。项目保留上游的统一 OpenAI 兼容中继、渠道管理、模型计费、用户额度、令牌管理、日志统计等核心能力，并在当前 fork 中补充了中文默认文档、源码构建部署、订阅套餐、支付网关、BEpusdt USDT 支付、Waffo Pancake 订阅支付、推广返佣、提现审核、充值审计、风控中心和新模板适配等功能。

当前仓库以中文说明为准。功能细节如果与上游文档不一致，请以本仓库当前代码、`docker-compose.yml`、前端页面和实际运行结果为准。

## 仓库地址

```bash
https://github.com/biubiubiu125/newapi.git
```

上游项目可作为背景参考：

```text
https://github.com/QuantumNous/new-api
https://docs.newapi.pro/zh/docs
```

从本 fork 部署时，请使用本仓库源码构建镜像，不要直接套用上游预构建镜像。

## 功能概览

### 多模型 API 网关

- 统一暴露 OpenAI 风格接口，支持 `/v1/chat/completions`、`/v1/responses`、`/v1/images/generations`、`/v1/images/edits`、`/v1/embeddings`、`/v1/audio/transcriptions`、`/v1/audio/translations`、`/v1/audio/speech`、`/v1/rerank` 等接口。
- 支持 Gemini 兼容入口 `/v1beta/models/*`，并保留 playground、dashboard billing、Midjourney、Suno、视频代理等扩展路由。
- 支持按模型、分组、渠道、优先级、权重和可用性进行请求分发。
- 支持流式响应、非流式响应、错误转换、上游模型列表、渠道可用性检测和余额检测。
- 支持请求日志、消费日志、token 统计、渠道缓存、模型倍率和供应商价格覆盖。

### 渠道与供应商管理

- 后台可配置渠道、模型、分组、优先级、权重、标签、多 key、模型同步和上游模型探测。
- 支持 OpenAI 兼容渠道以及项目内已有的 Claude、Gemini、Baidu、Tencent、Xunfei、Zhipu、Mistral、Cohere、Cloudflare、Dify、Jina、Palm、MokaAI 等适配器。
- 支持渠道测试、批量删除、禁用标签渠道、启用标签渠道、渠道能力修复、模型同步预览和同步应用。
- 支持 Codex OAuth 渠道授权、刷新和使用量查询。

### 用户、权限与安全

- 支持用户注册、登录、登出、密码重置、邮箱验证、OAuth、微信、Telegram、Passkey、2FA 和管理员用户管理。
- 支持普通用户、管理员、root 管理员等权限边界。
- 管理员可搜索、创建、更新、删除用户，管理用户绑定、重置 Passkey、禁用用户 2FA。
- 支持 Turnstile、关键接口限流、全局 API 限流、搜索限流、CORS 白名单和可信跳转域名。
- 支持支付合规确认，未确认前可锁定支付、兑换码、订阅计划和邀请奖励等资金相关功能。

### 令牌、额度与日志

- 用户可创建、更新、删除 API token，并查询 token key、token 列表和 token 使用情况。
- 管理员可查看全站日志、用户日志、日志统计、渠道缓存使用统计和额度日期统计。
- 支持用户额度、预消费额度、模型消耗、订阅抵扣、余额展示、货币展示和自定义货币符号。
- 支持按用户、分组、模型、渠道、请求类型进行计费和日志归档。

### 计费与模型价格

- 支持模型倍率、补全倍率、缓存倍率、图片倍率、音频倍率、工具价格和分组倍率。
- 支持固定倍率计费、按请求计费、分层表达式计费和上游价格同步。
- 管理端在 `系统设置 -> 计费设置` 中维护额度、货币展示、模型价格、分组价格和支付网关。
- 支持供应商价格覆盖、模型价格表、分层计费编辑器和上游价格同步预览。

### 钱包充值与订单

- 用户钱包支持预设金额、自定义金额、订单历史和支付方式选择。
- 充值订单由后端创建，前端传入金额只作为请求意图，后端根据配置计算支付金额、站内额度、币种和快照。
- 支付成功以服务端回调为准，不能依赖 `return_url` 或前端页面显示。
- 用户确认付款后会打开第三方支付页，原钱包页保留等待支付结果弹窗；支付回调成功后自动刷新余额、订单、订阅和返佣相关数据。
- 支持订单快照字段，包括支付网关、支付方式、实付金额、实付币种、价格、汇率、额度比例、折扣和返佣基数等关键口径。
- 重复回调、并发回调、跨网关回调、金额不一致、币种不一致和订单状态不允许流转时应被拒绝或幂等处理。

### 支付网关

当前代码包含以下支付能力：

- epay：支持支付宝、微信等 epay 支付方式，包含充值订单、订阅订单、return 和 notify。
- BEpusdt：对接 BEpusdt 兼容接口，创建 USDT 充值和订阅订单，支付页由 BEpusdt 侧处理链、币种、汇率和收款地址。
- Stripe：支持充值和订阅支付。
- Creem：支持充值和订阅支付。
- Waffo：支持充值支付。
- Waffo Pancake：支持充值和订阅支付，支持后台目录/产品绑定以及订阅套餐专属产品配置。

USDT 在本 fork 中采用“用户侧只展示一个 USDT 通道”的方式。newapi 后端固定创建 BEpusdt 订单，钱包前端只展示 `USDT` 按钮，具体链、币种、汇率和收款地址由 BEpusdt 支付页负责。这样避免在 newapi 钱包里重复展示 `USDT-TRC20`、`USDT-BEP20`、`USDT-Polygon` 等多个按钮。

### 订阅套餐

- 管理员可创建、修改、启停订阅套餐，配置价格、有效期、额度重置、优先级、总额度等信息。
- 用户可在钱包页面购买订阅套餐。
- 支持 epay、BEpusdt USDT、Stripe、Creem、Waffo Pancake 等订阅支付入口。
- 订阅支付创建时会固化套餐价格、货币、额度、有效期、支付方式和返佣快照。
- 订阅生效后可用于模型访问权限和额度消耗，用户可配置优先使用钱包或订阅额度。

### 推广返佣

- 用户可申请成为推广员，管理员可审批、拒绝、禁用、恢复和设置返佣比例。
- 推广员可获取邀请码和邀请链接。
- 被邀请人访问邀请链接后写入邀请 cookie，注册后绑定邀请关系。
- 支付成功后按订单快照生成佣金，佣金进入待结算状态。
- 支持佣金结算、待提现、冻结、提现申请、管理员审核、拒绝、打款和流水记录。
- 支持推广员账户、佣金列表、提现列表、后台概览、后台佣金列表、提现审核、审计日志和素材上传。

### 提现与结算

- 推广员可发起提现申请。
- 提现状态包括待审核、已通过、已打款、已拒绝、已取消等业务状态。
- 管理员可审核、拒绝、打款，打款时可记录交易号和付款凭证。
- 账户金额以返佣账户和流水为准，不直接混用用户 quota。
- 重复提现、并发提现、重复审核、重复打款和拒绝后非法回退应由状态机和事务保护。

### 风控中心与审计

- 风控中心聚合账户、IP、Token、订单、推广和支付审计信号。
- 管理员可扫描风险事件、查看详情、标记已查看、处理、忽略、禁用用户、恢复用户、禁用 Token 和维护白名单。
- 充值审计页展示充值/订阅订单的支付审计字段、异常摘要和回调信息，便于排查支付方式、回调 IP、服务器 IP、到账状态和返佣异常。
- 管理后台侧边栏角标基于服务端待处理数据和本地已读状态展示；点击处理入口后可清除本地未读提示，新数据到来后重新显示。

### 前端模板

- 默认模板：`web/default`，使用 React 19、TypeScript、Rsbuild、TanStack Router、TanStack Query、Tailwind CSS、Base UI。
- classic 模板：`web/classic`，使用 React 18、Vite、Semi UI、Ant Design。
- 后台可在站点设置中切换 `theme.frontend`。
- 当前重点维护新默认模板，后续如果剥离旧模板，应以默认模板功能完整性为准。
- 钱包、订阅、支付网关、推广返佣、提现审核等新功能主要适配默认模板。

### 管理后台

后台包含但不限于：

- 用户管理、用户搜索、用户绑定、Passkey/2FA 管理。
- 渠道管理、渠道测试、模型同步、渠道批量操作。
- token、日志、额度统计、数据统计。
- 兑换码管理。
- 模型元数据、供应商、部署管理。
- 系统设置、站点设置、计费设置、支付网关设置、侧边栏模块设置。
- 订阅套餐管理和用户订阅管理。
- 推广员审核、佣金管理、结算任务、提现审核、付款记录和审计日志。
- 充值审计、风控中心、待处理角标和管理员处理记录。

## 技术栈

- 后端：Go、Gin、GORM。
- 数据库：SQLite、MySQL、PostgreSQL。
- 缓存：Redis 或内存缓存。
- 默认前端：React 19、TypeScript、Rsbuild、Tailwind CSS、Base UI。
- classic 前端：React 18、Vite、Semi UI、Ant Design。
- 容器：Docker、Docker Compose。
- 构建：Dockerfile 使用 Bun 构建前端，Go 构建后端二进制。

## 目录结构

```text
.
├── common/                 通用工具、JSON、环境变量、Redis、限流、加密等
├── constant/               常量定义
├── controller/             HTTP 控制器、支付回调、用户和后台接口
├── dto/                    请求和响应 DTO
├── i18n/                   后端国际化资源
├── logger/                 日志
├── middleware/             鉴权、限流、CORS、分发、日志中间件
├── model/                  GORM 模型、迁移、订单状态机和账务更新
├── oauth/                  OAuth 供应商
├── pkg/                    内部包，例如 billingexpr
├── relay/                  AI API 中继与渠道适配
├── router/                 API、relay、web、dashboard 路由
├── service/                业务服务、返佣、支付、结算任务
├── setting/                系统配置、运营配置、模型配置
├── types/                  中继类型和通用类型
├── web/default/            默认新前端模板
├── web/classic/            classic 前端模板
├── docs/                   文档
├── Dockerfile              生产镜像构建
├── Dockerfile.dev          开发镜像构建
├── docker-compose.yml      生产/私有化部署示例
└── docker-compose.dev.yml  本地开发示例
```

## Docker Compose 快速部署

### 1. 准备环境

服务器建议安装：

- Docker
- Docker Compose plugin
- Git

最低配置取决于并发、渠道数量、日志量和数据库规模。小规模自用可以从 1 核 1G 起步，生产环境建议至少 2 核 4G，并使用持久化磁盘。

### 2. 拉取源码

```bash
git clone https://github.com/biubiubiu125/newapi.git
cd newapi
```

### 3. 创建 `.env`

```bash
cp .env.example .env
```

编辑 `.env`，至少修改：

```env
POSTGRES_USER=root
POSTGRES_PASSWORD=change_me_to_a_strong_random_postgres_password
POSTGRES_DB=new-api
REDIS_PASSWORD=change_me_to_a_strong_random_redis_password
SESSION_SECRET=change_me_to_a_strong_random_session_secret
APP_PORT=3000
CONTAINER_PREFIX=newapi
NODE_NAME=new-api-node-1
CORS_ALLOWED_ORIGINS=
TRUSTED_REDIRECT_DOMAINS=example.com
```

生产环境不要使用示例密码。`SESSION_SECRET` 必须使用足够长的随机字符串，多实例部署时保持一致。

可以用下面命令生成随机值：

```bash
openssl rand -hex 32
```

### 4. 启动

```bash
docker compose config
docker compose up -d --build
docker compose ps
```

默认访问：

```text
http://服务器IP:3000
```

如果使用反向代理，建议通过 HTTPS 对外提供服务。

### 5. 查看日志

```bash
docker compose logs -f new-api
```

### 6. 重启

```bash
docker compose restart new-api
```

### 7. 更新与回滚

更新前先确认工作区状态，生产环境建议先备份数据库：

```bash
cd /path/to/newapi
git status --short
mkdir -p backups
docker compose exec -T postgres pg_dump -U "${POSTGRES_USER:-root}" "${POSTGRES_DB:-new-api}" > "backups/newapi-$(date +%F-%H%M%S).sql"
```

拉取最新代码并重新构建：

```bash
git fetch --all --prune
git pull --ff-only
docker compose config
docker compose up -d --build
docker compose ps
docker compose logs --tail=100 new-api
curl -fsS http://127.0.0.1:${APP_PORT:-3000}/api/status
```

如果你本地有二次开发，请先提交或备份本地修改，不要直接覆盖。更新后至少验证管理员登录、普通用户登录、渠道测试、充值订单、支付回调、额度到账、推广返佣和提现审核。

如需回滚到旧版本，先确认要回滚的提交号，再在维护窗口执行：

```bash
git log --oneline -5
git checkout <old-commit>
docker compose up -d --build
docker compose ps
curl -fsS http://127.0.0.1:${APP_PORT:-3000}/api/status
```

如果本次更新已经执行了数据库结构变更，回滚前必须先确认数据库是否兼容旧代码，必要时从备份恢复。

## Docker Compose 说明

根目录 `docker-compose.yml` 默认包含：

- `new-api`：从当前源码构建镜像，镜像名 `biubiubiu125/newapi:local`。
- `postgres`：PostgreSQL 15，数据保存在 Docker volume `pg_data`。
- `redis`：Redis 7，仅在 compose 内部网络暴露。

默认端口：

- Web/API：`${APP_PORT:-3000}:3000`
- PostgreSQL：不映射宿主端口
- Redis：不映射宿主端口

默认挂载：

- `./data:/data`
- `./logs:/app/logs`
- `pg_data:/var/lib/postgresql/data`

关键环境变量：

| 变量 | 说明 |
| --- | --- |
| `POSTGRES_USER` | PostgreSQL 用户名 |
| `POSTGRES_PASSWORD` | PostgreSQL 密码，必须设置 |
| `POSTGRES_DB` | PostgreSQL 数据库名 |
| `REDIS_PASSWORD` | Redis 密码，必须设置 |
| `SESSION_SECRET` | 会话密钥，必须设置 |
| `APP_PORT` | 对外 Web/API 端口 |
| `CONTAINER_PREFIX` | 容器名前缀 |
| `NODE_NAME` | 节点名称 |
| `CORS_ALLOWED_ORIGINS` | CORS 允许来源，留空表示默认策略 |
| `TRUSTED_REDIRECT_DOMAINS` | 支付跳转可信域名，多个域名用逗号分隔 |
| `GLOBAL_API_RATE_LIMIT_ENABLE` | 是否启用全局 API 限流 |
| `GLOBAL_API_RATE_LIMIT` | 全局 API 限流次数 |
| `GLOBAL_API_RATE_LIMIT_DURATION` | 全局 API 限流窗口秒数 |
| `CRITICAL_RATE_LIMIT_ENABLE` | 是否启用关键接口限流 |
| `CRITICAL_RATE_LIMIT` | 登录、注册、支付等关键接口限流次数 |
| `CRITICAL_RATE_LIMIT_DURATION` | 关键接口限流窗口秒数 |

## 多机 / 多集群 Docker Compose 部署

本节参考上游官方集群部署教程整理，部署思路保持一致：多个 New API 节点共享同一个数据库、共享同一个 Redis，所有节点使用相同的 `SESSION_SECRET` 和 `CRYPTO_SECRET`，并通过 `NODE_TYPE` 区分主节点和从节点。

上游官方参考：<https://docs.newapi.pro/zh/docs/installation/deployment-methods/cluster-deployment>

下面内容按上游教程的结构整理，主要把源码仓库改为当前 fork：

- 源码仓库使用 `https://github.com/biubiubiu125/newapi.git`。
- 示例镜像从当前仓库源码构建为 `biubiubiu125/newapi:local`；如果你已经发布自己的镜像，可以改成自己的镜像 tag。
- 集群环境不要使用 SQLite。SQLite 只适合单容器试用或本地开发，多节点必须使用所有节点可访问的共享数据库。

### 前置要求

- 多台服务器，至少一台主节点和一台从节点。
- 每台应用服务器已安装 Docker 和 Docker Compose。
- 一个所有应用节点都能访问的共享 PostgreSQL 数据库。
- 一个所有应用节点都能访问的共享 Redis 服务。
- 可选：Nginx、HAProxy 或云负载均衡。

### 集群架构概述

New API 集群采用主从架构：

1. 主节点：处理写操作、部分读操作、数据库迁移和主节点任务。
2. 从节点：主要处理读请求和普通 Web/API 流量，提高整体吞吐量。

集群部署的关键配置：

1. 所有节点访问同一个数据库，`SQL_DSN` 必须一致。
2. 所有节点访问同一个 Redis，`REDIS_CONN_STRING` 必须一致。
3. 所有节点使用相同的 `SESSION_SECRET` 和 `CRYPTO_SECRET`。
4. 主节点使用 `NODE_TYPE=master` 或不设置 `NODE_TYPE`，从节点设置 `NODE_TYPE=slave`。

### 1. 准备共享数据库和 Redis

准备所有节点共用的 PostgreSQL 和 Redis。应用节点只需要拿到统一连接信息：

```env
SQL_DSN=postgresql://newapi:<db-password>@db.example.internal:5432/new-api
REDIS_CONN_STRING=redis://:<redis-password>@redis.example.internal:6379/0
```

如果使用 MySQL，`SQL_DSN` 使用 MySQL DSN，例如：

```env
SQL_DSN=newapi:<db-password>@tcp(db.example.internal:3306)/new-api?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai
```

### 2. 克隆当前仓库

```bash
mkdir -p /opt/newapi
cd /opt/newapi
git clone https://github.com/biubiubiu125/newapi.git .
git rev-parse HEAD
```

每台应用节点都使用同一个仓库版本或同一个镜像 tag。

### 3. 创建应用节点 Compose 文件

在每台应用服务器的 `/opt/newapi` 放置同一份 `docker-compose.cluster.yml`。这个文件只启动应用服务，不启动本地 PostgreSQL、Redis 或 SQLite。

```yaml
services:
  new-api:
    build: .
    image: biubiubiu125/newapi:local
    container_name: ${CONTAINER_PREFIX:-newapi}-${NODE_NAME:-node}
    restart: always
    ports:
      - "${APP_PORT:-3000}:3000"
    command: --log-dir /app/logs
    volumes:
      - ./data:/data
      - ./logs:/app/logs
    environment:
      # 所有节点必须使用同一个数据库和 Redis
      - SQL_DSN=${SQL_DSN:?set SQL_DSN}
      - REDIS_CONN_STRING=${REDIS_CONN_STRING:?set REDIS_CONN_STRING}
      # 多机部署必须显式设置，并保持所有节点一致
      - SESSION_SECRET=${SESSION_SECRET:?set SESSION_SECRET}
      - CRYPTO_SECRET=${CRYPTO_SECRET:?set CRYPTO_SECRET}
      # 主节点设置 master，从节点设置 slave
      - NODE_TYPE=${NODE_TYPE:-slave}
      - SYNC_FREQUENCY=${SYNC_FREQUENCY:-60}
      - TZ=${TZ:-Asia/Shanghai}
```

如果使用已经构建好的镜像，改成：

```yaml
services:
  new-api:
    image: your-registry/newapi:<tag>
```

### 4. 配置主节点

```env
APP_PORT=3000
CONTAINER_PREFIX=newapi
NODE_NAME=master-1
NODE_TYPE=master

SQL_DSN=postgresql://newapi:<db-password>@db.example.internal:5432/new-api
REDIS_CONN_STRING=redis://:<redis-password>@redis.example.internal:6379/0
SESSION_SECRET=<所有节点一致的强随机密钥>
CRYPTO_SECRET=<所有节点一致的强随机密钥>
```

主节点启动：

```bash
cd /opt/newapi
docker compose -f docker-compose.cluster.yml config
docker compose -f docker-compose.cluster.yml up -d --build
docker compose -f docker-compose.cluster.yml ps
curl -fsS http://127.0.0.1:${APP_PORT:-3000}/api/status
docker compose -f docker-compose.cluster.yml logs --tail=100 new-api
```

### 5. 配置从节点

从节点使用同一份 `docker-compose.cluster.yml`，只改 `.env`。`NODE_TYPE` 设置为 `slave`，`NODE_NAME` 每台机器唯一。`SQL_DSN`、`REDIS_CONN_STRING`、`SESSION_SECRET` 和 `CRYPTO_SECRET` 必须与主节点一致。

```env
APP_PORT=3000
CONTAINER_PREFIX=newapi
NODE_NAME=slave-1
NODE_TYPE=slave

SQL_DSN=postgresql://newapi:<db-password>@db.example.internal:5432/new-api
REDIS_CONN_STRING=redis://:<redis-password>@redis.example.internal:6379/0
SESSION_SECRET=<必须与主节点完全一致>
CRYPTO_SECRET=<必须与主节点完全一致>
```

每台从节点启动：

```bash
cd /opt/newapi
docker compose -f docker-compose.cluster.yml config
docker compose -f docker-compose.cluster.yml up -d --build
docker compose -f docker-compose.cluster.yml ps
curl -fsS http://127.0.0.1:${APP_PORT:-3000}/api/status
docker compose -f docker-compose.cluster.yml logs --tail=100 new-api
```

### 6. 启动顺序

1. 先启动共享数据库和 Redis，并确认应用节点能连通。
2. 启动主节点，确认日志中没有数据库迁移错误。
3. 逐台启动从节点，确认每台 `/api/status` 正常。
4. 所有节点健康后，再加入负载均衡。

### 7. 配置负载均衡

Nginx 示例：

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    '' close;
}

upstream newapi_cluster {
    least_conn;
    server 10.0.0.11:3000 max_fails=3 fail_timeout=30s;
    server 10.0.0.12:3000 max_fails=3 fail_timeout=30s;
    server 10.0.0.13:3000 max_fails=3 fail_timeout=30s;
}

server {
    listen 80;
    server_name your-domain.com;

    location / {
        proxy_pass http://newapi_cluster;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_buffering off;
        proxy_read_timeout 3600s;
    }
}
```

如果你的业务有长连接、流式输出或大响应，`proxy_buffering off` 和较长 `proxy_read_timeout` 很重要。生产环境还应配置 HTTPS、访问日志、错误日志、限流和上游健康检查。

### 8. 验证集群

逐台验证节点状态：

```bash
curl -fsS https://your-domain.com/api/status

for host in 10.0.0.11 10.0.0.12 10.0.0.13; do
  curl -fsS "http://${host}:3000/api/status"
done

docker compose -f docker-compose.cluster.yml ps
docker compose -f docker-compose.cluster.yml logs --tail=200 new-api
```

至少验证登录、管理后台、渠道测试、充值订单、支付回调、额度到账、订阅、返佣、提现审核和日志查询。

### 9. 集群更新

集群更新不要同时重启所有节点。普通代码更新可以滚动更新从节点，确认稳定后再更新主节点；如果本次更新可能包含数据库结构变更，建议安排维护窗口，先启动主节点完成迁移，再更新从节点。

1. 备份数据库，并记录当前提交。

   ```bash
   git rev-parse HEAD
   mkdir -p backups
   PGPASSWORD="<db-password>" pg_dump -h db.example.internal -U newapi -d new-api > "backups/newapi-$(date +%F-%H%M%S).sql"
   ```

   如果运维机没有安装 `pg_dump`，也可以在能访问数据库的机器上临时使用 `postgres:15` 镜像执行导出。

2. 判断更新类型。

   - 普通更新：先从负载均衡摘除一个从节点，逐台滚动更新。
   - 结构更新：短暂进入维护窗口，先更新主节点并确认迁移完成，再逐台更新从节点。

3. 在待更新节点更新源码并重建。

   ```bash
   cd /path/to/newapi
   git status --short
   git fetch --all --prune
   git pull --ff-only
   docker compose -f docker-compose.cluster.yml config
   docker compose -f docker-compose.cluster.yml up -d --build
   docker compose -f docker-compose.cluster.yml ps
   curl -fsS http://127.0.0.1:${APP_PORT:-3000}/api/status
   docker compose -f docker-compose.cluster.yml logs --tail=100 new-api
   ```

4. 验证该节点登录、接口、支付页和基础 API 正常后，再加回负载均衡。

5. 按相同方式逐台更新其他节点。

6. 如果是普通滚动更新，最后在低峰期更新主节点；如果是结构更新，主节点应已在维护窗口内优先完成迁移。

7. 全部节点更新后验证：

   ```bash
   curl -fsS https://your-domain.com/api/status
   docker compose -f docker-compose.cluster.yml ps
   docker compose -f docker-compose.cluster.yml logs --tail=200
   ```

8. 验证管理后台登录、普通用户登录、渠道测试、充值订单、支付回调、额度到账、返佣、提现审核和日志查询。

### 集群部署检查清单

- [ ] 所有节点运行同一个仓库版本或同一个镜像 tag。
- [ ] 所有节点 `SQL_DSN` 指向同一数据库入口。
- [ ] 所有节点 `REDIS_CONN_STRING` 指向同一 Redis。
- [ ] 集群节点没有使用 SQLite 或本地独立数据库文件。
- [ ] 所有节点 `SESSION_SECRET` 完全一致。
- [ ] 所有节点 `CRYPTO_SECRET` 完全一致。
- [ ] 只有主节点是 `NODE_TYPE=master`，从节点是 `NODE_TYPE=slave`。
- [ ] 每个节点有唯一 `NODE_NAME`。
- [ ] 反向代理保留 `Host` 和 `X-Forwarded-*` 请求头。
- [ ] 生产环境 `REFERRAL_TEST_MODE=false`。
- [ ] 生产环境启用合理的全局限流和关键接口限流。
- [ ] PostgreSQL / MySQL / Redis 不对公网开放。
- [ ] 支付回调域名、`TRUSTED_REDIRECT_DOMAINS`、支付网关商户号和密钥已按生产环境配置。
- [ ] 更新前已备份数据库，更新后已验证业务链路。

## 手动 Docker 运行

如果不使用 Compose，也可以手动构建镜像：

```bash
git clone https://github.com/biubiubiu125/newapi.git
cd newapi
docker build -t biubiubiu125/newapi:local .
```

SQLite 单容器示例：

```bash
mkdir -p data logs

docker run --name newapi -d --restart always \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -e SESSION_SECRET="$(openssl rand -hex 32)" \
  -v "$(pwd)/data:/data" \
  -v "$(pwd)/logs:/app/logs" \
  biubiubiu125/newapi:local --log-dir /app/logs
```

外部 PostgreSQL 和 Redis 示例：

```bash
docker run --name newapi -d --restart always \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -e SQL_DSN="postgresql://user:password@postgres-host:5432/new-api" \
  -e REDIS_CONN_STRING="redis://:redis-password@redis-host:6379" \
  -e SESSION_SECRET="your-random-session-secret" \
  -e TRUSTED_REDIRECT_DOMAINS="example.com" \
  -v "$(pwd)/data:/data" \
  -v "$(pwd)/logs:/app/logs" \
  biubiubiu125/newapi:local --log-dir /app/logs
```

## 反向代理建议

生产环境建议使用 Nginx、Caddy、Traefik 或云厂商负载均衡提供 HTTPS。

Nginx 示例：

```nginx
server {
    listen 80;
    server_name example.com;

    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

使用 HTTPS 后，请在后台支付网关配置里填写正确的回调地址，并在 `.env` 或后台配置里设置可信跳转域名。

## 初始化与后台配置

首次部署后：

1. 打开站点首页。
2. 按页面提示完成初始化或创建管理员账号。
3. 登录后台。
4. 配置站点名称、主题、侧边栏模块、用户注册策略。
5. 配置渠道、模型、分组和价格。
6. 在 `系统设置 -> 计费设置 -> 支付网关` 确认支付合规声明。
7. 配置充值金额、支付方式、epay、BEpusdt USDT、Stripe、Creem、Waffo、Waffo Pancake 等支付网关。
8. 如果启用订阅，先创建订阅套餐，再开放用户购买。
9. 如果启用推广返佣，先配置返佣规则，再审批推广员。

## 支付网关配置

### epay

后台路径：

```text
系统设置 -> 计费设置 -> 支付网关
```

需要配置：

- epay 地址
- 商户号
- 商户密钥
- 支付方式列表，例如支付宝、微信
- 价格换算
- 最低充值
- 自定义回调地址

回调入口：

- 充值：`/api/user/epay/notify`
- 订阅：`/api/subscription/epay/notify`
- 订阅 return：`/api/subscription/epay/return`

`return` 只用于页面跳转展示，不作为到账依据。

### BEpusdt / USDT

本 fork 的 USDT 网关只保留 BEpusdt 链路。后端配置键、文件名、路由、日志和前端文案均统一使用 BEpusdt 命名，不再保留旧 USDT 网关入口。

后台路径：

```text
系统设置 -> 计费设置 -> 支付网关 -> USDT 网关
```

需要配置：

- 启用 USDT 网关
- BEpusdt 地址，例如 `https://upay.example.com`
- BEpusdt 密钥
- 订单计价币种，当前固定为 `CNY`
- 显示名称，建议为 `USDT`
- 最低充值

付款方式建议只配置一个：

```json
[
  {
    "name": "USDT",
    "type": "usdt",
    "color": "#1890FF"
  }
]
```

用户在 newapi 钱包里只看到一个 USDT 按钮。点击后拉起 BEpusdt 支付页，链、币种、汇率和收款地址由 BEpusdt 支付页处理。

回调入口：

- 充值：`/api/user/bepusdt/notify`
- 订阅：`/api/subscription/bepusdt/notify`

注意：

- BEpusdt 网关必须能访问 newapi 的 notify URL。
- newapi 必须能访问 BEpusdt API 地址。
- BEpusdt 密钥不得写入前端、Git 或公开日志。
- newapi 只根据 BEpusdt 的签名合法异步 notify 入账，`return_url` 只负责页面跳转和刷新。
- 支付回调会校验签名、订单号、支付网关、支付方式、金额、币种和订单状态；跨网关回调、金额不一致、币种不一致和重复回调不会重复到账。
- 如果 BEpusdt 返回汇率或下单错误，优先检查 BEpusdt 后台 token、链、汇率、资产和收款地址配置。

### Stripe / Creem / Waffo / Waffo Pancake

这些网关在后台同一支付网关页面配置。请按对应平台提供的 API key、webhook secret、商品或价格 ID、回调地址进行配置。

生产环境必须使用真实 webhook secret 验签。测试模式、沙箱和 mock 不能当作真实扣款成功。

Waffo Pancake 额外支持后台绑定目录/产品：

- 后台可拉取 Pancake catalog，并绑定一个用于钱包充值的 store/product。
- 钱包充值使用绑定的 Pancake product，并按每笔订单覆盖本次 checkout 价格。
- 订阅套餐不复用钱包充值 product；每个订阅套餐应绑定或创建自己的 Pancake subscription product。
- 用户充值入口：`/api/user/waffo-pancake/pay`。
- 用户订阅入口：`/api/subscription/waffo-pancake/pay`。
- Webhook 入口：`/api/waffo-pancake/webhook/test` 和 `/api/waffo-pancake/webhook/prod`，应分别配置到 Pancake 后台对应环境。

## 订阅套餐配置

后台路径：

```text
订阅管理
```

可配置：

- 套餐名称
- 描述
- 价格
- 有效期
- 总额度
- 额度是否重置
- 优先级
- 启用状态

用户在钱包页的订阅套餐区域购买。支付成功后，后端按订单快照开通订阅权益。

订阅套餐展示货币应与系统货币展示设置一致。当前 fork 已重点适配新默认模板，建议后续运营也以新默认模板为准。

如果订阅套餐使用 Waffo Pancake 支付，需要在套餐管理中维护该套餐对应的 Pancake product ID，或通过后台提供的创建入口生成专属 product。

## 推广返佣配置

用户侧入口：

```text
推广中心
```

管理员入口：

```text
后台 -> 推广返佣相关页面
```

核心流程：

1. 用户申请推广员。
2. 管理员审批推广员。
3. 推广员获取邀请码和邀请链接。
4. 被邀请人通过邀请链接访问站点。
5. 注册后绑定邀请关系。
6. 被邀请人完成有效支付订单。
7. 后端生成佣金。
8. 佣金进入待结算。
9. 结算后变为可提现。
10. 推广员申请提现。
11. 管理员审核。
12. 管理员打款。
13. 系统记录提现和返佣流水。

返佣以订单快照为准。后台后续修改汇率、额度比例或返佣比例，不应影响已经创建或已经支付的历史订单。

## 开发环境

### 后端本地运行

使用 SQLite 快速运行：

```bash
go run .
```

默认监听端口由环境变量或程序默认值决定，通常为 `3000`。

使用 PostgreSQL / Redis 时，设置：

```bash
export SQL_DSN="postgresql://user:password@127.0.0.1:5432/new-api"
export REDIS_CONN_STRING="redis://:password@127.0.0.1:6379"
export SESSION_SECRET="your-random-session-secret"
go run .
```

### 默认前端开发

```bash
cd web/default
bun install
bun run dev
```

如果没有 Bun，可按项目脚本使用 npm，但不要混用锁文件：

```bash
npm install
npm run dev
```

### classic 前端开发

```bash
cd web/classic
bun install
bun run dev
```

或：

```bash
npm install --legacy-peer-deps
npm run dev
```

### 开发 Compose

```bash
docker compose -f docker-compose.dev.yml up -d --build
```

停止：

```bash
docker compose -f docker-compose.dev.yml down
```

重置开发数据：

```bash
docker compose -f docker-compose.dev.yml down -v
```

## 构建与测试

后端：

```bash
go test ./...
go vet ./...
```

默认前端：

```bash
cd web/default
bun install
bun run typecheck
bun run build
```

classic 前端：

```bash
cd web/classic
bun install
bun run build
```

Docker：

```bash
docker compose config
docker compose build
docker compose up -d
docker compose ps
```

## 生产安全检查

上线前至少确认：

- `.env` 没有使用示例密码。
- `SESSION_SECRET` 是随机强密钥。
- PostgreSQL 和 Redis 未直接暴露公网。
- 反向代理已启用 HTTPS。
- `TRUSTED_REDIRECT_DOMAINS` 只包含可信域名。
- `CORS_ALLOWED_ORIGINS` 没有放开到不可信来源。
- 支付密钥没有进入 Git、前端 bundle 或公开日志。
- 支付回调可以被公网网关访问。
- 充值审计和风控中心可以正常展示支付、订单、用户、IP、Token 和推广相关信号。
- `docker compose logs` 没有持续错误。
- 管理员账号已修改默认密码并启用必要安全措施。
- 已完成真实支付或沙箱/签名合法回调测试，并明确区分测试回调和真实扣款。

## 常见问题

### 钱包里没有 USDT 支付方式

检查：

1. 后台是否启用 USDT 网关。
2. BEpusdt 地址和密钥是否完整。
3. 支付方式列表是否包含 `USDT`，类型为 `usdt`。
4. 用户钱包页是否刷新了 `/api/user/topup/info`。

建议只配置一个 `USDT` 支付按钮，由 BEpusdt 支付页处理链、币种、汇率和收款地址。

### BEpusdt 拉起失败

优先检查：

1. newapi 后端日志里的 BEpusdt 错误。
2. BEpusdt 网关是否能访问。
3. BEpusdt 密钥是否匹配。
4. BEpusdt 的 token、链、汇率、资产和收款地址是否配置完整。
5. newapi 订单计价币种是否为 `CNY`。
6. 回调地址是否为公网可访问 HTTPS 地址。

### 支付成功但额度未到账

不要只看前端跳转页。应检查：

1. 支付网关是否发送 notify/webhook。
2. notify 是否验签通过。
3. 订单号、金额、币种、网关、订单类型是否匹配。
4. 订单状态是否从 pending 变为 success。
5. 用户 quota、订单记录、日志和返佣记录是否一致。

### 订阅套餐显示货币不一致

检查系统设置中的货币展示：

```text
系统设置 -> 计费设置 -> Currency & Display
```

确认 `quota_display_type`、自定义货币符号、汇率和订阅套餐价格展示口径一致。

## 许可证

本项目继承上游许可证。请阅读：

- `LICENSE`
- `NOTICE`
- `THIRD-PARTY-LICENSES.md`

## 说明

本 fork 保留上游项目名称、版权、许可证和归属信息。二次维护内容以当前仓库实际代码为准。
