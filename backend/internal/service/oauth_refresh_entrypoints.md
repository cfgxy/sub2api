# OAuth Refresh 入口清单

本清单对应 `TOKEN_REFRESH_ENABLED` 的启动时 gate。上游 OAuth refresh 指 `refresh_token` grant；授权码 exchange、SSO 转换和本系统用户 session refresh 不属于该开关。

## 已覆盖

| 类型 | 入口 | 统一边界 | 关闭时行为 | 证据层级 |
| --- | --- | --- | --- | --- |
| 协调器 | `OAuthRefreshAPI.RefreshIfNeeded` | 所有 provider 请求前、后台周期刷新、账号测试、模型同步、配额探测等间接调用 | 在本地锁、Redis 锁、DB 读取和 executor 前返回 `OAUTH_TOKEN_REFRESH_DISABLED` | 自动化 service 测试 |
| 后台 | `TokenRefreshService.Start` / `refreshLoop` | `RefreshIfNeeded` | 不启动后台首轮和周期刷新 | 自动化 service 测试 + 生产 provider 注入测试 |
| 直接出口 | `OAuthService.RefreshToken` | Claude OAuth token endpoint | gate 前置，不调用 client | 自动化 service 测试 |
| 直接出口 | `OpenAIOAuthService.RefreshTokenWithClientID` | OpenAI OAuth token endpoint，包括裸 token 管理接口 | gate 前置，不调用 client | 自动化 service 测试 + OpenAI handler HTTP 测试 |
| 直接出口 | `GeminiOAuthService.RefreshToken` | Gemini OAuth token endpoint及兼容回退 | gate 前置，不进入重试循环 | 自动化 service 测试 |
| 直接出口 | `AntigravityOAuthService.RefreshToken` | Google/Antigravity OAuth token endpoint | gate 前置，不创建 client | 自动化 service 测试 |
| 直接出口 | `GrokOAuthService.RefreshToken` | xAI/Grok OAuth token endpoint，包括裸 token、reconciliation 和账号刷新 | gate 前置，不调用 client | 自动化 service 测试 + Grok handler HTTP 测试 |
| 管理端 | `AccountHandler.refreshSingleAccount`、批量刷新、受限管理密钥 | 调用上述 service 或 `OAuthRefreshAPI` | 由同一 gate 阻断，不写账号凭据 | 自动化单账号/批量/受限密钥 handler 测试 |
| CRS | `CRSSyncService.refreshOAuthToken` | 调用 Claude/OpenAI/Gemini service | 由实际 OAuth service gate 阻断 | 静态调用链 + service 边界自动化；无独立 CRS HTTP 测试 |
| 请求前 | `*_token_provider.GetAccessToken` | `OAuthRefreshAPI.RefreshIfNeeded` | 不获取锁、不重读、不刷新、不写状态 | 自动化 provider 测试 |
| 诊断/探测 | `AccountTestService`、`upstream_models`、`GrokQuotaService`、`OpenAIQuotaService` | provider/token provider | 只会得到 gate 错误，不调用 OAuth token endpoint | 静态调用链 + 协调器/provider 边界自动化 |
| reconciliation | `GrokOAuthReconciliation` | 账号刷新 service / refresh API | 由实际 OAuth service 或协调器 gate 阻断 | 自动化 reconciliation/service 边界测试 |

## 明确排除

- `/api/v1/auth/refresh`：本系统用户 JWT/session refresh，走 `AuthService.RefreshToken`，不引用 `OAuthRefreshGate`。
- `/api/v1/enterprise/auth/refresh`：企业身份 session refresh，不引用 `OAuthRefreshGate`。
- OAuth authorization-code exchange、Claude setup-token exchange、Grok SSO 转换：不是 `refresh_token` grant，不由本开关阻断。

## 静态核对规则

生产代码中真正触发上游 refresh token endpoint 的边界只能是上述五个 service 方法；其余 `RefreshAccountToken`、`RefreshIfNeeded` 和 provider 调用必须经 service gate 或协调器 gate。新增 refresh endpoint 时必须同步更新本清单和 gate 测试。

## 自动化证据索引

- `internal/service/oauth_refresh_gate_test.go`：配置加载、生产 provider 注入、统一协调器、五类直接 service、请求前 provider 和 JWT/session service 边界。
- `internal/handler/admin/oauth_refresh_gate_handler_test.go`：管理员单账号、批量、OpenAI 专用 endpoint、Grok 专用 endpoint 和受限管理密钥路径；spy client 断言 token endpoint 调用次数为零，账号凭据和 `UpdateAccount` 调用不变。
- `internal/server/routes/auth_refresh_contract_test.go`：真实 `RegisterAuthRoutes` 注册的 `/api/v1/auth/refresh`，在关闭上游 gate 的配置下完成 session refresh 和 refresh-token rotation。

## 未知与剩余风险

- 本阶段未连接真实双栈部署、上游 OAuth endpoint、数据库或 Redis，未验证线上环境变量透传和实际封禁/503 风险。
- `TOKEN_REFRESH_ENABLED` 是启动时快照，修改环境变量后必须重启服务；运行中热切换不在本阶段范围内。
- Redis 不可用时既有协调器仍按原逻辑降级为进程内锁；本阶段只保证 gate 关闭时在任何锁和 DB 操作前返回，不改变既有并发治理语义。
- CRS、账号测试、模型同步/上游探测和 Grok/OpenAI 配额诊断尚未分别建立真实 handler/外部依赖集成测试；当前证据是静态调用链与统一 service/provider 边界自动化。
