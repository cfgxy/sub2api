# Linux DO 与 NodeLoc 登录接入

本接入复用官方 Sub2API 的 Linux DO 与 Generic OIDC。SHAN-427 的源码基线是 `Wei-Shaw/sub2api/main` 的 `b8dece9000c68815a5b867ca5a1e6f236e173905`，交付仓为 `cfgxy/sub2api`；不包含企业模块。本单不新增 provider、认证模型或数据迁移。

## 原生能力与最小接线

- Linux DO 已有登录按钮、后端授权码交换、账号创建及绑定流程；配置 `linuxdo_connect` 即可。
- NodeLoc 使用 `oidc_connect`，将 `provider_name` 设为 `NodeLoc`，原生按钮与账号绑定页面显示该名称。后端路由继续使用 `oidc`。
- Linux DO 的身份键为 `linuxdo / linuxdo / id`；NodeLoc 复用 OIDC 的 `oidc / issuer / sub`。用户名、显示名和邮箱不是外部身份主键；不另造 `nodeloc` 数据类型。
- 同一外部身份重复登录复用既有用户；同邮箱走原生待完成的绑定选择，不静默合并。绑定已属于另一用户的身份返回冲突。
- 通用 OIDC 当前是一个配置槽；使用 NodeLoc 后，该槽不能同时配置第二个 OIDC issuer。不要覆盖已有生产 OIDC 设置。

调用关系：登录按钮 → `/api/v1/auth/oauth/{linuxdo|oidc}/start` → 后端加载配置（OIDC 在此按需拉取 Discovery）→ 第三方授权 → 后端 callback 校验 state、PKCE，OIDC 校验签名、issuer、audience、nonce 与 sub → 原生身份查询/待完成注册绑定 → 前端 callback。

源码锚点：`backend/internal/service/setting_oauth.go`、`backend/internal/handler/auth_linuxdo_oauth.go`、`backend/internal/handler/auth_oidc_oauth.go`、`backend/internal/handler/auth_oauth_pending_flow.go`、`frontend/src/views/auth/LoginView.vue`。

## 服务端配置

以 `deploy/config.example.yaml` 为完整模板。以下是待填配置，不是可以直接启用的真实凭据配置；保留 `enabled: false`，填写并验证全部字段后再启用。配置文件和 Secret 仅放后端，限制文件读权限，不提交 Git。

Linux DO：

```yaml
linuxdo_connect:
  enabled: false
  client_id: ""
  client_secret: ""
  authorize_url: "https://connect.linux.do/oauth2/authorize"
  token_url: "https://connect.linux.do/oauth2/token"
  userinfo_url: "https://connect.linux.do/api/user"
  scopes: "user"
  redirect_url: "https://your-domain.example/api/v1/auth/oauth/linuxdo/callback"
  frontend_redirect_url: "/auth/linuxdo/callback"
  token_auth_method: "client_secret_post"
  use_pkce: true
```

NodeLoc：

```yaml
oidc_connect:
  enabled: false
  provider_name: "NodeLoc"
  client_id: ""
  client_secret: ""
  issuer_url: "" # 填入已核实 Discovery 返回的 issuer，必须精确匹配
  discovery_url: "" # 填入实际返回 OIDC JSON 的候选地址
  authorize_url: "" # 留空，由后端 Discovery 发现
  token_url: ""
  userinfo_url: ""
  jwks_url: ""
  scopes: "openid profile"
  redirect_url: "https://your-domain.example/api/v1/auth/oauth/oidc/callback"
  frontend_redirect_url: "/auth/oidc/callback"
  token_auth_method: "client_secret_post" # 必须与应用和 metadata 支持的方法一致
  use_pkce: true
  validate_id_token: true
  allowed_signing_algs: "RS256,ES256,PS256" # 与 metadata 的算法支持核对
  clock_skew_seconds: 120
  require_email_verified: false
```

NodeLoc 应用申请入口：`https://www.nodeloc.com/oauth-provider/applications`。候选 Discovery 地址：

- `https://www.nodeloc.com/.well-known/openid-configuration`
- `https://www.nodeloc.com/oauth-provider/.well-known/openid-configuration`

