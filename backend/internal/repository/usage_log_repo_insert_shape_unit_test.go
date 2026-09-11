//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

var (
	usageLogStaticInsertShapeRe = regexp.MustCompile(`(?s)INSERT INTO usage_logs \((.*?)\) VALUES \((.*?)\)`)
	usageLogPlaceholderRe       = regexp.MustCompile(`\$(\d+)`)
)

// newSQLCapturingMock 返回把实际下发 SQL 记录到 captured 的 sqlmock；语句一律视为匹配，
// 参数仍由 WithArgs 校验。
func newSQLCapturingMock(t *testing.T, captured *[]string) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	matcher := sqlmock.QueryMatcherFunc(func(_, actualSQL string) error {
		*captured = append(*captured, actualSQL)
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

// requireStaticInsertMatchesArgTypes 断言手写的 INSERT：列清单长度与 VALUES 占位符数量
// 都等于 usageLogInsertArgTypes，且占位符恰为 $1..$N 各出现一次。
func requireStaticInsertMatchesArgTypes(t *testing.T, query string) {
	t.Helper()
	m := usageLogStaticInsertShapeRe.FindStringSubmatch(query)
	require.Len(t, m, 3, "unrecognised INSERT shape:\n%s", query)

	want := len(usageLogInsertArgTypes)
	columns := 0
	for _, col := range strings.Split(m[1], ",") {
		if strings.TrimSpace(col) != "" {
			columns++
		}
	}
	require.Equal(t, want, columns, "INSERT column list must match usageLogInsertArgTypes")

	seen := make(map[int]struct{}, want)
	for _, ph := range usageLogPlaceholderRe.FindAllStringSubmatch(m[2], -1) {
		n, err := strconv.Atoi(ph[1])
		require.NoError(t, err)
		_, dup := seen[n]
		require.False(t, dup, "duplicate placeholder $%d", n)
		seen[n] = struct{}{}
	}
	require.Len(t, seen, want, "VALUES placeholder count must match usageLogInsertArgTypes")
	for i := 1; i <= want; i++ {
		_, ok := seen[i]
		require.True(t, ok, "missing placeholder $%d", i)
	}
}

// TestUsageLogStaticInsertShape_PlaceholdersMatchArgTypes 覆盖两条不经占位符生成器、
// 直接手写 $1..$N 的 INSERT 路径，防止加列后漏补占位符只在集成测试才暴露。
func TestUsageLogStaticInsertShape_PlaceholdersMatchArgTypes(t *testing.T) {
	upstreamRequestID := "20260902080329-oneapi"
	log := &service.UsageLog{
		UserID:            1,
		APIKeyID:          2,
		AccountID:         3,
		RequestID:         "client:insert-shape",
		UpstreamRequestID: &upstreamRequestID,
		Model:             "claude-3",
		InputTokens:       10,
		CreatedAt:         time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC),
	}
	prepared := prepareUsageLogInsert(log)
	args := anySliceToDriverValues(prepared.args)

	t.Run("createSingle", func(t *testing.T) {
		var captured []string
		db, mock := newSQLCapturingMock(t, &captured)
		repo := &usageLogRepository{sql: db}

		mock.ExpectQuery("INSERT INTO usage_logs").
			WithArgs(args...).
			WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(1), log.CreatedAt))

		inserted, err := repo.Create(context.Background(), log)
		require.NoError(t, err)
		require.True(t, inserted)
		require.NoError(t, mock.ExpectationsWereMet())
		require.Len(t, captured, 1)
		requireStaticInsertMatchesArgTypes(t, captured[0])
	})

	t.Run("execUsageLogInsertNoResult", func(t *testing.T) {
		var captured []string
		db, mock := newSQLCapturingMock(t, &captured)

		mock.ExpectExec("INSERT INTO usage_logs").
			WithArgs(args...).
			WillReturnResult(sqlmock.NewResult(0, 1))

		require.NoError(t, execUsageLogInsertNoResult(context.Background(), db, prepared))
		require.NoError(t, mock.ExpectationsWereMet())
		require.Len(t, captured, 1)
		requireStaticInsertMatchesArgTypes(t, captured[0])
	})
}

