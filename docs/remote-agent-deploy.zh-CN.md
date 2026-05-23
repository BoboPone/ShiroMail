# ShiroMail 远程本机发布流程

本文用于给远程机器上的 agent 执行本机发布。假设代码已经上传到远程机的 `/opt/shiromail/source`，agent 不需要再通过 SSH 操作其他机器。

## 部署模式

ShiroMail 使用 Podman 容器化部署：

- 源码目录：`/opt/shiromail/source`
- 镜像：`ghcr.io/galiais/shiromail:latest`
- Compose 文件：`/opt/shiromail/source/docker-compose.yml`
- 服务：
  - `mysql`：MySQL 数据库
  - `redis`：Redis
  - `app`：Web/API/SMTP 入口
  - `worker`：后台任务
- 持久化卷：
  - `mysql_data`
  - `redis_data`
  - `mail_data`

发布时默认只重建 `app` 和 `worker`，不要重建或删除数据库、Redis、数据卷。

## 发布前检查

在远程机本机执行：

```bash
cd /opt/shiromail/source

pwd
test -f docker-compose.yml
test -f .env

podman --version
podman compose version || podman-compose version

podman ps -a --format '{{.Names}} {{.Status}} {{.Image}}'
```

确认点：

- 当前目录必须是 `/opt/shiromail/source`
- `.env` 必须存在，且不要打印其中的密钥值
- `mysql`、`redis` 正常运行
- 发布窗口内允许短暂重启 `app` 和 `worker`

## 标准发布流程

```bash
set -euo pipefail

cd /opt/shiromail/source

ts=$(date +%Y%m%d%H%M%S)
backup_dir="/opt/shiromail/backups/source-before-${ts}"
rollback_tag="localhost/shiromail:rollback-${ts}"

echo "[1/7] 备份当前源码"
mkdir -p /opt/shiromail/backups
cp -a /opt/shiromail/source "${backup_dir}"

echo "[2/7] 标记当前镜像为回滚点"
podman image exists ghcr.io/galiais/shiromail:latest && \
  podman image tag ghcr.io/galiais/shiromail:latest "${rollback_tag}" || true

echo "[3/7] 构建新镜像"
podman build -t ghcr.io/galiais/shiromail:latest .

echo "[4/7] 重建 app 和 worker"
if podman compose version >/dev/null 2>&1; then
  podman compose up -d --force-recreate app worker
else
  podman-compose up -d --force-recreate app worker
fi

echo "[5/7] 检查容器状态"
podman ps -a --format '{{.Names}} {{.Status}} {{.Image}}' | grep -E 'shiromail|mysql|redis|app|worker'

echo "[6/7] 检查 API"
curl -fsS --max-time 10 http://127.0.0.1:5173/api/v1/site/stats

echo "[7/7] 查看最近日志"
podman logs --tail 80 shiromail-app || podman logs --tail 80 source-app-1 || true
podman logs --tail 80 shiromail-worker || podman logs --tail 80 source-worker-1 || true

echo "发布完成"
echo "源码备份：${backup_dir}"
echo "回滚镜像：${rollback_tag}"
```

## Compose 不可用时的兜底发布

只有在 `podman compose` 和 `podman-compose` 都不可用，或 Compose 在当前 shell 下异常时，才使用本节。

先确认现有网络名和卷名：

```bash
podman network ls
podman volume ls | grep -E 'mysql_data|redis_data|mail_data'
```

然后手动重建 `app` 和 `worker`：

```bash
set -euo pipefail

cd /opt/shiromail/source

podman rm -f shiromail-app shiromail-worker || true

podman run -d --name shiromail-app --restart unless-stopped \
  --network shiromail-net --network-alias app \
  --cap-add NET_BIND_SERVICE \
  --env-file /opt/shiromail/source/.env \
  -p 5173:80 -p 25:2525 \
  -v shiromail_mail_data:/app/data/mail \
  ghcr.io/galiais/shiromail:latest

podman run -d --name shiromail-worker --restart unless-stopped \
  --network shiromail-net --network-alias worker \
  --env-file /opt/shiromail/source/.env \
  -v shiromail_mail_data:/app/data/mail \
  ghcr.io/galiais/shiromail:latest shiro-worker
```

如果当前机器的网络或卷名不是 `shiromail-net`、`shiromail_mail_data`，必须以 `podman network ls` 和 `podman volume ls` 的实际结果为准。不要创建新的数据库卷替代旧卷。

