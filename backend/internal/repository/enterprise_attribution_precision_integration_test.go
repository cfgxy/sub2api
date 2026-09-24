//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	enterprise "github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 回归 SHAN-322 QA ⑦：requestAt 携带纳秒精度时，PG timestamptz 只保留微秒
// （且按微秒四舍五入），归因校验把读回的微秒值与内存纳秒值 Equal 比对恒不等，
// 整笔记录被当异常整体回滚：usage_logs 行、归因行同丢，allocation 用量恒 0。
// 纳秒时间源 × 真实 PG 的组合此前从未被覆盖（既有用例的 requestAt 均为整秒/整微秒）。
func TestEnterpriseUsageLogAttributionSurvivesNanosecondRequestAt(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)

	// QA 探针同款纳秒偏移（fraction = .123456789，PG 存为 .123457），保证亚微秒位非零。
	requestAt := fixture.anchor.Add(30 * time.Minute).Add(123456789 * time.Nanosecond)
	require.NotZero(t, requestAt.Nanosecond()%int(time.Microsecond),
		"precondition: requestAt must carry sub-microsecond precision")

	enterpriseRepo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	_, err := enterpriseRepo.SetAllocation(ctx, enterprise.SetAllocationParams{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeWeek, WindowAnchor: fixture.anchor,
		Amount: "100", Reason: "shan322 attribution precision regression",
	})
	require.NoError(t, err)

	loadedSubscription, err := NewUserSubscriptionRepository(testEntClient(t)).GetActiveByUserIDAndGroupID(
		ctx, fixture.enterpriseUserID, fixture.groupID,
	)
	require.NoError(t, err)
	loadedAPIKey, err := NewAPIKeyRepository(testEntClient(t), integrationDB).GetByID(ctx, fixture.apiKeyID)
	require.NoError(t, err)
	require.True(t, loadedAPIKey.EnterpriseAttributionCandidate)

	requestID := fmt.Sprintf("shan322-attrib-precision-%d", time.Now().UnixNano())
	input := &service.RecordUsageInput{
		Result: &service.ForwardResult{
			RequestID: requestID,
			Model:     "claude-sonnet-4", Duration: time.Second,
			Usage: service.ClaudeUsage{InputTokens: 1000, OutputTokens: 500},
		},
		APIKey:       loadedAPIKey,
		User:         &service.User{ID: fixture.enterpriseUserID},
		Account:      &service.Account{ID: fixture.accountID},
		PricingAt:    requestAt,
		Subscription: loadedSubscription,
	}
	input.APIKey.GroupID = &fixture.groupID
	input.APIKey.Group = &service.Group{
		ID: fixture.groupID, SubscriptionType: service.SubscriptionTypeSubscription, RateMultiplier: 1,
	}

	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	usageRepo := NewUsageLogRepository(nil, integrationDB)
	gateway := service.NewGatewayService(
		nil, nil, usageRepo, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, &service.BillingCacheService{}, nil, nil,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	require.NoError(t, gateway.RecordUsage(ctx, input))

	// 面 1：usage_logs 行落库（缺陷态下随归因校验失败被整体回滚，0 行）
	var usageLogID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT id FROM usage_logs WHERE request_id = $1 AND api_key_id = $2",
		requestID, fixture.apiKeyID).Scan(&usageLogID))

	// 面 2：三窗口归因行齐全、归因到员工、request_at 与窗口锚已收敛到微秒精度
	rows, err := integrationDB.QueryContext(ctx, `
		SELECT window_type, employee_id, classification, window_anchor, request_at
		FROM enterprise_usage_attributions
		WHERE usage_log_id = $1
		ORDER BY window_type
	`, usageLogID)
	require.NoError(t, err)
	type attributionRow struct {
		employeeID     sql.NullInt64
		classification string
		windowAnchor   time.Time
		requestAt      time.Time
	}
	attributions := map[string]attributionRow{}
	for rows.Next() {
		var windowType string
		var row attributionRow
		require.NoError(t, rows.Scan(&windowType, &row.employeeID, &row.classification,
			&row.windowAnchor, &row.requestAt))
		attributions[windowType] = row
	}
	require.NoError(t, rows.Err())
	require.Len(t, attributions, 3, "day/week/month 归因行都应存在")
	for _, windowType := range []string{"day", "week", "month"} {
		row := attributions[windowType]
		require.Equal(t, "employee", row.classification, windowType)
		require.True(t, row.employeeID.Valid && row.employeeID.Int64 == fixture.employeeID,
			"%s 归因应落到员工", windowType)
		require.True(t, row.windowAnchor.Equal(fixture.anchor), "%s 窗口锚应与订阅一致", windowType)
		require.True(t, row.requestAt.Equal(requestAt.UTC().Truncate(time.Microsecond)),
			"%s request_at 应等于纳秒源值收敛到微秒后的存储精度值", windowType)
	}

	// 面 3：员工 allocation 用量累计（缺陷态下恒 0）
	summary, err := enterpriseRepo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeWeek, WindowAnchor: fixture.anchor,
	})
	require.NoError(t, err)
	require.NotEqual(t, "0.00000000", summary.UsageCredit, "员工 allocation 用量应累计本次调用")
}
