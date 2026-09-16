package enterpriseidentity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const (
	workbenchDefaultPage     = 1
	workbenchDefaultPageSize = 20
	workbenchMaxPageSize     = 100
)

type WorkbenchHandler struct {
	owner *Handler
}

func NewWorkbenchHandler(owner *Handler) *WorkbenchHandler {
	return &WorkbenchHandler{owner: owner}
}

type workbenchQuery struct {
	DepartmentID *int64
	EmployeeID   *int64
	APIKeyID     *int64
	Model        string
	WindowType   string
	WindowAnchor *time.Time
	StartAt      *time.Time
	EndAt        *time.Time
	Page         int
	PageSize     int
}

type WorkbenchEmployeeSummary struct {
	EmployeeID       int64  `json:"employee_id"`
	Email            string `json:"email"`
	DepartmentID     *int64 `json:"department_id,omitempty"`
	Requests         int64  `json:"requests"`
	ConfiguredCredit string `json:"configured_credit"`
	UsageCredit      string `json:"usage_credit"`
	RemainingCredit  string `json:"remaining_credit"`
	OverageCredit    string `json:"overage_credit"`
	Recommendation   string `json:"recommendation"`
}

type WorkbenchUsageTrendPoint struct {
	At          time.Time `json:"at"`
	Requests    int64     `json:"requests"`
	UsageCredit string    `json:"usage_credit"`
}

type WorkbenchSummary struct {
	TotalUsageCredit           string                     `json:"total_usage_credit"`
	TotalRequests              int64                      `json:"total_requests"`
	EmployeeCount              int64                      `json:"employee_count"`
	ActiveEmployeeCount        int64                      `json:"active_employee_count"`
	SubscriptionID             *int64                     `json:"subscription_id,omitempty"`
	SubscriptionStatus         string                     `json:"subscription_status"`
	SubscriptionPlan           string                     `json:"subscription_plan"`
	EnterprisePoolLimit        string                     `json:"enterprise_pool_limit"`
	EnterprisePoolUsed         string                     `json:"enterprise_pool_used"`
	EnterprisePoolRemaining    string                     `json:"enterprise_pool_remaining"`
	EnterprisePoolExhausted    bool                       `json:"enterprise_pool_exhausted"`
	PoolSourceStatus           string                     `json:"pool_source_status"`
	PoolSource                 string                     `json:"pool_source"`
	PoolWindowType             string                     `json:"pool_window_type"`
	PoolWindowAnchor           *time.Time                 `json:"pool_window_anchor,omitempty"`
	PoolObservedAt             *time.Time                 `json:"pool_observed_at,omitempty"`
	ScheduledSubscriptionPlan  string                     `json:"scheduled_subscription_plan,omitempty"`
	ScheduledSubscriptionSince *time.Time                 `json:"scheduled_subscription_since,omitempty"`
	EmployeeSummaries          []WorkbenchEmployeeSummary `json:"employee_summaries"`
	UsageTrend                 []WorkbenchUsageTrendPoint `json:"usage_trend"`
}

type WorkbenchUsageRow struct {
	AttributionID        int64     `json:"attribution_id"`
	UsageLogID           int64     `json:"usage_log_id"`
	EmployeeID           *int64    `json:"employee_id,omitempty"`
	EmployeeEmail        string    `json:"employee_email,omitempty"`
	DepartmentID         *int64    `json:"department_id,omitempty"`
	APIKeyID             int64     `json:"api_key_id"`
	APIKeyMasked         string    `json:"api_key_masked"`
	Model                string    `json:"model,omitempty"`
	WindowType           string    `json:"window_type"`
	WindowAnchor         time.Time `json:"window_anchor"`
	RequestAt            time.Time `json:"request_at"`
	Classification       string    `json:"classification"`
	AssignmentGeneration int64     `json:"assignment_generation"`
	UsageCredit          string    `json:"usage_credit"`
	ConfiguredCredit     string    `json:"configured_credit"`
}