// TestPrepareUsageLogInsert_UpstreamRequestIDArgWiring 把 upstream_request_id 钉在
// session_id 之前，与参数类型表保持同位；缺失时落 NULL 而不是空串。
func TestPrepareUsageLogInsert_UpstreamRequestIDArgWiring(t *testing.T) {
	upstreamRequestID := "req_upstream_123"
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:            1,
		APIKeyID:          2,
		RequestID:         "client:wiring",
		Model:             "gpt-5",
		UpstreamRequestID: &upstreamRequestID,
		CreatedAt:         time.Now().UTC(),
	})
	require.Len(t, prepared.args, len(usageLogInsertArgTypes))

	idx := len(prepared.args) - 4
	arg, ok := prepared.args[idx].(sql.NullString)
	require.True(t, ok, "upstream_request_id arg should be sql.NullString, got %T", prepared.args[idx])
	require.True(t, arg.Valid)
	require.Equal(t, upstreamRequestID, arg.String)
	require.Equal(t, "text", usageLogInsertArgTypes[idx])

	absent := prepareUsageLogInsert(&service.UsageLog{UserID: 1, APIKeyID: 2, RequestID: "client:absent", Model: "gpt-5", CreatedAt: time.Now().UTC()})
	nullArg, ok := absent.args[idx].(sql.NullString)
	require.True(t, ok)
	require.False(t, nullArg.Valid, "absent upstream request id must be NULL")

	require.Contains(t, usageLogSelectColumns, "upstream_request_id")
}

func TestUsageLogEnterpriseAttributionUsesDatabaseTransaction(t *testing.T) {
	requestAt := time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)
	dayAnchor := requestAt.Add(-time.Hour)
	subscriptionID := int64(41)

	t.Run("提交 usage log 与部分 anchor", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		repo := newUsageLogRepositoryWithSQL(nil, db)
		log := &service.UsageLog{
			UserID: 1, APIKeyID: 2, AccountID: 3, RequestID: "client:enterprise-commit", Model: "gpt-5",
			SubscriptionID: &subscriptionID, CreatedAt: requestAt.Add(time.Hour), AttributionRequestAt: requestAt,
			AttributionDailyWindowAnchor: &dayAnchor,
		}
		log.EnterpriseAttribution = enterpriseAttributionSnapshotForUnit(requestAt, &dayAnchor)

		mock.ExpectBegin()
		mock.ExpectQuery("INSERT INTO usage_logs").WillReturnRows(
			sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(99), log.CreatedAt),
		)
		mock.ExpectExec("INSERT INTO enterprise_usage_attributions").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT enterprise_id").WillReturnRows(enterpriseAttributionRowsForUnit(requestAt, dayAnchor))
		mock.ExpectCommit()

		inserted, createErr := repo.Create(context.Background(), log)
		require.NoError(t, createErr)
		require.True(t, inserted)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("attribution 失败时回滚 usage log", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		repo := newUsageLogRepositoryWithSQL(nil, db)
		log := &service.UsageLog{
			UserID: 1, APIKeyID: 2, AccountID: 3, RequestID: "client:enterprise-rollback", Model: "gpt-5",
			SubscriptionID: &subscriptionID, CreatedAt: requestAt.Add(time.Hour), AttributionRequestAt: requestAt,
			AttributionDailyWindowAnchor: &dayAnchor,
		}
		log.EnterpriseAttribution = enterpriseAttributionSnapshotForUnit(requestAt, &dayAnchor)

		mock.ExpectBegin()
		mock.ExpectQuery("INSERT INTO usage_logs").WillReturnRows(
			sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(100), log.CreatedAt),
		)
		mock.ExpectExec("INSERT INTO enterprise_usage_attributions").WillReturnError(errors.New("attribution failed"))
		mock.ExpectRollback()

		inserted, createErr := repo.Create(context.Background(), log)
		require.ErrorContains(t, createErr, "attribution failed")
		require.False(t, inserted)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUsageLogEnterpriseAttributionWithoutDatabaseUsesProvidedExecutor(t *testing.T) {
	requestAt := time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)
	dayAnchor := requestAt.Add(-time.Hour)
	subscriptionID := int64(41)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &usageLogRepository{sql: db}
	log := &service.UsageLog{
		UserID: 1, APIKeyID: 2, AccountID: 3, RequestID: "client:enterprise-executor", Model: "gpt-5",
		SubscriptionID: &subscriptionID, CreatedAt: requestAt.Add(time.Hour), AttributionRequestAt: requestAt,
		AttributionDailyWindowAnchor: &dayAnchor,
	}
	log.EnterpriseAttribution = enterpriseAttributionSnapshotForUnit(requestAt, &dayAnchor)

	mock.ExpectQuery("INSERT INTO usage_logs").WillReturnRows(
		sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(101), log.CreatedAt),
	)
	mock.ExpectExec("INSERT INTO enterprise_usage_attributions").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT enterprise_id").WillReturnRows(enterpriseAttributionRowsForUnit(requestAt, dayAnchor))

	inserted, createErr := repo.Create(context.Background(), log)
	require.NoError(t, createErr)
	require.True(t, inserted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func enterpriseAttributionSnapshotForUnit(requestAt time.Time, dayAnchor *time.Time) *service.EnterpriseUsageAttributionSnapshot {
	return &service.EnterpriseUsageAttributionSnapshot{
		EnterpriseID: 10, SubscriptionID: 11, Classification: "controlled_external",
		RequestAt: requestAt, DailyWindowAnchor: dayAnchor,
	}
}

func enterpriseAttributionRowsForUnit(requestAt, dayAnchor time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"enterprise_id", "subscription_id", "employee_id", "api_key_id", "assignment_generation",
		"window_type", "window_anchor", "request_at", "classification",
	}).AddRow(int64(10), int64(11), nil, int64(2), int64(0), "day", dayAnchor, requestAt, "controlled_external")
}

