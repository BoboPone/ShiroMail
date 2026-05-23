# ShiroMail API 操作说明书

本文面向脚本、自动化 Agent 或第三方系统调用 ShiroMail API，覆盖鉴权、生成临时邮箱、获取邮件、提取验证码、下载原始邮件、续期与释放邮箱等常用操作。

## 1. 基础信息

所有业务接口默认以 `/api/v1` 为前缀。

常见部署下的 `BASE` 示例：

```bash
# Docker Compose / 前端同源部署
BASE="http://127.0.0.1:5173/api/v1"

# 直接访问后端 API 开发服务
BASE="http://127.0.0.1:8080/api/v1"
```

后续示例默认使用：

```bash
TOKEN="你的_API_KEY_或_ACCESS_TOKEN"
```

统一请求头：

```http
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

## 2. 鉴权方式

ShiroMail 支持两类调用模式：

1. 用户登录态：先调用登录接口获取 `accessToken`，再以 `Authorization: Bearer <accessToken>` 调用业务接口。
2. API Key：在控制台创建 API Key 后，直接以 `Authorization: Bearer <apiKey>` 调用业务接口。自动化场景推荐使用 API Key。

### 2.1 登录获取 Access Token

```bash
curl -s -X POST "$BASE/auth/login" \
  -H "Content-Type: application/json" \
  -d '{
    "login": "your_username_or_email",
    "password": "your_password"
  }'
```

成功响应包含：

```json
{
  "status": "ok",
  "userId": 1,
  "username": "demo",
  "roles": ["user"],
  "accessToken": "...",
  "refreshToken": "..."
}
```

### 2.2 创建 API Key

创建 API Key 需要用户登录态，不能用匿名请求创建。

```bash
curl -s -X POST "$BASE/portal/api-keys" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "automation",
    "scopes": [
      "domains.read",
      "mailboxes.read",
      "mailboxes.write",
      "messages.read",
      "messages.attachments.read"
    ],
    "resourcePolicy": {
      "domainAccessMode": "mixed",
      "allowPlatformPublicDomains": true,
      "allowUserPublishedDomains": true,
      "allowOwnedPrivateDomains": true,
      "allowProviderMutation": false,
      "allowProtectedRecordWrite": false
    },
    "domainBindings": []
  }'
```

响应中的 `plainSecret` 只会在创建或轮换时返回一次，应立即保存：

```json
{
  "id": 1,
  "name": "automation",
  "plainSecret": "sk_live_...",
  "scopes": ["domains.read", "mailboxes.read", "mailboxes.write", "messages.read"]
}
```

### 2.3 常用 Scope

生成邮箱并读取邮件至少需要：

- `domains.read`：读取可用域名。
- `mailboxes.read`：读取邮箱列表和仪表盘。
- `mailboxes.write`：创建、续期、释放邮箱。
- `messages.read`：读取邮件列表、详情、提取结果、原始邮件。

按需增加：

- `messages.attachments.read`：下载附件。
- `messages.write`：通过 API 注入测试邮件。
- `public_pool.use`：使用公共池域名创建邮箱时可能需要。

## 3. 生成邮箱

### 3.1 获取可用域名

先列出当前身份可访问的域名，并选择一个可用于创建邮箱的 `domainId`。

```bash
curl -s "$BASE/domains" \
  -H "Authorization: Bearer $TOKEN"
```

典型响应：

```json
{
  "items": [
    {
      "id": 2,
      "domain": "example.test",
      "status": "active",
      "visibility": "private",
      "healthStatus": "healthy"
    }
  ]
}
```

### 3.2 创建随机邮箱

不传 `localPart` 时，后端会自动生成邮箱前缀。

```bash
curl -s -X POST "$BASE/mailboxes" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "domainId": 2,
    "expiresInHours": 24
  }'
```

成功响应：

```json
{
  "id": 15,
  "userId": 1,
  "domainId": 2,
  "domain": "example.test",
  "localPart": "a1b2c3d4e5f6",
  "address": "a1b2c3d4e5f6@example.test",
  "status": "active",
  "isPermanent": false,
  "expiresAt": "2026-05-14T08:00:00Z",
  "createdAt": "2026-05-13T08:00:00Z",
  "updatedAt": "2026-05-13T08:00:00Z"
}
```

### 3.3 创建自定义邮箱

```bash
curl -s -X POST "$BASE/mailboxes" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "domainId": 2,
    "expiresInHours": 24,
    "localPart": "agentbox"
  }'
```

字段要求：

- `domainId` 必须是正整数。
- 普通临时邮箱的 `expiresInHours` 必须是大于 0 的整数。
- 永久邮箱传 `isPermanent: true`；此时可不传 `expiresInHours`，邮箱会保留到手动释放。
- `localPart` 可选；自定义时会转为小写。
- `localPart` 需以小写字母或数字开头，后续允许小写字母、数字、点号、下划线和中划线。
- JSON 字段必须使用 camelCase，例如 `domainId`、`expiresInHours`、`isPermanent`，不要写成 `domain_id`、`expires_in_hours`。

### 3.4 创建永久邮箱

永久邮箱不会被过期清理任务释放，除非显式调用释放接口。

```bash
curl -s -X POST "$BASE/mailboxes" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "domainId": 2,
    "localPart": "agentbox",
    "isPermanent": true
  }'