2026年10月04日本地只读请求两个地址均返回 HTTP 403，未获得 metadata。因此不能指定已验证的 NodeLoc issuer、端点、算法、PKCE 支持或 Token 认证方法。这里的地址源于本单 Owner 接入资料，受控 mock 仅验证两种路径的后端解析，不证明真实协议已兼容。不得改用旧 `conn.nodeloc.cc` 端点或关闭签名校验来代替取证。

启用前在获授权的目标环境读取真实 JSON，核对 issuer、授权/Token/UserInfo/JWKS 地址、`openid profile`、PKCE S256、算法和 Token 认证方法。若缺少 PKCE 或签名校验所需字段，交由维护者核对应用与协议，不直接弱化配置。需要邮箱时再确认支持并追加 `email` scope。

管理员数据库设置可覆盖配置文件。若界面表现与文件不同，核对原生管理员认证设置，禁止仅修改 YAML 后推定生效。缺凭据保持禁用；启用却漏填凭据时后端报配置错误。原有邮箱、密码及其他登录设置不变。

## callback 登记清单

| 使用场景 | Linux DO 应用登记 URI | NodeLoc 应用登记 URI |
| --- | --- | --- |
| 本单 dev1 mock | `http://127.0.0.1:18011/api/v1/auth/oauth/linuxdo/callback` | `http://127.0.0.1:18011/api/v1/auth/oauth/oidc/callback` |
| 获授权测试站 | `https://<测试域名>/api/v1/auth/oauth/linuxdo/callback` | `https://<测试域名>/api/v1/auth/oauth/oidc/callback` |
| 正式站 | `https://<正式域名>/api/v1/auth/oauth/linuxdo/callback` | `https://<正式域名>/api/v1/auth/oauth/oidc/callback` |

应用登记必须与后端 `redirect_url` 完全一致。`/auth/linuxdo/callback` 与 `/auth/oidc/callback` 是前端完成页面，不是应用登记地址。loopback 地址只用于本地受控 mock；真实第三方是否接受须由应用登记验证。

目前还需要每个平台的 Client ID、Client Secret、获认可的 callback 域名，以及 NodeLoc 可读 Discovery 文档。Secret 不应经评论或附件传递。

## 自测与独立验收

本单没有认证行为修改；增加的是配置指引和现有行为的回归测试，未构造用于虚假 Red 的失败断言。先运行官方已有认证测试，再补以下受控场景：

- `setting_service_nodeloc_test.go`：显式主站/子路径 Discovery、服务端 HTTP 请求、发现端点、保留安全配置、不可用/缺字段拒绝、无凭据默认禁用。
- `auth_oauth_provider_errors_test.go`：两 provider 取消授权、缺 code、无效 state、Token 被拒绝、UserInfo 不可用；校验 callback 错误码和账户、身份、待完成会话数量不增加。
- 沿用 `auth_linuxdo_oauth_test.go`：id 解析、新用户登录、重复身份、绑定与所有权冲突。
- 沿用 `auth_oidc_oauth_test.go`：签名与 nonce 校验、issuer/sub 身份、重复身份、账号绑定和冲突。
- 沿用 `auth_oauth_pending_flow_test.go`：已有邮箱选择、身份冲突、注册失败回滚与完成绑定。

```bash
go -C backend test -tags=unit ./internal/service ./internal/handler ./internal/config ./internal/pkg/oauth -count=1
pnpm --dir frontend test:run src/views/auth/__tests__/LoginView.spec.ts src/views/auth/__tests__/OAuthCallbackView.spec.ts src/components/auth/__tests__/OAuthLoginSections.spec.ts
```

本地 dev 禁用态只读检查（不输出任何凭据）：

```bash
curl --fail --silent http://127.0.0.1:18011/health
curl --silent http://127.0.0.1:18011/api/v1/auth/oauth/linuxdo/start
curl --silent http://127.0.0.1:18011/api/v1/auth/oauth/oidc/start
```

本单不需要准备或清理业务 SQL。单测使用独立 SQLite fixture 并随测试关闭；槽位容器经 `worktree.sh dev1 down` 回收，随后 `lock-release SHAN-427` 释放租约。配置回退为禁用两个 provider；源码回退使用上述官方 base，经环境入口 `use --sha` 加载。不得更改 QA、legacy 或正式站。

Leader Review 后，QA 必须独立加载本单新提交与产物，重新验证按钮、mock 成功/错误、禁用态和原登录回归。取得合法凭据后再执行双平台真实授权；mock 通过不代表真实授权已通过。