func TestUsageLogOrdinarySubscriptionDoesNotExecuteEnterpriseSQL(t *testing.T) {
	requestAt := time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)
	dayAnchor := requestAt.Add(-time.Hour)
	subscriptionID := int64(41)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &usageLogRepository{sql: db}
	log := &service.UsageLog{
		UserID: 1, APIKeyID: 2, AccountID: 3, Model: "gpt-5",
		SubscriptionID: &subscriptionID, CreatedAt: requestAt.Add(time.Hour), AttributionRequestAt: requestAt,
		AttributionDailyWindowAnchor: &dayAnchor,
	}

	mock.ExpectQuery("INSERT INTO usage_logs").WillReturnRows(
		sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(102), log.CreatedAt),
	)

	inserted, createErr := repo.Create(context.Background(), log)
	require.NoError(t, createErr)
	require.True(t, inserted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogOrdinarySubscriptionProductionRepositoryKeepsBestEffortBatchPath(t *testing.T) {
	requestAt := time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)
	dayAnchor := requestAt.Add(-time.Hour)
	subscriptionID := int64(41)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewUsageLogRepository(nil, db).(*usageLogRepository)
	repo.bestEffortBatchCh = make(chan usageLogBestEffortRequest, 1)
	batched := make(chan struct{}, 1)
	go func() {
		req := <-repo.bestEffortBatchCh
		batched <- struct{}{}
		sendUsageLogBestEffortResult(req.resultCh, nil)
	}()

	err = repo.CreateBestEffort(context.Background(), &service.UsageLog{
		UserID: 1, APIKeyID: 2, AccountID: 3, RequestID: "client:ordinary-best-effort", Model: "gpt-5",
		SubscriptionID: &subscriptionID, CreatedAt: requestAt.Add(time.Hour), AttributionRequestAt: requestAt,
		AttributionDailyWindowAnchor: &dayAnchor,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet(), "ordinary subscription must not execute enterprise SQL")
	select {
	case <-batched:
	default:
		t.Fatal("ordinary subscription did not use the best-effort batch path")
	}
}
