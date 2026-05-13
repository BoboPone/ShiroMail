# ShiroMail 部署指南

本文档详细说明如何在 VPS 上从零部署 ShiroMail，包括系统准备、端口配置、DNS 设置、Docker Compose 启动、HTTPS 证书、以及后续运维。

---

## 目录

- [环境要求](#环境要求)
- [端口规划](#端口规划)
- [VPS 防火墙配置](#vps-防火墙配置)
- [安装 Docker](#安装-docker)
- [获取项目](#获取项目)
- [配置环境变量](#配置环境变量)
- [DNS 配置](#dns-配置)
- [启动服务](#启动服务)
- [验证部署](#验证部署)
- [管理后台初始化](#管理后台初始化)
- [SMTP 收信配置](#smtp-收信配置)
- [自定义域名接入](#自定义域名接入)
- [备份与恢复](#备份与恢复)
- [升级](#升级)
- [常见问题](#常见问题)

---

## 环境要求

| 项目 | 最低配置 | 推荐配置 |
|------|---------|---------|
| CPU | 1 核 | 2 核 |
| 内存 | 2 GB | 4 GB |
| 磁盘 | 20 GB SSD | 40 GB SSD |
| 系统 | Ubuntu 22.04 / Debian 12 / CentOS Stream 9 | Ubuntu 24.04 LTS |
| Docker | 24.0+ | 最新稳定版 |
| Docker Compose | v2.20+ | 最新稳定版 |

公网 IP 必须能被外部直接访问（非 NAT 后端），否则 SMTP 收信和 Let's Encrypt 证书签发会失败。

---

## 端口规划

ShiroMail 需要以下端口对外开放：

| 端口 | 协议 | 用途 | 说明 |
|------|------|------|------|
| 80 | TCP | HTTP | Traefik 入口，自动重定向到 HTTPS |
| 443 | TCP | HTTPS | Web UI + API 访问 |
| 25 | TCP | SMTP | 接收外部邮件（MX 记录指向此端口） |

内部端口（不需要对外暴露）：

| 端口 | 服务 | 说明 |
|------|------|------|
| 8080 | Go API | 容器内 nginx 反代到此端口 |
| 2525 | SMTP listener | 容器内 SMTP 监听，宿主机 25 映射到此 |
| 3306 | MySQL | 仅 compose 内部网络 |
| 6379 | Redis | 仅 compose 内部网络 |

---

## VPS 防火墙配置

### Ubuntu / Debian (ufw)

```bash
# 允许 SSH（防止锁死自己）
sudo ufw allow 22/tcp

# 允许 HTTP/HTTPS
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# 允许 SMTP 收信
sudo ufw allow 25/tcp

# 启用防火墙
sudo ufw enable
sudo ufw status
```

### CentOS / RHEL (firewalld)

```bash
sudo firewall-cmd --permanent --add-service=ssh
sudo firewall-cmd --permanent --add-service=http
sudo firewall-cmd --permanent --add-service=https
sudo firewall-cmd --permanent --add-service=smtp
sudo firewall-cmd --reload
sudo firewall-cmd --list-all
```

### 云厂商安全组

如果使用 AWS、GCP、阿里云、腾讯云等，还需要在云控制台的安全组/防火墙规则中放行以上端口。

> 注意：部分云厂商默认封锁 25 端口以防止垃圾邮件。如遇此情况，需要向云厂商提交工单申请解封，或使用替代端口（如 587）并在 DNS 中配置对应 MX 记录。

---

## 安装 Docker

```bash
# 一键安装 Docker（官方脚本）
curl -fsSL https://get.docker.com | sh

# 将当前用户加入 docker 组（免 sudo）
sudo usermod -aG docker $USER
newgrp docker

# 验证
docker --version
docker compose version
```

---

## 获取项目

```bash
# 克隆仓库
git clone https://github.com/GALIAIS/ShiroMail.git
cd ShiroMail

# 或者只需要部署文件（不需要源码）
mkdir shiromail && cd shiromail
curl -LO https://raw.githubusercontent.com/GALIAIS/ShiroMail/main/docker-compose.prod.yml
curl -LO https://raw.githubusercontent.com/GALIAIS/ShiroMail/main/.env.example
```

---

## 配置环境变量

```bash
cp .env.example .env
```

编辑 `.env` 文件，修改以下关键配置：

```bash
# ─── 必须修改 ───────────────────────────────────────────────

# 你的域名（用于 Traefik 证书签发和 CORS）
SITE_DOMAIN=mail.example.com
CORS_ALLOWED_ORIGINS=https://mail.example.com

# 数据库密码（使用强密码）
MYSQL_ROOT_PASSWORD=your-strong-mysql-password-here
MYSQL_DSN=root:your-strong-mysql-password-here@tcp(mysql:3306)/shiro_email?parseTime=true

# Redis 密码
REDIS_PASSWORD=your-strong-redis-password-here

# JWT 密钥（至少 32 字符，用 openssl rand -base64 48 生成）
JWT_SECRET=your-jwt-secret-at-least-32-characters

# Metrics 端点 Bearer Token
METRICS_TOKEN=your-metrics-token-here

# Let's Encrypt 证书通知邮箱
SSL_EMAIL=admin@example.com

# ─── 可选修改 ───────────────────────────────────────────────

# SMTP 宿主机端口（默认 25）
SMTP_PORT=25

# 使用的镜像（默认拉取最新）
SHIROMAIL_IMAGE=ghcr.io/galiais/shiromail:latest
```

生成安全密钥的快捷方式：

```bash
# 生成 JWT_SECRET
openssl rand -base64 48

# 生成数据库密码
openssl rand -base64 24

# 生成 Redis 密码
openssl rand -base64 24
```

---

## DNS 配置

在你的域名 DNS 管理面板中添加以下记录：

### 基础记录

| 类型 | 名称 | 值 | 说明 |
|------|------|---|------|
| A | mail.example.com | `<VPS公网IP>` | Web UI 访问地址 |
| MX | example.com | `mail.example.com` (优先级 10) | 邮件接收入口 |

### 邮件认证记录（提高送达率和可信度）

| 类型 | 名称 | 值 | 说明 |
|------|------|---|------|
| TXT | example.com | `v=spf1 mx -all` | SPF 记录 |
| TXT | _dmarc.example.com | `v=DMARC1; p=reject; rua=mailto:dmarc@example.com` | DMARC 策略 |

> 如果使用子域名收信（如 `@sub.example.com`），MX 记录应设置在 `sub.example.com` 上。

### 验证 DNS 生效

```bash
# 检查 A 记录
dig +short mail.example.com A

# 检查 MX 记录
dig +short example.com MX

# 检查 SPF
dig +short example.com TXT
```

---

## 启动服务

### 生产环境（带 Traefik + HTTPS）

```bash
docker compose -f docker-compose.prod.yml up -d
```

### 开发/测试环境（无 HTTPS）

```bash
docker compose up -d
```

### 查看启动日志

```bash
# 查看所有服务日志
docker compose -f docker-compose.prod.yml logs -f

# 只看应用日志
docker compose -f docker-compose.prod.yml logs -f app

# 只看 worker 日志
docker compose -f docker-compose.prod.yml logs -f worker
```

### 等待服务就绪

首次启动时 MySQL 需要初始化，通常需要 30-60 秒。可以通过健康检查确认：

```bash
# 检查所有容器状态
docker compose -f docker-compose.prod.yml ps

# 期望输出：所有服务 STATUS 为 Up (healthy)
```

---

## 验证部署

### 1. 检查 Web 访问

浏览器打开 `https://mail.example.com`，应看到 ShiroMail 登录页面。

### 2. 检查 API 健康

```bash
curl -s https://mail.example.com/healthz | jq .
```

期望返回：

```json
{
  "status": "ok",
  "checks": {
    "redis": "ok"
  }
}
```

### 3. 检查 SMTP 端口

```bash
# 从外部测试 SMTP 连接
telnet mail.example.com 25
```

期望看到 SMTP banner 响应（如 `220 mail.example.com ESMTP`）。

### 4. 检查证书

```bash
curl -vI https://mail.example.com 2>&1 | grep -i "subject\|issuer\|expire"
```

---

## 管理后台初始化

首次部署后，需要通过注册创建第一个管理员账号：

1. 访问 `https://mail.example.com`
2. 点击「创建账号」注册第一个用户
3. 第一个注册的用户默认获得 `admin` 角色（可在系统设置中调整）
4. 登录后进入管理后台，完成以下初始化：
   - 添加收信域名
   - 配置 SMTP 监听参数
   - 设置站点名称和公告

> 如果注册模式为 `invite_only`，第一个用户需要通过数据库直接创建。参见 [常见问题](#常见问题) 部分。

---

## SMTP 收信配置

ShiroMail 内置 SMTP 服务器，收信配置通过管理后台的系统设置进行：

1. 登录管理后台
2. 进入「系统设置」>「邮件配置」
3. 配置 `mail.smtp` 相关项：

| 配置项 | 说明 | 推荐值 |
|--------|------|--------|
| enabled | 是否启用 SMTP 监听 | `true` |
| listen | 监听地址 | `0.0.0.0:2525`（容器内） |
| hostname | SMTP EHLO 主机名 | `mail.example.com` |
| maxMessageSize | 最大邮件大小 | `10485760`（10MB） |

> 宿主机端口 25 通过 docker-compose 映射到容器内 2525，无需在此处配置宿主机端口。

### 测试收信

```bash
# 使用 swaks 发送测试邮件
swaks --to test@example.com --from sender@gmail.com --server mail.example.com:25

# 或使用 telnet 手动测试
telnet mail.example.com 25
EHLO test.local
MAIL FROM:<sender@test.local>
RCPT TO:<test@example.com>
DATA
Subject: Test
Hello from SMTP test
.
QUIT
```

---

## 自定义域名接入

用户可以在管理后台添加自定义域名用于收信：

1. 管理后台 > 域名管理 > 添加域名
2. 系统会生成需要配置的 DNS 记录（MX、SPF、DKIM 等）
3. 用户在自己的 DNS 面板添加对应记录
4. 回到管理后台点击「验证」确认 DNS 生效
5. 验证通过后域名变为 `active` 状态，可以开始收信

---

## 备份与恢复

### 备份

```bash
#!/bin/bash
BACKUP_DIR="/opt/shiromail-backups/$(date +%Y%m%d_%H%M%S)"
mkdir -p "$BACKUP_DIR"

# 备份 MySQL
docker compose -f docker-compose.prod.yml exec -T mysql \
  mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" shiro_email \
  > "$BACKUP_DIR/shiro_email.sql"

# 备份邮件存储
docker compose -f docker-compose.prod.yml cp app:/app/data/mail "$BACKUP_DIR/mail"

# 备份环境配置
cp .env "$BACKUP_DIR/.env"

echo "Backup completed: $BACKUP_DIR"
```

### 恢复

```bash
# 恢复 MySQL
docker compose -f docker-compose.prod.yml exec -T mysql \
  mysql -uroot -p"$MYSQL_ROOT_PASSWORD" shiro_email \
  < /path/to/backup/shiro_email.sql

# 恢复邮件存储
docker compose -f docker-compose.prod.yml cp /path/to/backup/mail app:/app/data/
```

### 定时备份（crontab）

```bash
# 每天凌晨 3 点自动备份
0 3 * * * /opt/shiromail/backup.sh >> /var/log/shiromail-backup.log 2>&1
```

---

## 升级

```bash
cd /path/to/shiromail

# 拉取最新镜像
docker compose -f docker-compose.prod.yml pull

# 重启服务（零停机滚动更新）
docker compose -f docker-compose.prod.yml up -d

# 清理旧镜像
docker image prune -f
```

如需升级到指定版本：

```bash
# 修改 .env 中的镜像标签
SHIROMAIL_IMAGE=ghcr.io/galiais/shiromail:v0.7.1

# 重新部署
docker compose -f docker-compose.prod.yml up -d
```

---

## 常见问题

### 25 端口被云厂商封锁

部分云厂商（如阿里云、腾讯云）默认封锁出站/入站 25 端口。解决方案：

1. 提交工单申请解封 25 端口
2. 或使用替代端口（如 2525），但需要在 MX 记录中指定端口（部分邮件服务器不支持非标准端口）

### Let's Encrypt 证书签发失败

确认以下条件：

- 域名 A 记录已正确指向 VPS IP
- 80 端口对外可访问（Traefik 使用 HTTP-01 验证）
- 没有其他程序占用 80/443 端口

```bash
# 检查端口占用
sudo ss -tlnp | grep -E ':80|:443'

# 查看 Traefik 日志
docker compose -f docker-compose.prod.yml logs traefik
```

### 首次部署如何创建管理员（invite_only 模式）

如果系统配置为仅邀请注册，可以通过数据库直接创建：

```bash
# 进入 MySQL 容器
docker compose -f docker-compose.prod.yml exec mysql mysql -uroot -p shiro_email

# 查看已有用户
SELECT id, username, email FROM users;
```

或者临时将注册模式改为开放，注册后再改回：

```bash
# 在管理后台系统设置中修改 auth.registrationMode 为 "open"
# 注册完成后改回 "invite_only"
```

### 邮件收不到

排查步骤：

1. 确认 MX 记录正确：`dig +short example.com MX`
2. 确认 25 端口可达：`telnet mail.example.com 25`
3. 确认 SMTP 已启用：管理后台 > 系统设置 > 邮件配置
4. 查看 worker 日志：`docker compose -f docker-compose.prod.yml logs -f worker`
5. 查看管理后台的 Inbound Spool 队列是否有失败记录

### 内存不足 / OOM

调整 `docker-compose.prod.yml` 中的资源限制：

```yaml
deploy:
  resources:
    limits:
      memory: 1024M  # 根据实际情况调整
```

MySQL 是内存大户，最低建议分配 512MB。

### 如何使用自定义 SSL 证书（不用 Let's Encrypt）

如果已有证书，可以替换 Traefik 为 nginx 反代，或在 Traefik 中配置静态证书：

```bash
# 将证书文件挂载到 Traefik 容器
volumes:
  - ./certs:/certs:ro

# 在 Traefik 配置中引用
- --providers.file.filename=/certs/dynamic.toml
```

---

## 架构概览

```text
                    Internet
                       |
            ┌──────────┴──────────┐
            │    VPS Firewall     │
            │  80 / 443 / 25     │
            └──────────┬──────────┘
                       |
         ┌─────────────┼─────────────┐
         │             │             │
    ┌────┴────┐   ┌────┴────┐  ┌────┴────┐
    │ Traefik │   │   App   │  │  Worker │
    │ :80/:443│   │  :2525  │  │ (async) │
    └────┬────┘   └────┬────┘  └────┬────┘
         │             │             │
         │    ┌────────┴────────┐    │
         │    │  nginx (:80)    │    │
         │    │  API (:8080)    │    │
         │    │  SMTP (:2525)   │    │
         │    └────────┬────────┘    │
         │             │             │
         └──────┬──────┴──────┬──────┘
                │             │
          ┌─────┴─────┐ ┌────┴────┐
          │   MySQL   │ │  Redis  │
          │   :3306   │ │  :6379  │
          └───────────┘ └─────────┘
```

- Traefik: 反向代理 + 自动 HTTPS 证书
- App 容器: nginx（静态前端 + API 反代）+ Go API + SMTP listener
- Worker 容器: 异步任务处理（邮件入库、webhook 分发等）
- MySQL: 持久化存储
- Redis: 缓存 + 会话 + 队列

---

## 监控

ShiroMail 暴露 Prometheus 格式的 metrics 端点：

```bash
curl -H "Authorization: Bearer $METRICS_TOKEN" https://mail.example.com/metrics
```

可接入 Grafana 进行可视化监控。

---

## 安全建议

1. 所有密码使用强随机值，不要使用默认值
2. 定期更新镜像以获取安全补丁
3. MySQL 和 Redis 不要暴露到公网
4. 启用 DMARC/SPF/DKIM 防止域名被仿冒
5. 定期备份数据库和邮件存储
6. 考虑启用 fail2ban 防止 SMTP 暴力攻击
