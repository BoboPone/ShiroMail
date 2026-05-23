# 远程 Agent 发布 ShiroMail 3310aed 指南

本文档给远程服务器上的 agent 使用。目标是在服务器 `121.40.156.176` 上，基于已经上传好的源码包 `/tmp/shiromail-3310aed.tar.gz` 发布 ShiroMail。

## 背景

本次待发布版本：

- 最新本地提交：`3310aed fix(build): 禁用 npm 安装审计`
- 功能提交：`b5b1df5 feat(mail): 支持收取并恢复未知邮箱邮件`
- 源码包：`/tmp/shiromail-3310aed.tar.gz`
- 源码包 SHA256：`AC45DADD9EDA2C32FCE7506CA6A29B449E29AA3CDF31BAAA548752FD7C5BA281`

远端现有运行拓扑：

- 源码目录：`/opt/shiromail/source`
- 环境文件：`/opt/shiromail/env`
- 运行镜像：`ghcr.io/galiais/shiromail:latest`
- Podman 网络：`shiromail-net`
- 邮件数据卷：`shiromail_mail_data`
- 运行容器：
  - `shiromail-mysql`
  - `shiromail-redis`
  - `shiromail-app`
  - `shiromail-worker`

注意：MySQL、Redis 和所有数据卷都必须保留。本次只替换源码、镜像、`shiromail-app` 和 `shiromail-worker`。

## 已知问题

不要直接运行之前生成的 `/tmp/shiromail-deploy-3310aed.sh`。

原因：远端 `podman build` 在 Dockerfile 的前端 `npm ci` 步骤中曾卡住或失败。单独使用 Node 容器在源码目录外构建 `frontend/dist` 是可行的，所以本指南采用以下策略：

1. 解压源码包到 staging 目录。
2. 用 Node 容器在 staging 中预构建 `frontend/dist`。
3. 生成一个临时 `Dockerfile.remote`，跳过 Docker build 内部的前端 `npm ci`。
4. 用 `Dockerfile.remote` 构建新镜像。
5. 构建成功后再切换源码目录和重建 `app`/`worker`。
6. 健康检查失败时回滚到旧镜像。

## 发布前检查

在远程服务器本机执行：

```bash
set -euo pipefail

test -f /tmp/shiromail-3310aed.tar.gz
test -f /opt/shiromail/env
test -d /opt/shiromail/source

sha256sum /tmp/shiromail-3310aed.tar.gz
podman ps -a --format '{{.Names}} {{.Status}} {{.Image}} {{.Ports}}' | grep shiromail
curl -fsS --max-time 10 http://127.0.0.1:5173/api/v1/site/stats
```

`sha256sum` 应输出：

```text
ac45dadd9eda2c32fce7506ca6a29b449e29aa3cdf31baaa548752fd7c5ba281  /tmp/shiromail-3310aed.tar.gz
```

不要打印 `/opt/shiromail/env` 内容，里面有数据库密码和 `JWT_SECRET`。

## 一键发布脚本

在远程服务器本机执行：