## 发布后验证

至少执行：

```bash
podman ps -a --format '{{.Names}} {{.Status}} {{.Image}}'
curl -fsS --max-time 10 http://127.0.0.1:5173/api/v1/site/stats
podman logs --tail 100 shiromail-app || true
podman logs --tail 100 shiromail-worker || true
```

功能验证：

- 管理后台可以登录
- 管理员可以打开目标邮箱
- 已存在但过期或删除的邮箱可以恢复使用
- 历史邮件可以在恢复后的邮箱中看到
- 新邮件仍能正常接收

如果 API 返回 HTML 而不是 JSON，优先检查请求路径是否包含 `/api/v1/`，再检查 app 日志。

## 回滚流程

如果发布后验证失败，使用发布前输出的 `rollback_tag` 回滚：

```bash
set -euo pipefail

cd /opt/shiromail/source

rollback_tag="localhost/shiromail:rollback-替换为实际时间戳"

podman image tag "${rollback_tag}" ghcr.io/galiais/shiromail:latest

if podman compose version >/dev/null 2>&1; then
  podman compose up -d --force-recreate app worker
else
  podman-compose up -d --force-recreate app worker
fi

curl -fsS --max-time 10 http://127.0.0.1:5173/api/v1/site/stats
```

如果 Compose 仍不可用，按“Compose 不可用时的兜底发布”重建 `app` 和 `worker`，镜像仍使用 `ghcr.io/galiais/shiromail:latest`。

## 清理中间产物

清理只能在发布和验证成功后执行。不要删除 `mysql_data`、`redis_data`、`mail_data`，不要使用带 `--volumes` 的 prune。

推荐保留最近 3 个源码备份和最近 3 个回滚镜像：

```bash
set -euo pipefail

echo "[cleanup] 清理 /tmp 中的 ShiroMail 临时包"
find /tmp -maxdepth 1 -type f -name 'shiromail-deploy-*.tar.gz' -mtime +1 -print -delete

echo "[cleanup] 保留最近 3 个源码备份"
find /opt/shiromail/backups -maxdepth 1 -type d -name 'source-before-*' \
  | sort \
  | head -n -3 \
  | xargs -r rm -rf

echo "[cleanup] 保留最近 3 个回滚镜像 tag"
podman images --format '{{.Repository}}:{{.Tag}}' \
  | grep '^localhost/shiromail:rollback-' \
  | sort \
  | head -n -3 \
  | xargs -r podman rmi

echo "[cleanup] 清理 dangling 镜像和构建缓存"
podman image prune -f
podman builder prune -f || true

echo "[cleanup] 查看磁盘占用"
df -h
podman system df || true
du -sh /opt/shiromail/backups || true
```

注意：

- `rm -rf` 只允许作用在 `/opt/shiromail/backups/source-before-*`
- 不要执行 `podman volume prune`
- 不要执行 `podman system prune --volumes`
- 不要删除 `/opt/shiromail/source/.env`
- 不要打印 `.env` 中的 `JWT_SECRET`、数据库密码或其他密钥

## 常见问题

### app 无法连接 MySQL

检查 `app` 和 `worker` 是否在正确网络内：

```bash
podman inspect shiromail-app --format '{{json .NetworkSettings.Networks}}' || true
podman inspect shiromail-worker --format '{{json .NetworkSettings.Networks}}' || true
```

如果手动运行容器，必须使用和 MySQL/Redis 相同的网络。

### app 日志提示不能绑定 80 端口

手动 `podman run` 时需要给 app 加：

```bash
--cap-add NET_BIND_SERVICE
```

Compose 正常情况下不需要手动处理。

### 登录态失效或接口 401

检查发布前后 `JWT_SECRET` 是否变化。只能检查长度或 hash，不要直接打印密钥：

```bash
awk -F= '$1=="JWT_SECRET"{print "JWT_SECRET length=" length($2)}' /opt/shiromail/source/.env
```

如果 `JWT_SECRET` 被误改，恢复原 `.env` 后重建 `app` 和 `worker`。

### 需要确认镜像版本

```bash
podman images ghcr.io/galiais/shiromail:latest
podman inspect ghcr.io/galiais/shiromail:latest --format '{{.Id}} {{.Created}}'
```

记录镜像 ID，便于排查发布是否真的生效。
