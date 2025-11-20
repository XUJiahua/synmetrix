# 权限控制策略建议 - 快速参考

> 详细文档请参阅 [SECURITY_GUIDE.md](SECURITY_GUIDE.md)

## 🔒 核心安全原则

```
默认拒绝 + 多层防御 + 最小权限 + 可审计
```

## 📊 安全层级

| 层级 | 策略 | 难度 | 必要性 | 防护能力 |
|-----|------|------|--------|---------|
| 1️⃣ | 网络隔离 | ⭐ | 必须 | 基础 |
| 2️⃣ | Action Secret | ⭐⭐ | 必须 | 中等 |
| 3️⃣ | Session Variables 验证 | ⭐⭐ | 必须 | 中等 |
| 4️⃣ | JWT 独立验证 | ⭐⭐⭐ | 推荐 | 高 |
| 5️⃣ | 资源权限控制 | ⭐⭐⭐ | 推荐 | 高 |
| 6️⃣ | Rate Limiting | ⭐⭐ | 推荐 | 中等 |

## 🎯 针对不同场景的策略

### 场景 1：用户自己的数据（ensure_user）

**特征**：用户只能访问/修改自己的数据

**策略**：
```yaml
Hasura Permissions: role: user
安全层级: 1️⃣ + 2️⃣ + 3️⃣
```

**代码模式**：
```go
// ✅ 正确：userID 来自 JWT，不可伪造
userID := req.SessionVariables["x-hasura-user-id"]
h.service.EnsureUserExists(ctx, userID)
```

**风险**：低 - Hasura 已验证 JWT，session_variables 可信

---

### 场景 2：管理员操作（delete_user, manage_team）

**特征**：需要特定角色才能执行

**策略**：
```yaml
Hasura Permissions: role: admin
安全层级: 1️⃣ + 2️⃣ + 3️⃣ + 审计日志
```

**代码模式**：
```go
// ✅ 正确：验证角色
role := req.SessionVariables["x-hasura-role"]
if role != "admin" {
    return ErrForbidden
}

// ✅ 记录审计日志
h.logger.Infof("Admin %s deleted user %s", operatorID, targetID)
```

**风险**：中 - 需要验证角色，记录操作

---

### 场景 3：跨用户访问（send_notification, share_resource）

**特征**：用户 A 操作用户 B 的资源，需要验证关系

**策略**：
```yaml
Hasura Permissions: role: user
安全层级: 1️⃣ + 2️⃣ + 3️⃣ + 5️⃣ + 6️⃣
```

**代码模式**：
```go
// ✅ 正确：验证关系（如同一团队）
senderID := req.SessionVariables["x-hasura-user-id"]
receiverID := req.Input["receiver_id"]

inSameTeam, err := h.service.CheckSameTeam(ctx, senderID, receiverID)
if !inSameTeam {
    return ErrForbidden
}

// ✅ 限流防滥用
if !h.rateLimiter.Allow(senderID) {
    return ErrRateLimitExceeded
}
```

**风险**：高 - 容易出现越权漏洞

---

### 场景 4：敏感操作（change_password, delete_account）

**特征**：高风险操作，需要额外验证

**策略**：
```yaml
Hasura Permissions: role: user
安全层级: 1️⃣ + 2️⃣ + 3️⃣ + 4️⃣ + 审计 + 通知
```

**代码模式**：
```go
// ✅ 独立验证 JWT（不信任 session_variables）
claims, err := h.jwtValidator.Validate(c.GetHeader("Authorization"))
if err != nil {
    return ErrUnauthorized
}

// ✅ 额外验证（原密码/2FA）
if !h.service.VerifyPassword(ctx, userID, currentPassword) {
    return ErrInvalidPassword
}

// ✅ 审计日志
h.auditLogger.Log(AuditEvent{
    Type:   "PASSWORD_CHANGE",
    UserID: userID,
    IP:     c.ClientIP(),
})

// ✅ 发送通知
h.notificationService.SendPasswordChangeEmail(userID)
```

**风险**：极高 - 需要最高级别防护

---

## 🚀 快速实施指南

### 阶段 1：最小安全配置（立即实施）

**耗时**：~30 分钟

```bash
# 1. 生成 Action Secret
ACTION_SECRET=$(openssl rand -hex 32)
echo "ACTION_SECRET=$ACTION_SECRET" >> .env

# 2. 配置 Hasura metadata
cat <<EOF >> services/hasura/metadata/actions.yaml
- name: ensure_user_exists
  definition:
    headers:
      - name: x-action-secret
        value_from_env: ACTION_SECRET
EOF

# 3. 应用配置
hasura metadata apply
```