type WorkbenchAuditEvent struct {
	ID         int64          `json:"id"`
	EventType  string         `json:"event_type"`
	EntityType string         `json:"entity_type"`
	EntityID   *int64         `json:"entity_id,omitempty"`
	Result     string         `json:"result"`
	Reason     string         `json:"reason,omitempty"`
	Payload    map[string]any `json:"payload"`
	ActorRef   string         `json:"actor_ref"`
	CreatedAt  time.Time      `json:"created_at"`
}

func (h *WorkbenchHandler) registerRoutes(admin *gin.RouterGroup) {
	workbench := admin.Group("/workbench")
	workbench.GET("/summary", h.summary)
	workbench.GET("/usage", h.usage)
	workbench.GET("/audit-events", h.auditEvents)
	// 兼容直接以资源命名的客户端，保持同一认证和查询实现。
	admin.GET("/usage", h.usage)
	admin.GET("/audit-events", h.auditEvents)
}

func (h *WorkbenchHandler) summary(c *gin.Context) {
	claims := mustClaims(c)
	query, ok := parseWorkbenchQuery(c, false)
	if !ok {
		return
	}
	result, err := h.getSummary(c.Request.Context(), claims.EnterpriseID, query)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

func (h *WorkbenchHandler) usage(c *gin.Context) {
	claims := mustClaims(c)
	query, ok := parseWorkbenchQuery(c, true)
	if !ok {
		return
	}
	items, total, err := h.listUsage(c.Request.Context(), claims.EnterpriseID, query)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Paginated(c, items, total, query.Page, query.PageSize)
}

func (h *WorkbenchHandler) auditEvents(c *gin.Context) {
	claims := mustClaims(c)
	query, ok := parseWorkbenchQuery(c, true)
	if !ok {
		return
	}
	eventType := strings.TrimSpace(c.Query("event_type"))
	entityType := strings.TrimSpace(c.Query("entity_type"))
	actorRef := strings.TrimSpace(c.Query("actor_ref"))
	result := strings.TrimSpace(c.Query("result"))
	reason := strings.TrimSpace(c.Query("reason"))
	search := strings.TrimSpace(c.Query("search"))
	items, total, err := h.listAuditEvents(c.Request.Context(), claims.EnterpriseID, query, eventType, entityType, actorRef, result, reason, search)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Paginated(c, items, total, query.Page, query.PageSize)
}

func parseWorkbenchQuery(c *gin.Context, requirePage bool) (workbenchQuery, bool) {
	q := workbenchQuery{Page: workbenchDefaultPage, PageSize: workbenchDefaultPageSize}
	for name, target := range map[string]**int64{
		"department_id": &q.DepartmentID,
		"employee_id":   &q.EmployeeID,
		"api_key_id":    &q.APIKeyID,
	} {
		raw := strings.TrimSpace(c.Query(name))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			response.BadRequest(c, "invalid "+name)
			return workbenchQuery{}, false
		}
		*target = &value
	}

	q.Model = strings.TrimSpace(c.Query("model"))

	q.WindowType = strings.TrimSpace(c.Query("window_type"))
	if q.WindowType != "" && q.WindowType != "week" {
		response.BadRequest(c, "invalid window_type")
		return workbenchQuery{}, false
	}
	windowAnchor, ok := parseOptionalTime(c, "window_anchor")
	if !ok {
		return workbenchQuery{}, false
	}
	q.WindowAnchor = windowAnchor
	if q.WindowAnchor != nil && q.WindowType == "" {
		response.BadRequest(c, "window_type is required with window_anchor")
		return workbenchQuery{}, false
	}
	q.StartAt, ok = parseOptionalTime(c, "start_at")
	if !ok {
		return workbenchQuery{}, false
	}
	q.EndAt, ok = parseOptionalTime(c, "end_at")
	if !ok {
		return workbenchQuery{}, false
	}
	if q.StartAt != nil && q.EndAt != nil && !q.StartAt.Before(*q.EndAt) {
		response.BadRequest(c, "end_at must be after start_at")
		return workbenchQuery{}, false
	}
	if requirePage {
		var valid bool
		q.Page, valid = parsePositiveQuery(c, "page", workbenchDefaultPage)
		if !valid {
			return workbenchQuery{}, false
		}
		q.PageSize, valid = parsePositiveQuery(c, "page_size", workbenchDefaultPageSize)
		if !valid {
			return workbenchQuery{}, false
		}
		if q.PageSize > workbenchMaxPageSize {
			q.PageSize = workbenchMaxPageSize
		}
	}
	return q, true
}

