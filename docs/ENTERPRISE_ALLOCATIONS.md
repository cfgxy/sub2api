# 企业员工积分分配

企业积分分配接口用于按 Sub2API 原生订阅窗口配置员工积分目标并查询实际使用量。该能力仅用于管理与报表，不改变原生订阅限额执行，不修改 group，不预留或预扣积分，也不阻断员工请求。

## 窗口契约

`window_type` 仅接受以下值：

- `day`：`window_anchor` 对应 `user_subscriptions.daily_window_start`。
- `week`：`window_anchor` 对应 `user_subscriptions.weekly_window_start`。
- `month`：`window_anchor` 对应 `user_subscriptions.monthly_window_start`。

服务端不根据当前时间推导 anchor。当前窗口必须使用对应 `user_subscriptions` 的权威 anchor；历史窗口仅在同一企业、订阅和 `window_type` 下已存在 allocation 或 attribution 时可查询。未来或伪造 anchor 返回 `ErrInvalidWindowAnchor`。

## 配置积分

`PUT /api/v1/enterprise/subscriptions/{subscription_id}/allocations/{employee_id}`

```json
{
  "enterprise_id": 9,
  "window_type": "day",
  "window_anchor": "2026-09-08T00:00:00Z",
  "credit": "25.00000000",
  "expected_version": 3,
  "reason": "调整员工积分"
}
```

`credit` 必须是非负十进制数，`reason` 必填。`expected_version` 用于同一员工 allocation 的 compare-and-swap。管理员未修改配置时，最近一次配置延续到后续窗口；新窗口 usage 从 `0` 开始，历史窗口仍可查询。

员工积分合计允许超过原生订阅限额，单个员工配置不会因合计超限被拒绝。只有原生 `HasDailyLimit`、`HasWeeklyLimit`、`HasMonthlyLimit` 语义判定为启用，即对应 limit `> 0` 时，响应才返回 `authoritative_limit` 并计算 overallocated warning；否则 `authoritative_limit` 为 `null`。

## 查询使用量

`GET /api/v1/enterprise/subscriptions/{subscription_id}/allocations/{employee_id}?enterprise_id=9&window_type=day&window_anchor=2026-09-08T00:00:00Z`

```json
{
  "configured_credit": "25.00000000",
  "usage_credit": "27.00000000",
  "remaining_credit": "0.00000000",
  "overage_credit": "2.00000000",
  "allocated_total": "110.00000000",
  "authoritative_limit": "100.00000000",
  "overallocated_by": "10.00000000",
  "warning": "allocated credit exceeds the authoritative subscription limit"
}
```

`remaining_credit = max(configured_credit - usage_credit, 0)`，`overage_credit = max(usage_credit - configured_credit, 0)`。

## 使用归属与兼容边界

Sub2API 继续通过既有 `actual_cost` 字段记录并执行原生订阅用量；企业报表直接将该数值作为积分使用量，不改变原生协议。

生产链路以请求时冻结的 `PricingAt` 作为 `request_at`，并分别保存请求时非 nil 的 day/week/month anchor。延迟结算仍归入请求发生时的旧窗口。API Key 在请求时存在有效 employee assignment 时写入 `classification=employee` 和对应 generation；企业 dedicated user 的订阅已匹配但当时没有有效 assignment 时写入 `classification=controlled_external`、`employee_id=NULL`、`assignment_generation=0`。普通非企业 usage 不生成企业归属。

migration 237 对既有 weekly attribution 兼容迁移为 `window_type=week`。历史记录无法重建真实 `request_at` 时，仅在该次历史迁移中使用关联 `usage_logs.created_at` 回填；迁移完成后的新记录严格使用 `PricingAt`，生产写入不得使用该 fallback。历史 employee 记录无法可靠匹配 generation 时使用 `assignment_generation=0` 表示代次未知，不据此改写 employee 归属。

部署必须先完成 migration 236 和 237，再启动包含 API Key 企业归属标志及 auth snapshot v24 的服务版本。migration 237 会回填并通过数据库触发器维护 `api_keys.enterprise_attribution_candidate`；普通请求的订阅加载和 usage 写入不会查询企业表，只有标志为 true 的企业 Key 才在 usage 事务中解析归属。服务不在缺表时降级猜测归属。