**代码修改**：
```go
// cmd/serve.go
import "go-actions/internal/middleware"

router.Use(middleware.SecurityMiddleware(log))
```

✅ **完成后**：防止直接调用 actions 服务

---

### 阶段 2：生产环境准备（部署前）

**耗时**：~2 小时

**清单**：
- [ ] 确认网络隔离（go-actions 不暴露到公网）
- [ ] 启用 HTTPS/TLS
- [ ] 配置监控和日志
- [ ] 测试所有 actions 的权限配置

✅ **完成后**：满足生产环境基本要求

---

### 阶段 3：高安全场景（可选）

**耗时**：~1 天

**清单**：
- [ ] 实现 JWT 独立验证
- [ ] 添加 Rate Limiting
- [ ] 实施审计日志系统
- [ ] 配置告警

✅ **完成后**：满足高安全/合规要求

---

## ⚠️ 常见安全陷阱

### ❌ 陷阱 1：盲目信任 session_variables

```go
// ❌ 错误：如果 Hasura 被绕过，session_variables 可以伪造
userID := req.SessionVariables["x-hasura-user-id"]
// 直接使用 userID 操作敏感数据
```

**解决**：对敏感操作启用 JWT 独立验证

---

### ❌ 陷阱 2：忘记验证资源所有权

```go
// ❌ 错误：用户 A 可以删除用户 B 的数据
func DeleteResource(resourceID string) {
    db.Delete("resources", resourceID)
}
```

**解决**：
```go
// ✅ 正确：验证所有权
func DeleteResource(userID, resourceID string) error {
    resource := db.FindResource(resourceID)
    if resource.OwnerID != userID {
        return ErrForbidden
    }
    db.Delete("resources", resourceID)
}
```

---

### ❌ 陷阱 3：没有配置 Action Secret

```go
// ❌ 危险：任何人都可以直接调用
curl http://your-server:3000/rpc/delete_user \
  -d '{"session_variables":{"x-hasura-user-id":"fake-admin"}}'
```

**解决**：必须配置 Action Secret（阶段 1）

---

### ❌ 陷阱 4：过于宽松的 Hasura 权限

```yaml
# ❌ 错误：所有人都可以调用管理员操作
- name: delete_all_users
  permissions:
    - role: user  # 太宽松！
```

**解决**：
```yaml
# ✅ 正确：只允许管理员
- name: delete_all_users
  permissions:
    - role: admin
```

---

## 📋 安全检查清单

### 开发阶段
- [ ] 配置了 ACTION_SECRET
- [ ] 所有 handlers 验证 session_variables
- [ ] 敏感操作有额外验证
- [ ] 有基本的错误日志

### 部署前
- [ ] 网络隔离已配置
- [ ] HTTPS/TLS 已启用
- [ ] Hasura Actions 权限已正确配置
- [ ] 所有环境变量已设置
- [ ] 测试了不同角色的访问

### 生产环境
- [ ] 监控和告警已配置
- [ ] 审计日志系统运行中
- [ ] 定期审查访问日志
- [ ] 有应急响应计划

---

## 🔗 相关文档

- **[SECURITY_GUIDE.md](SECURITY_GUIDE.md)** - 完整的安全策略指南
- **[TODO.md](TODO.md)** - Hasura Actions 规范和初步安全建议
- **[ADDING_NEW_ACTION.md](ADDING_NEW_ACTION.md)** - 添加新 action 时的安全注意事项

---

## 💡 关键要点

1. **永远不要只依赖一层防护** - 使用多层安全策略
2. **最小权限原则** - 只授予必要的权限
3. **验证，然后再验证** - 对敏感操作进行额外验证
4. **记录一切** - 审计日志是追踪问题的关键
5. **默认拒绝** - 明确允许的才放行

## 🆘 快速问答

**Q: 最低要求是什么？**
A: 网络隔离 + Action Secret + Session Variables 验证

**Q: 何时需要 JWT 独立验证？**
A: 涉及密码、账户删除等敏感操作时

**Q: 如何判断是否需要资源权限控制？**
A: 如果操作涉及其他用户的资源，就需要

**Q: Rate Limiting 必须吗？**
A: 生产环境强烈推荐，防止滥用和 DoS

**Q: 如何测试安全配置？**
A: 尝试绕过每一层防护，看是否能成功攻击