```bash
cat >/tmp/publish-shiromail-3310aed.sh <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

APP_DIR=/opt/shiromail
ARCHIVE=/tmp/shiromail-3310aed.tar.gz
EXPECTED_SHA=AC45DADD9EDA2C32FCE7506CA6A29B449E29AA3CDF31BAAA548752FD7C5BA281
COMMIT=3310aed
TS=$(date +%Y%m%d%H%M%S)
SRC_DIR="$APP_DIR/source"
STAGING="$APP_DIR/source.deploy-$COMMIT-$TS"
BACKUP="$APP_DIR/source.backup.before-$COMMIT.$TS"
IMAGE=ghcr.io/galiais/shiromail:latest
NEW_IMAGE="localhost/shiromail:$COMMIT-$TS"
ROLLBACK_IMAGE="localhost/shiromail:rollback-before-$COMMIT-$TS"
ENV_FILE="$APP_DIR/env"
NETWORK=shiromail-net
MAIL_VOL=shiromail_mail_data

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"
}

require_file() {
  if [ ! -f "$1" ]; then
    echo "missing file: $1" >&2
    exit 1
  fi
}

health_check() {
  i=0
  while [ "$i" -lt 60 ]; do
    if curl -fsS --max-time 5 http://127.0.0.1:5173/api/v1/site/stats >/tmp/shiromail-health.json 2>/tmp/shiromail-health.err; then
      cat /tmp/shiromail-health.json
      printf '\n'
      return 0
    fi
    i=$((i + 1))
    sleep 2
  done

  log "health check failed"
  cat /tmp/shiromail-health.err 2>/dev/null || true
  return 1
}

write_dependency_dropin() {
  unit="$1"
  dir="/etc/systemd/system/${unit}.d"
  mkdir -p "$dir"
  printf '[Unit]\nAfter=container-shiromail-mysql.service container-shiromail-redis.service\nRequires=container-shiromail-mysql.service container-shiromail-redis.service\n' > "$dir/dependencies.conf"
}

refresh_app_units() {
  if ! command -v systemctl >/dev/null 2>&1; then
    return 0
  fi

  log "refreshing app/worker systemd units"
  unit_tmp="$(mktemp -d)"
  (
    cd "$unit_tmp"
    podman generate systemd --files --name --restart-policy=always shiromail-app
    podman generate systemd --files --name --restart-policy=always shiromail-worker
    sed -i 's/WantedBy=default.target/WantedBy=multi-user.target/' container-shiromail-*.service
    install -m 0644 container-shiromail-*.service /etc/systemd/system/
  )
  rm -rf "$unit_tmp"

  write_dependency_dropin container-shiromail-app.service
  write_dependency_dropin container-shiromail-worker.service
  systemctl daemon-reload
  systemctl reset-failed container-shiromail-app.service container-shiromail-worker.service || true
  systemctl enable container-shiromail-app.service container-shiromail-worker.service >/dev/null
  systemctl start container-shiromail-app.service container-shiromail-worker.service || true
}

start_app_worker() {
  image="$1"

  podman rm -f shiromail-worker >/dev/null 2>&1 || true
  podman rm -f shiromail-app >/dev/null 2>&1 || true

  log "starting ShiroMail app from $image"
  podman run -d --name shiromail-app \
    --restart=unless-stopped \
    --network "$NETWORK" \
    --sysctl net.ipv4.ip_unprivileged_port_start=0 \
    --env-file "$ENV_FILE" \
    -p 5173:80 \
    -p 25:2525 \
    -v "$MAIL_VOL":/app/data/mail \
    "$image"

  log "starting ShiroMail worker from $image"
  podman run -d --name shiromail-worker \
    --restart=unless-stopped \
    --network "$NETWORK" \
    --env-file "$ENV_FILE" \
    -v "$MAIL_VOL":/app/data/mail \
    "$image" shiro-worker
}

log "validating inputs"
require_file "$ARCHIVE"
require_file "$ENV_FILE"
[ -d "$SRC_DIR" ] || { echo "missing source dir: $SRC_DIR" >&2; exit 1; }
podman network exists "$NETWORK" || { echo "missing podman network: $NETWORK" >&2; exit 1; }
podman volume exists "$MAIL_VOL" || { echo "missing podman volume: $MAIL_VOL" >&2; exit 1; }

ACTUAL_SHA=$(sha256sum "$ARCHIVE" | awk '{print toupper($1)}')
[ "$ACTUAL_SHA" = "$EXPECTED_SHA" ] || { echo "archive sha256 mismatch: $ACTUAL_SHA" >&2; exit 1; }

[ ! -e "$STAGING" ] || { echo "staging path already exists: $STAGING" >&2; exit 1; }
[ ! -e "$BACKUP" ] || { echo "backup path already exists: $BACKUP" >&2; exit 1; }

log "extracting source to $STAGING"
mkdir -p "$STAGING"
tar -xzf "$ARCHIVE" -C "$STAGING"
printf '%s\n' "$COMMIT" > "$STAGING/.deploy-commit"

require_file "$STAGING/Dockerfile"
require_file "$STAGING/backend/go.mod"
require_file "$STAGING/frontend/package.json"

log "building frontend/dist outside podman build"
mkdir -p /tmp/shiromail-npm-cache
podman run --rm --network host \
  -v "$STAGING/frontend:/frontend" \
  -v /tmp/shiromail-npm-cache:/root/.npm \
  -w /frontend \
  docker.io/library/node:22-alpine \
  sh -lc 'npm ci --no-audit --no-fund && npm run build'

log "writing Dockerfile.remote"
cat >"$STAGING/Dockerfile.remote" <<'DOCKER'
FROM docker.io/library/golang:1.24-alpine AS backend-builder
RUN apk add --no-cache git

WORKDIR /backend
COPY backend/go.mod backend/go.sum ./
COPY backend/vendor ./vendor
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-s -w" -o /bin/shiro-api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-s -w" -o /bin/shiro-worker ./cmd/worker

FROM docker.io/library/alpine:3.20
RUN apk add --no-cache ca-certificates tzdata nginx
RUN adduser -D -u 1000 shiro
ENV GIN_MODE=release

COPY --from=backend-builder /bin/shiro-api /usr/local/bin/shiro-api
COPY --from=backend-builder /bin/shiro-worker /usr/local/bin/shiro-worker
COPY frontend/dist /usr/share/nginx/html
COPY frontend/nginx.conf /etc/nginx/http.d/default.conf
COPY docker/start-app.sh /usr/local/bin/start-app

RUN sed -i 's/\r$//' /usr/local/bin/start-app \
  && chmod +x /usr/local/bin/start-app \
  && sed -i '/^user /d' /etc/nginx/nginx.conf \
  && mkdir -p /app/data/mail /run/nginx /var/lib/nginx/tmp /var/log/nginx \
  && chown -R shiro:shiro /app /run/nginx /var/lib/nginx /var/log/nginx /usr/share/nginx/html

USER shiro
WORKDIR /app
EXPOSE 80 8080 2525
CMD ["/usr/local/bin/start-app"]
DOCKER

log "building image $NEW_IMAGE"
podman build -f "$STAGING/Dockerfile.remote" -t "$NEW_IMAGE" "$STAGING"

if podman image exists "$IMAGE"; then
  log "tagging rollback image $ROLLBACK_IMAGE"
  podman tag "$IMAGE" "$ROLLBACK_IMAGE"
fi

log "tagging $IMAGE"
podman tag "$NEW_IMAGE" "$IMAGE"

log "backing up source to $BACKUP"
mv "$SRC_DIR" "$BACKUP"
mv "$STAGING" "$SRC_DIR"

if command -v systemctl >/dev/null 2>&1; then
  log "stopping app/worker systemd services"
  systemctl stop container-shiromail-worker.service container-shiromail-app.service || true
fi

start_app_worker "$IMAGE"
refresh_app_units

log "waiting for health check"
if ! health_check; then
  log "new deployment failed health check"
  podman logs --tail 120 shiromail-app || true

  if podman image exists "$ROLLBACK_IMAGE"; then
    log "rolling back to $ROLLBACK_IMAGE"
    podman tag "$ROLLBACK_IMAGE" "$IMAGE"
    start_app_worker "$IMAGE"
    refresh_app_units
    health_check || true
  fi

  exit 1
fi

log "deployment finished"
podman ps -a --format '{{.Names}} {{.Status}} {{.Image}} {{.Ports}}' | grep shiromail || true
log "source backup: $BACKUP"
log "new source: $SRC_DIR"
log "rollback image: $ROLLBACK_IMAGE"
EOF

bash /tmp/publish-shiromail-3310aed.sh
```

