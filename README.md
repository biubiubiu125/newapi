# newapi

`newapi` 是基于上游 `new-api` 二次维护的中文默认分支，定位仍然是多模型网关与 AI 资产管理系统。

当前仓库默认使用中文说明，部署方式也按当前 fork 仓库调整为：
- 从 `https://github.com/biubiubiu125/newapi.git` 拉源码
- 使用当前仓库本地构建镜像
- 再用 `docker compose` 启动

## 功能概览

- 多模型网关，兼容常见 OpenAI 风格接口
- 用户、令牌、分组、额度、账单与订阅管理
- 默认前端与 classic 前端双界面
- 推广返佣链路、提现审核与后台管理
- 支持 Docker / Docker Compose 私有化部署

## 仓库地址

当前 fork 仓库地址：

```bash
https://github.com/biubiubiu125/newapi.git
```

如果你是从这个 fork 部署，请不要再按上游仓库地址拉取，也不要直接依赖上游预构建镜像。

## 快速部署

### 方式一：Docker Compose

推荐直接使用仓库根目录自带的 `docker-compose.yml`。

```bash
git clone https://github.com/biubiubiu125/newapi.git
cd newapi

# 按需修改数据库、Redis、时区和密码
nano docker-compose.yml

# 首次启动会基于当前仓库源码构建镜像
docker compose up -d --build
```

启动完成后，默认访问：

```bash
http://localhost:3000
```

### 方式二：手动构建镜像

如果你不想使用 Compose，也可以直接本地构建：

```bash
git clone https://github.com/biubiubiu125/newapi.git
cd newapi

docker build -t biubiubiu125/newapi:local .
```

使用 SQLite 启动：

```bash
docker run --name newapi -d --restart always \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  biubiubiu125/newapi:local
```

使用外部 MySQL / PostgreSQL / Redis 时，请自行补齐对应环境变量。

## 默认 Compose 说明

根目录 `docker-compose.yml` 当前默认特性：

- `new-api` 服务从当前仓库源码本地构建
- 默认带 `PostgreSQL + Redis`
- 数据目录挂载到本地 `./data`
- 日志目录挂载到本地 `./logs`

生产部署前请至少修改这些默认值：

- PostgreSQL 密码
- Redis 密码
- `SESSION_SECRET`
- 对外域名、反代和 HTTPS 配置

## 开发与构建

### 后端测试

```bash
go test ./...
```

### 默认前端

```bash
cd web/default
npm install
npm run build
```

### classic 前端

```bash
cd web/classic
npm install --legacy-peer-deps
npm run build
```

## 文档说明

这个 fork 的 README 现在以中文为准。

上游官方文档仍可作为功能背景参考，但如果文档内容与当前 fork 实现不一致，请以当前仓库代码、`docker-compose.yml`、前端页面和实际行为为准。

上游文档入口：

```text
https://docs.newapi.pro/zh/docs
```

## 上游关系

本仓库基于上游 `new-api` fork 维护。

如果需要追查历史设计或上游实现，可参考：

```text
https://github.com/QuantumNous/new-api
```

但当前部署、README、推广返佣链路以及前端结构，请以本 fork 为准。
