# ShiroMail 完善优化方案

> 基于当前代码库的全面审计，按优先级分类列出所有可改进项。
> 审计日期：2026-05-09

---

## 目录

1. [安全加固（Critical / High）](#1-安全加固)
2. [后端性能优化](#2-后端性能优化)
3. [前端优化](#3-前端优化)
4. [部署与运维](#4-部署与运维)
5. [CI/CD 改进](#5-cicd-改进)
6. [可观测性与监控](#6-可观测性与监控)
7. [代码质量与可维护性](#7-代码质量与可维护性)
8. [功能完善建议](#8-功能完善建议)

---

## 1. 安全加固

### 1.1 Critical

| # | 问题 | 位置 | 影响 | 修复方案 |
|---|------|------|------|----------|
| S1 | WebSocket `CheckOrigin` 接受所有来源 | `backend/internal/realtime/hub.go:39` | 跨站 WebSocket 劫持（CSWSH），攻击者可在恶意页面中利用受害者 token 建立 WS 连接 | 复用 CORS 白名单校验 Origin header |
| S2 | SMTP 无 TLS/STARTTLS 支持 | `backend/internal/modules/ingest/smtp/server.go` | 所有入站邮件明文传输，中间人可窃取邮件内容 | 添加 STARTTLS 支持，加载证书配置 |
| S3 | `/metrics` 端点无认证 | `backend/internal/bootstrap/app.go` | 信息泄露：请求量、错误率、延迟等运营数据对外暴露 | 添加 Bearer token 或 IP 白名单认证 |

### 1.2 High

| # | 问题 | 位置 | 影响 | 修复方案 |
|---|------|------|------|----------|
| S4 | Nginx 缺少安全响应头 | `frontend/nginx.conf` | XSS、点击劫持、MIME 嗅探风险 | 添加 `X-Frame-Options`, `X-Content-Type-Options`, `Strict-Transport-Security`, `Content-Security-Policy`, `Referrer-Policy` |
| S5 | 服务端无密码复杂度校验 | `backend/internal/modules/auth/` | 前端校验可绕过，用户可设置弱密码 | 在 Register/ChangePassword handler 中强制校验 `PasswordMinLength` 等策略 |
| S6 | JWT 生产环境守卫不完整 | `backend/internal/bootstrap/app.go:66-68` | 仅检查 `"dev-secret"`，`.env.example` 默认值 `"change-me-in-production"` 可通过检查 | 增加最小长度检查（≥32 字符）和常见弱密钥黑名单 |
| S7 | 无 SPF/DKIM/DMARC 验证 | `backend/internal/modules/ingest/smtp/session.go` | 接收伪造发件人邮件，影响用户信任度 | 集成 SPF 验证库，记录 DKIM 验证结果到 message 元数据 |
| S8 | 限流器 Redis 故障时静默放行 | `backend/internal/middleware/ratelimit.go:50-53` | Redis 宕机时所有限流失效，auth 端点暴露于暴力破解 | 对关键端点（login/register/forgot-password）在 Redis 不可用时返回 503 或使用本地令牌桶降级 |

### 1.3 Medium

| # | 问题 | 位置 | 修复方案 |
|---|------|------|----------|
| S9 | Nginx 无 `client_max_body_size` | `frontend/nginx.conf` | 添加 `client_max_body_size 10m` 防止大文件 DoS |
| S10 | CORS 反射任意请求头 | `backend/internal/middleware/cors.go:25-29` | 改为显式允许的 header 白名单 |
| S11 | Webhook 签名使用 `SecretPreview` 字段 | `backend/internal/webhook/dispatcher.go:79` | 确认使用完整密钥签名，重命名字段避免歧义 |
| S12 | `io.ReadAll` 无大小限制 | `backend/internal/modules/ingest/direct_service.go:104` | 使用 `io.LimitReader(source, maxSize)` 包装 |
| S13 | SMTP 无单会话最大收件人数限制 | `backend/internal/modules/ingest/smtp/session.go` | 添加 `MaxRecipients` 配置（建议 100） |

---

## 2. 后端性能优化

### 2.1 Critical — 数据库查询

| # | 问题 | 位置 | 影响 | 修复方案 |
|---|------|------|------|----------|
| P1 | `PeekNextSpool` 加载全表含 LONGBLOB | `backend/internal/modules/ingest/spool.go:222-238` | 每秒 Worker tick 加载全部原始邮件到内存 | 改为 `SELECT id FROM inbound_message_spool WHERE status='pending' AND next_attempt_at <= ? ORDER BY id LIMIT 1` |
| P2 | `ListUsers` N+1 查询 | `backend/internal/modules/auth/mysql_repository.go:119-134` | 每个用户触发一次额外 JOIN 查询 | 批量 Preload roles，或为 `PublicSiteStats` 使用 `COUNT(*)` |
| P3 | `PublicSiteStats` 无缓存 | `backend/internal/bootstrap/app.go:337-350` | 公开端点每次请求触发 5+ 次 DB 查询 | 添加 30-60 秒 Redis 缓存 |
| P4 | `ListMailboxes` API Key 场景 N+1 | `backend/internal/modules/mailbox/service.go:130-140` | 每个邮箱触发一次 domain 查询 | 批量查询 domain IDs，构建 map 后过滤 |

### 2.2 High — Worker 与并发

| # | 问题 | 位置 | 修复方案 |
|---|------|------|----------|
| P5 | Worker 1 秒固定轮询 | `backend/internal/bootstrap/app.go:681` | 改为自适应间隔：空闲时 5-10 秒，有任务时 500ms；或使用 Redis Pub/Sub 事件驱动 |
| P6 | Webhook 无界 goroutine 无重试 | `backend/internal/webhook/dispatcher.go:60-66` | 引入有界 worker pool（`chan struct{}` 信号量），添加指数退避重试队列 |
| P7 | Worker 各 Job 串行执行 | `backend/internal/bootstrap/worker.go:16-29` | 使用 `errgroup.Group` 并行执行独立 Job |
| P8 | Spool 并发 Worker 无共享取消 | `backend/internal/jobs/sync_messages.go:70-98` | 使用 `errgroup.WithContext` 实现错误传播和统一取消 |

### 2.3 Medium — 连接与缓存

| # | 问题 | 位置 | 修复方案 |
|---|------|------|----------|
| P9 | Redis 连接池未配置 | `backend/internal/database/redis.go:21-24` | 配置 `PoolSize`, `MinIdleConns`, `MaxRetries`, `DialTimeout`, `ReadTimeout` |
| P10 | `DeleteByPattern` 使用 SCAN | `backend/internal/shared/cache/cache.go:58-79` | 改为结构化 key 命名 + 精确删除，或使用 Redis Hash 按 mailbox 分组 |
| P11 | Message 列表无分页 | `backend/internal/modules/message/mysql_repository.go:207-221` | 添加 `LIMIT/OFFSET` 或游标分页 |
| P12 | 缺少复合索引 | `messages` 表 | 添加 `idx_messages_deleted_received (is_deleted, received_at)` 索引 |

---

## 3. 前端优化

### 3.1 构建与包体积

| # | 问题 | 位置 | 修复方案 |
|---|------|------|----------|
| F1 | 无 vendor chunk 拆分 | `frontend/vite.config.ts` | 添加 `manualChunks` 拆分 react/radix-ui/tanstack-query/i18next/axios 为独立缓存块 |
| F2 | `animate.css` 死依赖 | `frontend/package.json:18` | 从 dependencies 中移除 |
| F3 | `shadcn` CLI 在 dependencies | `frontend/package.json:29` | 移至 devDependencies |
| F4 | `@base-ui/react` 仅用于一个组件 | `frontend/src/components/ui/combobox.tsx` | 评估是否可用 radix-ui 替代，减少包体积 |

### 3.2 运行时性能

| # | 问题 | 位置 | 修复方案 |
|---|------|------|----------|
| F5 | 巨型单文件页面组件 | `dns-page.tsx` (3062行), `mailboxes-page.tsx` (1113行) | 拆分为子组件：列表、详情、表单、对话框各自独立 |
| F6 | 日期格式硬编码 `"zh-CN"` | 7+ 文件中的 `Intl.DateTimeFormat` | 使用 `i18n.language` 动态获取当前语言 |
| F7 | `useIsMobile` 初始值 undefined | `frontend/src/hooks/use-mobile.ts:6` | 使用 `window.matchMedia` 同步初始化，避免移动端布局闪烁 |
| F8 | `useAutoDismiss` 回调不稳定 | `frontend/src/hooks/use-auto-dismiss.ts:15` | 使用 `useRef` 包装 `onDismiss` 避免 effect 重复触发 |
| F9 | 无 API 数据预取 | 路由 prefetch 仅加载 JS chunk | 在 `prefetch-route.ts` 中添加 `queryClient.prefetchQuery` 预取关键数据 |

### 3.3 可访问性

| # | 问题 | 修复方案 |
|---|------|----------|
| F10 | 无 skip-to-content 链接 | 在 layout shell 顶部添加 `<a href="#main-content" class="sr-only focus:not-sr-only">` |
| F11 | 无 `aria-live` 区域 | 为自动刷新的邮件列表、加载状态添加 `aria-live="polite"` |
| F12 | ErrorBoundary 无焦点管理 | 错误发生时将焦点移至错误消息容器 |

### 3.4 TanStack Query 策略

| # | 问题 | 修复方案 |
|---|------|----------|
| F13 | `refetchOnWindowFocus: false` 全局禁用 | 对频繁变化的数据（邮件列表）启用 `refetchOnWindowFocus`，对静态数据保持禁用 |
| F14 | 翻译文件静态导入 | 当前体积可接受（~935行），但如翻译增长应改为 `import()` 动态加载 |

---

## 4. 部署与运维

### 4.1 Nginx 加固

```nginx
# 建议添加到 frontend/nginx.conf 的 server 块
add_header X-Frame-Options "SAMEORIGIN" always;
add_header X-Content-Type-Options "nosniff" always;
add_header X-XSS-Protection "0" always;
add_header Referrer-Policy "strict-origin-when-cross-origin" always;
add_header Permissions-Policy "camera=(), microphone=(), geolocation=()" always;
add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self' wss:" always;
client_max_body_size 10m;
```

### 4.2 Docker 优化

| # | 问题 | 修复方案 |
|---|------|----------|
| D1 | 未指定目标平台 | 在 docker-build.yml 中添加 `platforms: linux/amd64,linux/arm64` |
| D2 | 单容器多进程 | 长期考虑拆分为独立容器：nginx + api + worker，便于独立扩缩容 |
| D3 | 无健康检查粒度 | 为 API 和 Worker 分别添加 `/healthz` 和 `/readyz` 端点 |
| D4 | 无资源限制 | 在 docker-compose.yml 中添加 `deploy.resources.limits` (CPU/Memory) |

### 4.3 数据备份

| # | 方案 | 说明 |
|---|------|------|
| D5 | MySQL 自动备份 | 添加 cron 容器或 sidecar 执行 `mysqldump --single-transaction` 定时备份 |
| D6 | 备份存储 | 配置 S3/MinIO 远程存储，保留 7 天滚动 + 30 天周备份 |
| D7 | 恢复演练文档 | 编写灾难恢复 runbook，包含 RTO/RPO 目标 |
| D8 | EML 文件备份 | mail_data volume 纳入备份策略 |

---

## 5. CI/CD 改进

### 5.1 安全扫描

```yaml
# 建议添加到 .github/workflows/ci.yml
- name: Go vulnerability check
  run: govulncheck ./...

- name: npm audit
  run: npm audit --audit-level=high
  working-directory: frontend

- name: Container scan
  uses: aquasecurity/trivy-action@master
  with:
    image-ref: ghcr.io/galiais/shiromail:${{ github.sha }}
    severity: HIGH,CRITICAL
```

### 5.2 质量门禁

| # | 改进项 | 说明 |
|---|--------|------|
| C1 | 代码覆盖率报告 | 后端 `go test -coverprofile`，前端 `vitest --coverage`，上传至 Codecov |
| C2 | Lint 强化 | 后端添加 `golangci-lint`（启用 errcheck, ineffassign, govet），前端确保 ESLint 零警告 |
| C3 | 依赖审计 | `npm audit` + `govulncheck` 作为 CI 必过步骤 |
| C4 | E2E 测试 | 引入 Playwright 覆盖关键路径：注册→创建邮箱→收信→查看 |
| C5 | 部署流水线 | 添加 staging 环境自动部署 + smoke test + 手动审批生产发布 |

---

## 6. 可观测性与监控

### 6.1 日志

| # | 改进项 | 说明 |
|---|--------|------|
| O1 | 请求关联 ID | 在 middleware 中生成 `X-Request-ID`，贯穿日志链路 |
| O2 | 结构化访问日志 | 记录 method, path, status, latency, user_id, ip |
| O3 | 认证失败审计 | 记录所有失败的 login/register/2fa 尝试，含 IP 和 user agent |
| O4 | SMTP 会话日志 | 记录连接来源 IP、EHLO 域名、收件人、处理结果 |

### 6.2 指标

| # | 改进项 | 说明 |
|---|--------|------|
| O5 | 指标端点认证 | 添加 Bearer token 或仅绑定内部网络 |
| O6 | 业务指标 | 添加：活跃邮箱数、消息入站速率、spool 积压量、webhook 投递成功率 |
| O7 | SLI/SLO 定义 | API P99 延迟 < 200ms，SMTP 入站处理 < 5s，可用性 99.9% |

### 6.3 告警

| # | 告警规则 | 阈值建议 |
|---|----------|----------|
| O8 | Spool 积压 | pending 数量 > 100 持续 5 分钟 |
| O9 | 5xx 错误率 | > 1% 持续 2 分钟 |
| O10 | Redis 连接失败 | 任何连接失败立即告警 |
| O11 | 磁盘使用率 | mail_data volume > 80% |
| O12 | 证书过期 | TLS 证书到期前 14 天 |

---

## 7. 代码质量与可维护性

### 7.1 后端

| # | 改进项 | 说明 |
|---|--------|------|
| Q1 | 统一错误处理 | 定义 `AppError` 类型，包含 code/message/details，中间件统一格式化响应 |
| Q2 | Repository 接口测试 | 为每个 repository 接口添加 mock 实现，提升单元测试覆盖率 |
| Q3 | 配置验证 | 启动时校验所有必需配置项，缺失时 panic 并给出明确提示 |
| Q4 | 优雅关闭 | API server 添加 `Shutdown(ctx)` 等待活跃请求完成，Worker 等待当前 job 结束 |
| Q5 | Context 传播 | 确保所有 DB 查询和外部调用使用请求级 context，支持超时和取消 |

### 7.2 前端

| # | 改进项 | 说明 |
|---|--------|------|
| Q6 | 页面组件拆分 | DNS 页面（3062行）拆分为：ZoneList, ZoneDetail, RecordEditor, ChangeSetPanel |
| Q7 | 共享类型定义 | 提取 API 响应类型到 `src/types/` 目录，避免各页面重复定义 |
| Q8 | Hook 测试 | 为 `use-auto-dismiss`, `use-mobile`, `use-page-title` 添加单元测试 |
| Q9 | HTTP 客户端测试 | 测试 token refresh 竞态处理和 session 失效逻辑 |
| Q10 | Error Boundary 增强 | 添加错误上报（Sentry 等），区分网络错误和应用错误 |

---

## 8. 功能完善建议

### 8.1 邮件系统

| # | 功能 | 优先级 | 说明 |
|---|------|--------|------|
| E1 | STARTTLS 入站 | High | 支持加密 SMTP 连接，提升与外部 MTA 的兼容性 |
| E2 | SPF/DKIM 验证 | High | 标记邮件验证状态，帮助用户识别伪造邮件 |
| E3 | 邮件搜索 | Medium | 基于 MySQL FULLTEXT 或 Meilisearch 实现全文搜索 |
| E4 | 邮件转发 | Medium | 支持将收到的邮件转发到用户真实邮箱 |
| E5 | 自定义邮箱地址 | Low | 允许用户指定邮箱前缀而非纯随机 |

### 8.2 平台能力

| # | 功能 | 优先级 | 说明 |
|---|------|--------|------|
| E6 | OAuth 社交登录 | Medium | 支持 GitHub/Google OAuth，降低注册门槛 |
| E7 | 多语言扩展 | Low | 添加日语、韩语等语言支持 |
| E8 | API 文档 | Medium | 基于 OpenAPI spec 生成交互式 API 文档（Swagger UI） |
| E9 | 速率限制仪表盘 | Low | 用户可查看自己的 API 使用量和剩余配额 |
| E10 | Webhook 投递日志 | Medium | 记录每次 webhook 投递的状态码、延迟、重试次数 |

### 8.3 运营工具

| # | 功能 | 优先级 | 说明 |
|---|------|--------|------|
| E11 | Admin 仪表盘 | Medium | 实时展示系统健康状态、spool 积压、活跃连接数 |
| E12 | 用户封禁/解封 | High | Admin 可禁用滥用用户，阻止其创建邮箱 |
| E13 | 域名健康监控 | Medium | 定期检查 DNS 记录是否正确，MX 是否可达 |
| E14 | 数据导出 | Low | 用户可导出自己的邮件数据（GDPR 合规） |

---

## 实施路线图

### Phase 1 — 安全加固（1-2 周）

- [ ] S1: 修复 WebSocket Origin 校验
- [ ] S3: `/metrics` 添加认证
- [ ] S4: Nginx 安全头
- [ ] S5: 服务端密码校验
- [ ] S6: JWT secret 强度检查
- [ ] S8: 限流器降级策略
- [ ] S9: Nginx body size 限制
- [ ] S12: 入站消息大小限制

### Phase 2 — 性能优化（2-3 周）

- [ ] P1: 修复 PeekNextSpool 全表扫描
- [ ] P2: PublicSiteStats 使用 COUNT 查询
- [ ] P3: PublicSiteStats 添加缓存
- [ ] P5: Worker 自适应轮询
- [ ] P6: Webhook bounded worker pool
- [ ] P9: Redis 连接池配置
- [ ] P11: Message 列表分页
- [ ] P12: 添加缺失索引

### Phase 3 — 前端优化（1-2 周）

- [ ] F1: Vite manualChunks 配置
- [ ] F2: 移除 animate.css
- [ ] F3: shadcn 移至 devDependencies
- [ ] F5: 拆分大型页面组件（优先 dns-page）
- [ ] F6: 修复硬编码 locale
- [ ] F7: useIsMobile 同步初始化

### Phase 4 — 运维与可观测性（2-3 周）

- [ ] O1: 请求关联 ID
- [ ] O2: 结构化访问日志
- [ ] O5: 指标端点认证
- [ ] D1: Docker 多平台构建
- [ ] D4: 容器资源限制
- [ ] D5: MySQL 自动备份
- [ ] C1: 代码覆盖率报告
- [ ] C2: golangci-lint + ESLint 强化

### Phase 5 — 功能完善（持续）

- [ ] E1: STARTTLS
- [ ] E2: SPF/DKIM 验证
- [ ] E8: OpenAPI 文档
- [ ] E10: Webhook 投递日志
- [ ] E12: 用户封禁功能

---

## 附录：关键文件索引

| 领域 | 关键文件 |
|------|----------|
| API 入口 | `backend/cmd/api/main.go` |
| Worker 入口 | `backend/cmd/worker/main.go` |
| 路由注册 | `backend/internal/bootstrap/app.go` |
| SMTP 服务器 | `backend/internal/modules/ingest/smtp/server.go` |
| 认证模块 | `backend/internal/modules/auth/` |
| 限流中间件 | `backend/internal/middleware/ratelimit.go` |
| WebSocket Hub | `backend/internal/realtime/hub.go` |
| Webhook 分发 | `backend/internal/webhook/dispatcher.go` |
| 数据库迁移 | `backend/internal/database/migrations/` |
| 前端路由 | `frontend/src/app/router.tsx` |
| HTTP 客户端 | `frontend/src/lib/http.ts` |
| Vite 配置 | `frontend/vite.config.ts` |
| Nginx 配置 | `frontend/nginx.conf` |
| Docker 配置 | `Dockerfile`, `docker-compose.yml` |
| CI 流水线 | `.github/workflows/ci.yml` |
