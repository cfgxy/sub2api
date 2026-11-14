# OAuth Refresh 入口清单

本清单对应 `TOKEN_REFRESH_ENABLED` 的启动时 gate。上游 OAuth refresh 指 `refresh_token` grant；授权码 exchange、SSO 转换和本系统用户 session refresh 不属于该开关。

## 已覆盖

| 类型 | 入口 | 统一边界 | 关闭时行为 |
| --- | --- | --- | --- |
| 协调器 | `OAuthRefreshAPI.RefreshIfNeeded` | 所有 provider 请求前、后台周期刷新、账号测试、模型同步、配额探测等间接调用 | 在本地锁、Redis 锁、DB 读取和 executor 前返回 `OAUTH_TOKEN_REFRESH_DISABLED` |
| 后台 | `TokenRefreshService.Start` / `refreshLoop` | `RefreshIfNeeded` | 不启动后台首轮和周期刷新 |
| 直接出口 | `OAuthService.RefreshToken` | Claude OAuth token endpoint | gate 前置，不调用 client |
| 直接出口 | `OpenAIOAuthService.RefreshTokenWithClientID` | OpenAI OAuth token endpoint，包括裸 token 管理接口 | gate 前置，不调用 client |
| 直接出口 | `GeminiOAuthService.RefreshToken` | Gemini OAuth token endpoint及兼容回退 | gate 前置，不进入重试循环 |
| 直接出口 | `AntigravityOAuthService.RefreshToken` | Google/Antigravity OAuth token endpoint | gate 前置，不创建 client |
| 直接出口 | `GrokOAuthService.RefreshToken` | xAI/Grok OAuth token endpoint，包括裸 token、reconciliation 和账号刷新 | gate 前置，不调用 client |
| 管理端 | `AccountHandler.refreshSingleAccount`、批量刷新、受限管理密钥 | 调用上述 service 或 `OAuthRefreshAPI` | 由同一 gate 阻断，不写账号凭据 |
| CRS | `CRSSyncService.refreshOAuthToken` | 调用 Claude/OpenAI/Gemini service | 由实际 OAuth service gate 阻断 |
| 请求前 | `*_token_provider.GetAccessToken` | `OAuthRefreshAPI.RefreshIfNeeded` | 不获取锁、不重读、不刷新、不写状态 |
| 诊断/探测 | `AccountTestService`、`upstream_models`、`GrokQuotaService`、`OpenAIQuotaService` | provider/token provider | 只会得到 gate 错误，不调用 OAuth token endpoint |
| reconciliation | `GrokOAuthReconciliation` | 账号刷新 service / refresh API | 由实际 OAuth service 或协调器 gate 阻断 |

## 明确排除

- `/api/v1/auth/refresh`：本系统用户 JWT/session refresh，走 `AuthService.RefreshToken`，不引用 `OAuthRefreshGate`。
- `/api/v1/enterprise/auth/refresh`：企业身份 session refresh，不引用 `OAuthRefreshGate`。
- OAuth authorization-code exchange、Claude setup-token exchange、Grok SSO 转换：不是 `refresh_token` grant，不由本开关阻断。

## 静态核对规则

生产代码中真正触发上游 refresh token endpoint 的边界只能是上述五个 service 方法；其余 `RefreshAccountToken`、`RefreshIfNeeded` 和 provider 调用必须经 service gate 或协调器 gate。新增 refresh endpoint 时必须同步更新本清单和 gate 测试。

## 未知与剩余风险

- 本阶段未连接真实双栈部署、上游 OAuth endpoint、数据库或 Redis，未验证线上环境变量透传和实际封禁/503 风险。
- `TOKEN_REFRESH_ENABLED` 是启动时快照，修改环境变量后必须重启服务；运行中热切换不在本阶段范围内。
- Redis 不可用时既有协调器仍按原逻辑降级为进程内锁；本阶段只保证 gate 关闭时在任何锁和 DB 操作前返回，不改变既有并发治理语义。
