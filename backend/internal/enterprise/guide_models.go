package enterprise

import (
	"context"
	"database/sql"
	"errors"
)

// GetEmployeeGuideGroupID 与网关的企业归属校验使用相同的有效订阅和分组约束。
func (r *Repository) GetEmployeeGuideGroupID(ctx context.Context, enterpriseID, employeeID int64) (*int64, error) {
	var groupID int64
	err := r.db.QueryRowContext(ctx, `
		SELECT assignment.upstream_group_id
		FROM enterprise_key_assignments AS assignment
		JOIN enterprises AS enterprise
		  ON enterprise.id = assignment.enterprise_id AND enterprise.status = 'active'
		JOIN enterprise_employees AS employee
		  ON employee.id = assignment.employee_id AND employee.enterprise_id = enterprise.id
		 AND employee.status = 'active'
		JOIN api_keys AS api_key
		  ON api_key.id = assignment.api_key_id
		 AND api_key.user_id = enterprise.dedicated_upstream_user_id
		 AND api_key.group_id = assignment.upstream_group_id
		 AND api_key.deleted_at IS NULL AND api_key.status = 'active'
		 AND (api_key.expires_at IS NULL OR api_key.expires_at > CURRENT_TIMESTAMP)
		 AND (api_key.quota <= 0 OR api_key.quota_used < api_key.quota)
		JOIN enterprise_subscriptions AS enterprise_subscription
		  ON enterprise_subscription.enterprise_id = enterprise.id
		 AND enterprise_subscription.upstream_user_subscription_id = assignment.upstream_user_subscription_id
		 AND enterprise_subscription.status = 'active'
		 AND enterprise_subscription.activated_at IS NOT NULL AND enterprise_subscription.ended_at IS NULL
		JOIN user_subscriptions AS upstream_subscription
		  ON upstream_subscription.id = assignment.upstream_user_subscription_id
		 AND upstream_subscription.user_id = api_key.user_id
		 AND upstream_subscription.group_id = assignment.upstream_group_id
		 AND upstream_subscription.deleted_at IS NULL AND upstream_subscription.status = 'active'
		 AND upstream_subscription.starts_at <= CURRENT_TIMESTAMP
		 AND upstream_subscription.expires_at > CURRENT_TIMESTAMP
		WHERE assignment.enterprise_id = $1 AND assignment.employee_id = $2 AND assignment.status = 'active'
		ORDER BY assignment.generation DESC, assignment.id DESC LIMIT 1`, enterpriseID, employeeID).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &groupID, nil
}