func parseOptionalTime(c *gin.Context, name string) (*time.Time, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		response.BadRequest(c, "invalid "+name)
		return nil, false
	}
	return &value, true
}

func parsePositiveQuery(c *gin.Context, name string, fallback int) (int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		response.BadRequest(c, "invalid "+name)
		return fallback, false
	}
	return value, true
}

type workbenchSQLScope struct {
	where string
	args  []any
}

func buildWorkbenchUsageScope(enterpriseID int64, q workbenchQuery) workbenchSQLScope {
	conditions := []string{"attribution.enterprise_id = $1"}
	args := []any{enterpriseID}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if q.DepartmentID != nil {
		add("employee.department_id = $%d", *q.DepartmentID)
	}
	if q.EmployeeID != nil {
		add("attribution.employee_id = $%d", *q.EmployeeID)
	}
	if q.APIKeyID != nil {
		add("attribution.api_key_id = $%d", *q.APIKeyID)
	}
	if q.Model != "" {
		add("usage_log.model = $%d", q.Model)
	}
	if q.WindowType != "" {
		add("attribution.window_type = $%d", q.WindowType)
	} else {
		// 一条请求会落三条窗口快照；未指定窗口时用周快照作为唯一统计口径。
		conditions = append(conditions, "attribution.window_type = 'week'")
	}
	if q.WindowAnchor != nil {
		add("attribution.window_anchor = $%d", *q.WindowAnchor)
	}
	if q.StartAt != nil {
		add("attribution.request_at >= $%d", *q.StartAt)
	}
	if q.EndAt != nil {
		add("attribution.request_at < $%d", *q.EndAt)
	}
	return workbenchSQLScope{where: strings.Join(conditions, " AND "), args: args}
}