```

成功响应中的 `isPermanent` 为 `true`，`expiresAt` 会是一个远未来时间。

## 4. 获取邮件

创建邮箱后，将响应里的 `address` 提供给目标网站或系统发信，然后轮询邮件列表。

### 4.1 查询邮箱列表

```bash
curl -s "$BASE/mailboxes" \
  -H "Authorization: Bearer $TOKEN"
```

响应：

```json
{
  "items": [
    {
      "id": 15,
      "address": "agentbox@example.test",
      "status": "active",
      "isPermanent": false,
      "expiresAt": "2026-05-14T08:00:00Z"
    }
  ]
}
```

### 4.2 查询某个邮箱的邮件列表

```bash
MAILBOX_ID=15

curl -s "$BASE/mailboxes/$MAILBOX_ID/messages" \
  -H "Authorization: Bearer $TOKEN"
```

邮件列表按 `receivedAt` 倒序返回。典型响应：

```json
{
  "items": [
    {
      "id": 123,
      "mailboxId": 15,
      "mailboxAddress": "agentbox@example.test",
      "fromAddr": "no-reply@example.com",
      "toAddr": "agentbox@example.test",
      "subject": "Your verification code",
      "textPreview": "Your code is 123456",
      "htmlPreview": "",
      "hasAttachments": false,
      "attachmentCount": 0,
      "sizeBytes": 2048,
      "isRead": false,
      "isDeleted": false,
      "receivedAt": "2026-05-13T08:01:00Z"
    }
  ]
}
```

也可以带关键词搜索：

```bash
curl -s "$BASE/mailboxes/$MAILBOX_ID/messages?q=verification" \
  -H "Authorization: Bearer $TOKEN"
```

### 4.3 读取邮件详情

```bash
MESSAGE_ID=123

curl -s "$BASE/mailboxes/$MAILBOX_ID/messages/$MESSAGE_ID" \
  -H "Authorization: Bearer $TOKEN"
```

详情响应包含正文、HTML、headers 和附件元数据：

```json
{
  "id": 123,
  "mailboxId": 15,
  "subject": "Your verification code",
  "fromAddr": "no-reply@example.com",
  "toAddr": "agentbox@example.test",
  "textBody": "Your code is 123456",
  "htmlBody": "<p>Your code is <b>123456</b></p>",
  "headers": {
    "Message-Id": ["..."]
  },
  "attachments": []
}
```

## 5. 提取验证码或链接

自动化读取验证码时，优先调用提取结果接口。它会按已配置的提取规则返回结构化结果，避免自己解析邮件正文。

```bash
curl -s "$BASE/mailboxes/$MAILBOX_ID/messages/$MESSAGE_ID/extractions" \
  -H "Authorization: Bearer $TOKEN"
```

典型响应：

```json
{
  "items": [
    {
      "ruleId": 1,
      "ruleName": "verification-code",
      "label": "code",
      "sourceType": "admin_default",
      "sourceField": "text_body",
      "value": "123456",
      "matchedText": "123456"
    }
  ]
}
```

推荐读取顺序：

1. 先读 `/extractions`。
2. 若无匹配，再读邮件详情里的 `subject`、`textBody`、`htmlBody`。
3. MIME 结构复杂时，再读 `/raw/parsed`。
4. 只有需要原始 RFC822 内容时，才读 `/raw`。

## 6. 原始邮件和附件

### 6.1 获取解析后的原始邮件

```bash
curl -s "$BASE/mailboxes/$MAILBOX_ID/messages/$MESSAGE_ID/raw/parsed" \
  -H "Authorization: Bearer $TOKEN"
```

响应包含解析后的正文、HTML、headers 和附件列表：

```json
{
  "messageId": 123,
  "mailboxId": 15,
  "subject": "Your verification code",
  "fromAddr": "no-reply@example.com",
  "toAddr": "agentbox@example.test",
  "textBody": "Your code is 123456",
  "htmlBody": "<p>Your code is <b>123456</b></p>",
  "headers": {},
  "attachmentCount": 0,
  "attachments": [],
  "rawSizeBytes": 2048
}
```

### 6.2 下载原始 EML

```bash
curl -L "$BASE/mailboxes/$MAILBOX_ID/messages/$MESSAGE_ID/raw" \
  -H "Authorization: Bearer $TOKEN" \
  -o "message-$MESSAGE_ID.eml"
```

### 6.3 下载附件

附件下标从 0 开始。

```bash
ATTACHMENT_INDEX=0

curl -L "$BASE/mailboxes/$MAILBOX_ID/messages/$MESSAGE_ID/attachments/$ATTACHMENT_INDEX" \
  -H "Authorization: Bearer $TOKEN" \
  -o "attachment-$ATTACHMENT_INDEX.bin"