## 发布后验证

发布脚本会自动运行一次健康检查。发布后建议再执行：

```bash
podman ps -a --format '{{.Names}} {{.Status}} {{.Image}} {{.Ports}}' | grep shiromail
curl -fsS --max-time 10 http://127.0.0.1:5173/api/v1/site/stats
podman logs --tail 120 shiromail-app
podman logs --tail 120 shiromail-worker
test "$(cat /opt/shiromail/source/.deploy-commit)" = "3310aed"
```

期望结果：

- `shiromail-app` 和 `shiromail-worker` 都是 `Up`。
- `/api/v1/site/stats` 返回 JSON。
- `/opt/shiromail/source/.deploy-commit` 内容是 `3310aed`。
- app/worker 日志没有持续崩溃或数据库连接错误。

## 功能验收建议

本版本核心目标是支持“邮箱不存在时先收信，管理员后续创建该邮箱后可以查看之前收到的邮件”。

建议远程 agent 或人工做以下验证：

1. 确认目标域名是已启用的托管域。
2. 在该域下选择一个尚不存在的邮箱地址。
3. 向该地址发送一封测试邮件。
4. 邮件发送后再由管理员创建这个邮箱。
5. 管理后台打开该邮箱，确认能看到创建前收到的测试邮件。

注意：如果数据库里已有 `mail.inbound_policy` 配置行，且其中 `allowCatchAll=false` 或 `requireExistingMailbox=true`，需要在管理后台把入站策略调整为：

```json
{
  "allowCatchAll": true,
  "requireExistingMailbox": false
}
```

不要直接打印或泄露数据库连接串、JWT 密钥、邮箱密码。

## 回滚

如果发布后健康检查失败，脚本会自动尝试把 `ghcr.io/galiais/shiromail:latest` 回滚到旧镜像。

如果需要手动回滚：

```bash
set -euo pipefail

ROLLBACK_IMAGE="localhost/shiromail:rollback-before-3310aed-替换为脚本输出的时间戳"
IMAGE=ghcr.io/galiais/shiromail:latest

podman tag "$ROLLBACK_IMAGE" "$IMAGE"
systemctl stop container-shiromail-worker.service container-shiromail-app.service || true
podman rm -f shiromail-worker shiromail-app || true

podman run -d --name shiromail-app \
  --restart=unless-stopped \
  --network shiromail-net \
  --sysctl net.ipv4.ip_unprivileged_port_start=0 \
  --env-file /opt/shiromail/env \
  -p 5173:80 \
  -p 25:2525 \
  -v shiromail_mail_data:/app/data/mail \
  "$IMAGE"

podman run -d --name shiromail-worker \
  --restart=unless-stopped \
  --network shiromail-net \
  --env-file /opt/shiromail/env \
  -v shiromail_mail_data:/app/data/mail \
  "$IMAGE" shiro-worker

curl -fsS --max-time 10 http://127.0.0.1:5173/api/v1/site/stats
```

源码目录的备份路径会在发布脚本末尾输出，格式类似：

```text
/opt/shiromail/source.backup.before-3310aed.YYYYMMDDHHMMSS
```

## 安全边界

远程 agent 必须遵守：

- 不要删除或重建 `shiromail-mysql`、`shiromail-redis`。
- 不要执行 `podman volume prune`。
- 不要执行 `podman system prune --volumes`。
- 不要删除 `shiromail_mysql_data`、`shiromail_redis_data`、`shiromail_mail_data`。
- 不要打印 `/opt/shiromail/env` 中的密钥。
- 构建成功前不要切换 `/opt/shiromail/source`。
- 健康检查失败时优先回滚镜像和 app/worker 容器。
