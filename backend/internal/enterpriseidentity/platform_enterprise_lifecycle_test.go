package enterpriseidentity

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

var errInvalidEnterpriseInputTarget = infraerrors.BadRequest("INVALID_ENTERPRISE", "test target")

// SHAN-382 缺口②：启用接口把 disabled 企业置回 active 并落审计事件。
func TestEnableEnterpriseRestoresDisabledEnterprise(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE enterprises SET status = 'active', updated_at = NOW\(\) WHERE id = \$1 AND status = 'disabled'`).
		WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events.*'enterprise\.enabled'`).WithArgs(int64(7), sqlmock.AnyArg(), "platform_user:1").
		WillReturnResult(sqlmock.NewResult(3, 1))
	mock.ExpectCommit()

	err := svc.EnableEnterprise(context.Background(), 7, 1, "平台运营确认启用")

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 缺口②：对已 active 的企业启用是幂等成功，不写审计。
func TestEnableEnterpriseIsIdempotentForActiveEnterprise(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE enterprises SET status = 'active', updated_at = NOW\(\) WHERE id = \$1 AND status = 'disabled'`).
		WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT status FROM enterprises WHERE id = \$1`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	mock.ExpectRollback()

	err := svc.EnableEnterprise(context.Background(), 7, 1, "平台运营确认启用")

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 缺口②：不存在的企业 id 返回 errNotFound。
func TestEnableEnterpriseRejectsUnknownID(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE enterprises SET status = 'active', updated_at = NOW\(\) WHERE id = \$1 AND status = 'disabled'`).
		WithArgs(int64(404)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT status FROM enterprises WHERE id = \$1`).WithArgs(int64(404)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err := svc.EnableEnterprise(context.Background(), 404, 1, "平台运营确认启用")

	require.ErrorIs(t, err, errNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 缺口②：id 与 reason 必填，缺失时零数据库访问。
func TestEnableEnterpriseRequiresIDAndReason(t *testing.T) {
	svc, mock := newMockService(t)

	require.ErrorIs(t, svc.EnableEnterprise(context.Background(), 0, 1, "reason"), errInvalidEnterpriseInputTarget)
	require.ErrorIs(t, svc.EnableEnterprise(context.Background(), 7, 1, "  "), errInvalidEnterpriseInputTarget)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 缺口③：合法域名修改成功并落审计（含旧值）。
func TestUpdateEnterpriseHostAppliesValidChangeWithAudit(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT LOWER\(BTRIM\(portal_host\)\) FROM enterprises WHERE id = \$1`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"host"}).AddRow("old.example.com"))
	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM enterprises WHERE LOWER\(BTRIM\(portal_host\)\) = \$1 AND id <> \$2\)`).
		WithArgs("new.example.com", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(`UPDATE enterprises SET portal_host = \$2, updated_at = NOW\(\) WHERE id = \$1`).
		WithArgs(int64(7), "new.example.com").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events.*'enterprise\.host_updated'`).WithArgs(int64(7), sqlmock.AnyArg(), "platform_user:1").
		WillReturnResult(sqlmock.NewResult(4, 1))
	mock.ExpectCommit()

	err := svc.UpdateEnterpriseHost(context.Background(), 7, 1, "  New.Example.COM. ", "平台运营修改入口域名")

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 缺口③：域名格式非法时零数据库访问直接拒绝。
func TestUpdateEnterpriseHostRejectsInvalidFormat(t *testing.T) {
	svc, mock := newMockService(t)
	for _, host := range []string{
		"", "http://acme.example.com", "acme example.com", "acme.example.com:8080",
		"-lead.example.com", "trail-.example.com", "acme..example.com",
		"acme_example.com", "acme.example.com/", strings.Repeat("a", 256) + ".com",
	} {
		err := svc.UpdateEnterpriseHost(context.Background(), 7, 1, host, "平台运营修改入口域名")
		require.ErrorIs(t, err, errInvalidEnterpriseInputTarget, "host=%q", host)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 缺口③：域名与既有企业冲突时拒绝并回滚。
func TestUpdateEnterpriseHostRejectsDuplicateHost(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT LOWER\(BTRIM\(portal_host\)\) FROM enterprises WHERE id = \$1`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"host"}).AddRow("old.example.com"))
	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM enterprises WHERE LOWER\(BTRIM\(portal_host\)\) = \$1 AND id <> \$2\)`).
		WithArgs("taken.example.com", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()

	err := svc.UpdateEnterpriseHost(context.Background(), 7, 1, "taken.example.com", "平台运营修改入口域名")

	require.ErrorIs(t, err, errConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 缺口③：不存在的企业 id 返回 errNotFound。
func TestUpdateEnterpriseHostRejectsUnknownID(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT LOWER\(BTRIM\(portal_host\)\) FROM enterprises WHERE id = \$1`).WithArgs(int64(404)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err := svc.UpdateEnterpriseHost(context.Background(), 404, 1, "new.example.com", "平台运营修改入口域名")

	require.ErrorIs(t, err, errNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 缺口③：提交与目标域名一致时幂等成功，零写入零审计。
func TestUpdateEnterpriseHostAcceptsSameHostWithoutWrite(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT LOWER\(BTRIM\(portal_host\)\) FROM enterprises WHERE id = \$1`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"host"}).AddRow("same.example.com"))
	mock.ExpectRollback()

	err := svc.UpdateEnterpriseHost(context.Background(), 7, 1, "SAME.example.com", "平台运营修改入口域名")

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 缺口③：修改后旧 Host 不再解析到任何企业（token Host 绑定自然失效），新 Host 可解析。
func TestUpdateEnterpriseHostOldHostStopsResolvingNewHostResolves(t *testing.T) {
	svc, mock := newMockService(t)
	mock.MatchExpectationsInOrder(false)
	// 修改事务
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT LOWER\(BTRIM\(portal_host\)\) FROM enterprises WHERE id = \$1`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"host"}).AddRow("old.example.com"))
	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM enterprises WHERE LOWER\(BTRIM\(portal_host\)\) = \$1 AND id <> \$2\)`).
		WithArgs("new.example.com", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(`UPDATE enterprises SET portal_host = \$2, updated_at = NOW\(\) WHERE id = \$1`).
		WithArgs(int64(7), "new.example.com").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events.*'enterprise\.host_updated'`).WithArgs(int64(7), sqlmock.AnyArg(), "platform_user:1").
		WillReturnResult(sqlmock.NewResult(5, 1))
	mock.ExpectCommit()
	// 旧 Host 解析：无行 → errWrongHost
	mock.ExpectQuery(`SELECT id, name, LOWER\(BTRIM\(portal_host\)\), admin_user_id, status\s+FROM enterprises\s+WHERE LOWER\(BTRIM\(portal_host\)\) = \$1`).
		WithArgs("old.example.com").WillReturnError(sql.ErrNoRows)
	// 新 Host 解析：命中 active 企业
	mock.ExpectQuery(`SELECT id, name, LOWER\(BTRIM\(portal_host\)\), admin_user_id, status\s+FROM enterprises\s+WHERE LOWER\(BTRIM\(portal_host\)\) = \$1`).
		WithArgs("new.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(int64(7), "Acme", "new.example.com", int64(99), "active"))

	require.NoError(t, svc.UpdateEnterpriseHost(context.Background(), 7, 1, "new.example.com", "平台运营修改入口域名"))
	_, oldHostErr := svc.enterpriseByHost(context.Background(), "old.example.com")
	require.ErrorIs(t, oldHostErr, errWrongHost)
	resolved, err := svc.enterpriseByHost(context.Background(), "new.example.com")
	require.NoError(t, err)
	require.Equal(t, int64(7), resolved.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

var _ = time.Now

// SHAN-382 回归：DisableEnterprise 的级联停用链路保持既有语义——
// 置 disabled、吊销会话、吊销 Key 分配、禁用 API Key，并落 enterprise.disabled 审计。
func TestDisableEnterpriseAppliesWholeCascade(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE enterprises SET status = 'disabled', updated_at = NOW\(\) WHERE id = \$1 AND status = 'active'`).
		WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE enterprise_sessions SET revoked_at = COALESCE\(revoked_at, NOW\(\)\), updated_at = NOW\(\) WHERE enterprise_id = \$1 AND revoked_at IS NULL`).
		WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE enterprise_key_assignments`).WithArgs(int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE api_keys SET status = 'disabled'`).WithArgs(int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events.*'enterprise\.disabled'`).WithArgs(int64(7), sqlmock.AnyArg(), "platform_user:1").
		WillReturnResult(sqlmock.NewResult(9, 1))
	mock.ExpectCommit()

	err := svc.DisableEnterprise(context.Background(), 7, 1, "平台运营停用")

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 回归：对非 active 状态（含不存在 id）停用返回 errNotFound，不落审计。
func TestDisableEnterpriseRejectsUnknownID(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE enterprises SET status = 'disabled', updated_at = NOW\(\) WHERE id = \$1 AND status = 'active'`).
		WithArgs(int64(404)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := svc.DisableEnterprise(context.Background(), 404, 1, "平台运营停用")

	require.ErrorIs(t, err, errNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 回归：停用 id 与 reason 必填，缺失时零数据库访问。
func TestDisableEnterpriseRequiresIDAndReason(t *testing.T) {
	svc, mock := newMockService(t)

	require.ErrorIs(t, svc.DisableEnterprise(context.Background(), 0, 1, "reason"), errInvalidEnterpriseInputTarget)
	require.ErrorIs(t, svc.DisableEnterprise(context.Background(), 7, 1, "  "), errInvalidEnterpriseInputTarget)
	require.NoError(t, mock.ExpectationsWereMet())
}
