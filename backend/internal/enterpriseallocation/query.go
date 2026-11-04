package enterpriseallocation

import "fmt"

// EffectiveWeeklyAllocationQuery 返回目标订阅周的权威额度查询。
// 配置会继承至下一条锚点配置生效，同一锚点的并发编辑由 version 决定。
// 参数是调用方控制的 SQL 表达式，不接受用户输入。
func EffectiveWeeklyAllocationQuery(enterpriseID, subscriptionID, employeeID, targetAnchor string) string {
	return fmt.Sprintf(`
		SELECT amount
		FROM enterprise_weekly_allocations
		WHERE enterprise_id = %s AND subscription_id = %s AND employee_id = %s
		  AND window_type = 'week' AND window_anchor <= %s
		ORDER BY window_anchor DESC, version DESC
		LIMIT 1`, enterpriseID, subscriptionID, employeeID, targetAnchor)
}

// LockedEffectiveWeeklyAllocationQuery 用于 Key 生命周期和预留路径的事务锁定读取。
func LockedEffectiveWeeklyAllocationQuery(enterpriseID, subscriptionID, employeeID, targetAnchor string) string {
	return EffectiveWeeklyAllocationQuery(enterpriseID, subscriptionID, employeeID, targetAnchor) + ` FOR UPDATE`
}