```

## 7. 续期与释放邮箱

### 7.1 续期

`expiresInHours` 会基于当前过期时间继续增加；如果邮箱已过期，则以当前时间为基准。永久邮箱无需续期，调用续期接口会保持永久状态不变。

```bash
curl -s -X POST "$BASE/mailboxes/$MAILBOX_ID/extend" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "expiresInHours": 24
  }'
```

### 7.2 释放

释放后应停止轮询该邮箱。后端会停止该邮箱后续收信，并清理相关消息可见性。

```bash
curl -s -X POST "$BASE/mailboxes/$MAILBOX_ID/release" \
  -H "Authorization: Bearer $TOKEN"
```

## 8. 测试注入邮件

如果需要在测试环境中直接向邮箱处理流程注入一封 RFC822 邮件，可以使用：

```bash
curl -s -X POST "$BASE/mailboxes/$MAILBOX_ID/messages/receive" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "mailFrom": "tester@example.com",
    "raw": "From: tester@example.com\r\nTo: agentbox@example.test\r\nSubject: Test code\r\n\r\nYour code is 123456"
  }'
```

该接口需要 `messages.write` scope，并且依赖服务端启用直接入站处理能力。

## 9. 推荐自动化流程

验证码或注册邮箱自动化推荐按以下步骤实现：

1. `GET /domains` 获取可用域名。
2. `POST /mailboxes` 创建邮箱，保存 `mailboxId` 和 `address`。
3. 将 `address` 提交给目标网站或系统。
4. 每 2 到 5 秒轮询 `GET /mailboxes/:mailboxId/messages`。
5. 发现新邮件后，取最新邮件的 `id`。
6. 调用 `GET /mailboxes/:mailboxId/messages/:messageId/extractions` 获取验证码或链接。
7. 若提取结果为空，再调用邮件详情或 `/raw/parsed` 兜底。
8. 使用完成后调用 `POST /mailboxes/:mailboxId/release` 释放邮箱。

伪代码：

```text
domains = GET /domains
domainId = choose(domains.items)

mailbox = POST /mailboxes { domainId, expiresInHours: 24 }
submit mailbox.address to target site

repeat until timeout:
  messages = GET /mailboxes/{mailbox.id}/messages
  if messages.items is not empty:
    message = messages.items[0]
    result = GET /mailboxes/{mailbox.id}/messages/{message.id}/extractions
    if result.items is not empty:
      return result.items[0].value
    detail = GET /mailboxes/{mailbox.id}/messages/{message.id}
    return parse(detail.textBody or detail.htmlBody or detail.subject)
  sleep 2-5 seconds

POST /mailboxes/{mailbox.id}/release
```

## 10. 常见错误排查

| 状态码 | 常见原因 | 处理方式 |
| --- | --- | --- |
| `400` | JSON 字段名错误、类型错误、`expiresInHours` 为 0、`localPart` 不合法 | 检查请求体是否使用 camelCase，数值是否为正整数 |
| `401` | 未传 Bearer Token、Token/API Key 无效或过期 | 检查 `Authorization` 请求头 |
| `403` | API Key 缺少 scope，或域名绑定策略不允许访问 | 检查 scopes 和 domainBindings |
| `404` | `domainId`、`mailboxId` 或 `messageId` 不存在/不可访问 | 重新查询域名、邮箱或邮件列表 |
| `429` | 请求过快触发限流 | 降低轮询频率，增加指数退避 |
| `5xx` | 服务端、存储或入站处理异常 | 查看服务端日志和健康检查 |

## 11. 关键接口速查

| 方法 | 路径 | 用途 | 推荐 scope |
| --- | --- | --- | --- |
| `GET` | `/domains` | 获取可用域名 | `domains.read` |
| `GET` | `/dashboard` | 获取域名和邮箱概览 | `domains.read`, `mailboxes.read` |
| `GET` | `/mailboxes` | 获取邮箱列表 | `mailboxes.read` |
| `POST` | `/mailboxes` | 创建邮箱 | `mailboxes.write` |
| `POST` | `/mailboxes/:mailboxId/extend` | 续期邮箱 | `mailboxes.write` |
| `POST` | `/mailboxes/:mailboxId/release` | 释放邮箱 | `mailboxes.write` |
| `GET` | `/mailboxes/:mailboxId/messages` | 获取邮件列表 | `messages.read` |
| `GET` | `/mailboxes/:mailboxId/messages/:id` | 获取邮件详情 | `messages.read` |
| `GET` | `/mailboxes/:mailboxId/messages/:id/extractions` | 获取验证码/链接等结构化提取结果 | `messages.read` |
| `GET` | `/mailboxes/:mailboxId/messages/:id/raw/parsed` | 获取解析后的原始邮件 | `messages.read` |
| `GET` | `/mailboxes/:mailboxId/messages/:id/raw` | 下载原始 EML | `messages.read` |
| `GET` | `/mailboxes/:mailboxId/messages/:id/attachments/:index` | 下载附件 | `messages.attachments.read` |
| `POST` | `/mailboxes/:mailboxId/messages/receive` | 注入测试邮件 | `messages.write` |