func (h *WorkbenchHandler) getSummary(ctx context.Context, enterpriseID int64, q workbenchQuery) (*WorkbenchSummary, error) {
	scope := buildWorkbenchUsageScope(enterpriseID, q)
	result := &WorkbenchSummary{
		TotalUsageCredit:        "0",
		EnterprisePoolLimit:     "0",
		EnterprisePoolUsed:      "0",
		EnterprisePoolRemaining: "0",
		PoolSourceStatus:        "unavailable",
		PoolSource:              "upstream subscription",
		PoolWindowType:          "week",
		EmployeeSummaries:       make([]WorkbenchEmployeeSummary, 0),
		UsageTrend:              make([]WorkbenchUsageTrendPoint, 0),
	}
	if err := h.owner.service.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(COALESCE(usage_log.actual_cost, 0)), 0)::text, COUNT(*)
		FROM enterprise_usage_attributions AS attribution
		LEFT JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
		LEFT JOIN enterprise_employees AS employee
		  ON employee.enterprise_id = attribution.enterprise_id AND employee.id = attribution.employee_id
		WHERE `+scope.where, scope.args...).Scan(&result.TotalUsageCredit, &result.TotalRequests); err != nil {
		return nil, err
	}
	activeArgs := []any{enterpriseID}
	activeWhere := "enterprise_id = $1 AND status = 'active'"
	if q.DepartmentID != nil {
		activeArgs = append(activeArgs, *q.DepartmentID)
		activeWhere += " AND department_id = $2"
	}
	if err := h.owner.service.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_employees WHERE `+activeWhere, activeArgs...).Scan(&result.ActiveEmployeeCount); err != nil {
		return nil, err
	}
	if err := h.owner.service.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT attribution.employee_id)
		FROM enterprise_usage_attributions AS attribution
		LEFT JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
		LEFT JOIN enterprise_employees AS employee
		  ON employee.enterprise_id = attribution.enterprise_id AND employee.id = attribution.employee_id
		WHERE `+scope.where+` AND attribution.employee_id IS NOT NULL`, scope.args...).Scan(&result.EmployeeCount); err != nil {
		return nil, err
	}

	rows, err := h.owner.service.db.QueryContext(ctx, buildEmployeeSummaryQuery(scope), scope.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item WorkbenchEmployeeSummary
		var departmentID sql.NullInt64
		if err := rows.Scan(&item.EmployeeID, &item.Email, &departmentID, &item.Requests, &item.ConfiguredCredit, &item.UsageCredit, &item.RemainingCredit, &item.OverageCredit); err != nil {
			return nil, err
		}
		item.DepartmentID = nullableInt64(departmentID)
		// overage 由数据库 NUMERIC 定长 ::text 输出，零值形态不固定（"0" 或 "0.00000000"），
		// 必须按数值语义判断；解析失败视为无法确认，保留人工核对提示。
		item.Recommendation = "当前 allocation 范围内"
		if overage, err := decimal.NewFromString(item.OverageCredit); err != nil || overage.IsPositive() {
			item.Recommendation = "核对个人超用，并按业务需要调整 allocation"
		}
		result.EmployeeSummaries = append(result.EmployeeSummaries, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var poolStatus, plan, subscriptionStatus sql.NullString
	var poolLimit, poolUsed, poolRemaining sql.NullString
	var poolExhausted sql.NullBool
	var poolAnchor sql.NullTime
	var subscriptionID sql.NullInt64
	if err := h.owner.service.db.QueryRowContext(ctx, `
		SELECT enterprise_subscription.id,
		       CASE WHEN enterprise_subscription.id IS NULL OR upstream_subscription.id IS NULL
		                 OR subscription_group.weekly_limit_usd IS NULL OR subscription_group.weekly_limit_usd <= 0
		            THEN 'unavailable' ELSE 'available' END,
		       COALESCE(subscription_group.name, ''), COALESCE(upstream_subscription.status, ''),
		       COALESCE(CASE WHEN subscription_group.weekly_limit_usd > 0 THEN subscription_group.weekly_limit_usd END, 0)::NUMERIC(20, 8)::text,
		       COALESCE(upstream_subscription.weekly_usage_usd, 0)::NUMERIC(20, 8)::text,
		       CASE WHEN subscription_group.weekly_limit_usd IS NULL OR subscription_group.weekly_limit_usd <= 0 THEN '0'
		            ELSE GREATEST(subscription_group.weekly_limit_usd - COALESCE(upstream_subscription.weekly_usage_usd, 0), 0)::NUMERIC(20, 8)::text END,
		       (subscription_group.weekly_limit_usd IS NOT NULL AND subscription_group.weekly_limit_usd > 0
		        AND COALESCE(upstream_subscription.weekly_usage_usd, 0) >= subscription_group.weekly_limit_usd),
		       upstream_subscription.weekly_window_start
		FROM enterprises AS enterprise
		LEFT JOIN enterprise_subscriptions AS enterprise_subscription
		  ON enterprise_subscription.enterprise_id = enterprise.id AND enterprise_subscription.status = 'active'
		LEFT JOIN user_subscriptions AS upstream_subscription
		  ON upstream_subscription.id = enterprise_subscription.upstream_user_subscription_id
		 AND upstream_subscription.user_id = enterprise.dedicated_upstream_user_id
		 AND upstream_subscription.deleted_at IS NULL
		LEFT JOIN groups AS subscription_group ON subscription_group.id = upstream_subscription.group_id
		WHERE enterprise.id = $1`, enterpriseID).Scan(
		&subscriptionID, &poolStatus, &plan, &subscriptionStatus, &poolLimit, &poolUsed, &poolRemaining, &poolExhausted, &poolAnchor); err != nil {
		return nil, err
	}
	if poolStatus.Valid {
		result.PoolSourceStatus = poolStatus.String
	}
	result.SubscriptionID = nullableInt64(subscriptionID)
	result.SubscriptionPlan = plan.String
	result.SubscriptionStatus = subscriptionStatus.String
	if poolLimit.Valid {
		result.EnterprisePoolLimit = poolLimit.String
	}
	if poolUsed.Valid {
		result.EnterprisePoolUsed = poolUsed.String
	}
	if poolRemaining.Valid {
		result.EnterprisePoolRemaining = poolRemaining.String
	}
	result.EnterprisePoolExhausted = poolExhausted.Valid && poolExhausted.Bool
	if poolAnchor.Valid {
		anchor := poolAnchor.Time
		result.PoolWindowAnchor = &anchor
	}
	if subscriptionID.Valid {
		var observedAt sql.NullTime
		if err := h.owner.service.db.QueryRowContext(ctx, `
			SELECT observed_at
			FROM enterprise_subscription_windows
			WHERE enterprise_id = $1 AND subscription_id = $2
			ORDER BY observed_weekly_window_start DESC, observed_at DESC
			LIMIT 1`, enterpriseID, subscriptionID.Int64).Scan(&observedAt); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if observedAt.Valid {
			observed := observedAt.Time
			result.PoolObservedAt = &observed
		}
	}

	var scheduledPlan sql.NullString
	var scheduledSince sql.NullTime
	if err := h.owner.service.db.QueryRowContext(ctx, `
		SELECT COALESCE(subscription_group.name, ''), enterprise_subscription.created_at
		FROM enterprise_subscriptions AS enterprise_subscription
		JOIN enterprises AS enterprise ON enterprise.id = enterprise_subscription.enterprise_id
		LEFT JOIN user_subscriptions AS upstream_subscription
		  ON upstream_subscription.id = enterprise_subscription.upstream_user_subscription_id
		 AND upstream_subscription.user_id = enterprise.dedicated_upstream_user_id
		 AND upstream_subscription.deleted_at IS NULL
		LEFT JOIN groups AS subscription_group ON subscription_group.id = upstream_subscription.group_id
		WHERE enterprise_subscription.enterprise_id = $1 AND enterprise_subscription.status = 'scheduled'
		ORDER BY enterprise_subscription.created_at DESC
		LIMIT 1`, enterpriseID).Scan(&scheduledPlan, &scheduledSince); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if scheduledSince.Valid {
		result.ScheduledSubscriptionPlan = scheduledPlan.String
		since := scheduledSince.Time
		result.ScheduledSubscriptionSince = &since
	}
	if q.WindowType != "" && q.WindowType != "week" {
		// 当前产品契约只冻结 weekly 总池；其他窗口的 usage 可以查询，但不得借用 weekly 权威值。
		result.PoolSourceStatus = "unavailable"
		result.EnterprisePoolLimit = "0"
		result.EnterprisePoolUsed = "0"
		result.EnterprisePoolRemaining = "0"
		result.EnterprisePoolExhausted = false
		result.PoolWindowAnchor = nil
	}

	granularity := "hour"
	if q.WindowType == "" || q.WindowType == "week" {
		granularity = "day"
	}
	trendRows, err := h.owner.service.db.QueryContext(ctx, `
		SELECT DATE_TRUNC('`+granularity+`', attribution.request_at),
		       COUNT(*), COALESCE(SUM(COALESCE(usage_log.actual_cost, 0)), 0)::text
		FROM enterprise_usage_attributions AS attribution
		LEFT JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
		LEFT JOIN enterprise_employees AS employee
		  ON employee.enterprise_id = attribution.enterprise_id AND employee.id = attribution.employee_id
		WHERE `+scope.where+`
		GROUP BY 1 ORDER BY 1`, scope.args...)
	if err != nil {
		return nil, err
	}
	defer trendRows.Close()
	for trendRows.Next() {
		var point WorkbenchUsageTrendPoint
		if err := trendRows.Scan(&point.At, &point.Requests, &point.UsageCredit); err != nil {
			return nil, err
		}
		result.UsageTrend = append(result.UsageTrend, point)
	}
	return result, trendRows.Err()
}

func buildEmployeeSummaryQuery(scope workbenchSQLScope) string {
	return `
		WITH employee_window_usage AS (
			SELECT attribution.enterprise_id, attribution.employee_id,
			       COALESCE(employee.current_email, employee.email, '') AS email,
			       employee.department_id, attribution.subscription_id,
			       attribution.window_type, attribution.window_anchor,
			       COUNT(*) AS requests,
			       COALESCE(SUM(COALESCE(usage_log.actual_cost, 0)), 0) AS usage_credit
			FROM enterprise_usage_attributions AS attribution
			LEFT JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
			LEFT JOIN enterprise_employees AS employee
			  ON employee.enterprise_id = attribution.enterprise_id AND employee.id = attribution.employee_id
			WHERE ` + scope.where + ` AND attribution.employee_id IS NOT NULL
			GROUP BY attribution.enterprise_id, attribution.employee_id,
			         COALESCE(employee.current_email, employee.email, ''), employee.department_id,
			         attribution.subscription_id, attribution.window_type, attribution.window_anchor
		)
		SELECT usage.employee_id, usage.email, usage.department_id,
		       SUM(usage.requests), COALESCE(SUM(allocation.amount), 0)::text,
		       COALESCE(SUM(usage.usage_credit), 0)::text,
		       GREATEST(COALESCE(SUM(allocation.amount), 0) - COALESCE(SUM(usage.usage_credit), 0), 0)::text,
		       GREATEST(COALESCE(SUM(usage.usage_credit), 0) - COALESCE(SUM(allocation.amount), 0), 0)::text
		FROM employee_window_usage AS usage
		LEFT JOIN enterprise_weekly_allocations AS allocation
		  ON allocation.enterprise_id = usage.enterprise_id
		 AND allocation.subscription_id = usage.subscription_id
		 AND allocation.employee_id = usage.employee_id
		 AND allocation.window_type = usage.window_type
		 AND allocation.window_anchor = usage.window_anchor
		GROUP BY usage.employee_id, usage.email, usage.department_id
		ORDER BY SUM(usage.usage_credit) DESC, usage.employee_id
		LIMIT 100`
}

func (h *WorkbenchHandler) listUsage(ctx context.Context, enterpriseID int64, q workbenchQuery) ([]WorkbenchUsageRow, int64, error) {
	scope := buildWorkbenchUsageScope(enterpriseID, q)
	var total int64
	countQuery := `SELECT COUNT(*) FROM enterprise_usage_attributions AS attribution
		LEFT JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
		LEFT JOIN enterprise_employees AS employee
		  ON employee.enterprise_id = attribution.enterprise_id AND employee.id = attribution.employee_id
		WHERE ` + scope.where
	if err := h.owner.service.db.QueryRowContext(ctx, countQuery, scope.args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limitPos := len(scope.args) + 1
	offsetPos := len(scope.args) + 2
	args := append(append([]any{}, scope.args...), q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := h.owner.service.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT attribution.id, attribution.usage_log_id, attribution.employee_id,
		       COALESCE(employee.current_email, employee.email, ''), employee.department_id,
		       attribution.api_key_id,
		       CASE WHEN LENGTH(api_key.key) <= 10 THEN '********'
		            ELSE SUBSTRING(api_key.key FROM 1 FOR 6) || '...' || RIGHT(api_key.key, 4) END,
		       COALESCE(usage_log.model, ''),
		       attribution.window_type, attribution.window_anchor, attribution.request_at,
		       attribution.classification, attribution.assignment_generation,
		       COALESCE(usage_log.actual_cost, 0)::text,
		       COALESCE((SELECT allocation.amount::text
		                 FROM enterprise_weekly_allocations AS allocation
		                 WHERE allocation.enterprise_id = attribution.enterprise_id
		                   AND allocation.subscription_id = attribution.subscription_id
		                   AND allocation.employee_id = attribution.employee_id
		                   AND allocation.window_type = attribution.window_type
		                   AND allocation.window_anchor = attribution.window_anchor), '0')
		FROM enterprise_usage_attributions AS attribution
		LEFT JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
		LEFT JOIN enterprise_employees AS employee
		  ON employee.enterprise_id = attribution.enterprise_id AND employee.id = attribution.employee_id
		LEFT JOIN api_keys AS api_key ON api_key.id = attribution.api_key_id
		WHERE %s
		ORDER BY attribution.request_at DESC, attribution.id DESC
		LIMIT $%d OFFSET $%d`, scope.where, limitPos, offsetPos), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]WorkbenchUsageRow, 0)
	for rows.Next() {
		var item WorkbenchUsageRow
		var employeeID, departmentID sql.NullInt64
		if err := rows.Scan(&item.AttributionID, &item.UsageLogID, &employeeID, &item.EmployeeEmail,
			&departmentID, &item.APIKeyID, &item.APIKeyMasked, &item.Model, &item.WindowType, &item.WindowAnchor,
			&item.RequestAt, &item.Classification, &item.AssignmentGeneration, &item.UsageCredit,
			&item.ConfiguredCredit); err != nil {
			return nil, 0, err
		}
		item.EmployeeID = nullableInt64(employeeID)
		item.DepartmentID = nullableInt64(departmentID)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (h *WorkbenchHandler) listAuditEvents(ctx context.Context, enterpriseID int64, q workbenchQuery, eventType, entityType, actorRef, result, reason, search string) ([]WorkbenchAuditEvent, int64, error) {
	conditions := []string{"enterprise_id = $1"}
	args := []any{enterpriseID}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if q.EmployeeID != nil {
		args = append(args, *q.EmployeeID)
		position := len(args)
		conditions = append(conditions, fmt.Sprintf("payload->>'employee_id' = $%d::text", position))
	}
	if q.APIKeyID != nil {
		args = append(args, *q.APIKeyID)
		position := len(args)
		conditions = append(conditions, fmt.Sprintf("(entity_type = 'api_key' AND (entity_id = $%d OR payload->>'api_key_id' = $%d::text OR payload->>'previous_api_key_id' = $%d::text))", position, position, position))
	}
	if q.DepartmentID != nil {
		args = append(args, *q.DepartmentID)
		position := len(args)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM enterprise_employees AS employee_filter
			WHERE employee_filter.enterprise_id = enterprise_audit_events.enterprise_id
			  AND employee_filter.department_id = $%d
			  AND enterprise_audit_events.payload->>'employee_id' = $%d::text
		)`, position, position))
	}
	if q.WindowType != "" {
		add("payload->>'window_type' = $%d", q.WindowType)
	}
	if q.WindowAnchor != nil {
		add("NULLIF(payload->>'window_anchor', '')::timestamptz = $%d", *q.WindowAnchor)
	}
	if eventType != "" {
		add("event_type = $%d", eventType)
	}
	if entityType != "" {
		add("entity_type = $%d", entityType)
	}
	if actorRef != "" {
		add("actor_ref = $%d", actorRef)
	}
	if result != "" {
		args = append(args, result)
		position := len(args)
		conditions = append(conditions, fmt.Sprintf("COALESCE(payload->>'result', payload->>'status', 'success') = $%d", position))
	}
	if reason != "" {
		add("payload->>'reason' ILIKE '%%' || $%d || '%%'", reason)
	}
	if search != "" {
		args = append(args, search)
		position := len(args)
		conditions = append(conditions, fmt.Sprintf("(event_type ILIKE '%%' || $%d || '%%' OR entity_type ILIKE '%%' || $%d || '%%' OR actor_ref ILIKE '%%' || $%d || '%%' OR payload::text ILIKE '%%' || $%d || '%%')", position, position, position, position))
	}
	if q.StartAt != nil {
		add("created_at >= $%d", *q.StartAt)
	}
	if q.EndAt != nil {
		add("created_at < $%d", *q.EndAt)
	}
	where := strings.Join(conditions, " AND ")
	var total int64
	if err := h.owner.service.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_audit_events WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limitPos := len(args) + 1
	offsetPos := len(args) + 2
	listArgs := append(append([]any{}, args...), q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := h.owner.service.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, event_type, entity_type, entity_id,
		       COALESCE(payload->>'result', payload->>'status', 'success'),
		       COALESCE(payload->>'reason', ''), payload, actor_ref, created_at
		FROM enterprise_audit_events
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d`, where, limitPos, offsetPos), listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]WorkbenchAuditEvent, 0)
	for rows.Next() {
		var item WorkbenchAuditEvent
		var entityID sql.NullInt64
		var payload []byte
		if err := rows.Scan(&item.ID, &item.EventType, &item.EntityType, &entityID, &item.Result, &item.Reason, &payload, &item.ActorRef, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		item.EntityID = nullableInt64(entityID)
		item.ActorRef = sanitizeAuditActorRef(item.ActorRef)
		item.Payload = sanitizeAuditPayload(payload)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func sanitizeAuditActorRef(actorRef string) string {
	if strings.Contains(strings.ToLower(actorRef), "session") {
		return "enterprise_actor"
	}
	return actorRef
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

// auditPayloadFieldWhitelist enumerates every payload field any audit writer
// in this codebase is known to emit. The detail drawer must never render a
// field outside this set — unknown/unexpected keys are dropped rather than
// passed through, so a future audit writer that accidentally includes a
// secret-bearing field fails closed instead of leaking it.
var auditPayloadFieldWhitelist = map[string]struct{}{
	"result": {}, "reason": {},
	"employee_id": {}, "department_id": {}, "api_key_id": {},
	"subscription_id": {}, "upstream_subscription_id": {}, "upstream_group_id": {},
	"cancelled_subscription_id": {}, "scheduled_subscription_id": {},
	"previous_assignment_id": {}, "new_assignment_id": {},
	"previous_api_key_id": {}, "previous_masked_key": {}, "masked_key": {},
	"window_type": {}, "window_anchor": {},
	"previous_window_start": {}, "observed_window_start": {},
	"assignment_segment_boundary": {},
	"expected_version":            {}, "current_version": {}, "actual_version": {}, "version": {}, "credit": {},
	"name": {}, "status": {}, "affected_employees": {},
	"initial": {}, "keys_disabled": {}, "fields": {},
	"content_type": {}, "size_bytes": {},
	"quota": {}, "quota_used": {},
	"rate_limit_5h": {}, "rate_limit_1d": {}, "rate_limit_7d": {},
	"usage_5h": {}, "usage_1d": {}, "usage_7d": {},
	"window_5h_start": {}, "window_1d_start": {}, "window_7d_start": {},
}

func sanitizeAuditPayload(raw []byte) map[string]any {
	var decoded map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &decoded) != nil {
		return map[string]any{}
	}
	sanitized := make(map[string]any, len(decoded))
	for key, value := range decoded {
		if _, allowed := auditPayloadFieldWhitelist[key]; !allowed {
			continue
		}
		sanitized[key] = value
	}
	return sanitized
}
