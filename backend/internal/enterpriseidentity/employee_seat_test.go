package enterpriseidentity

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func expectEnterpriseEmployeeSeatCount(mock sqlmock.Sqlmock, enterpriseID int64, seatCount, activeEmployeeCount int) {
	mock.ExpectQuery(`(?s)SELECT enterprise\.seat_count,.*FROM enterprises AS enterprise.*FOR UPDATE`).
		WithArgs(enterpriseID).
		WillReturnRows(sqlmock.NewRows([]string{"seat_count", "active_employee_count"}).AddRow(seatCount, activeEmployeeCount))
}

func TestCreateEmployeeRejectsFullSeatCapacityWithActionableMetadata(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	expectEnterpriseEmployeeSeatCount(mock, 7, 1, 2)
	mock.ExpectRollback()

	_, err := svc.CreateEmployee(context.Background(), 7, "blocked@example.com", "strong-password", nil)
	require.ErrorIs(t, err, errEmployeeSeatLimit)
	var appErr *infraerrors.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, "ENTERPRISE_EMPLOYEE_SEAT_LIMIT_REACHED", appErr.Reason)
	require.Equal(t, map[string]string{
		"active_employee_count": "2",
		"seat_count":            "1",
		"seat_overage":          "1",
	}, appErr.Metadata)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateEmployeeRejectsReactivationWhenSeatsAreFull(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status, version FROM enterprise_employees")).
		WithArgs(int64(7), int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "version"}).AddRow("disabled", 3))
	expectEnterpriseEmployeeSeatCount(mock, 7, 2, 2)
	mock.ExpectRollback()

	err := svc.UpdateEmployee(context.Background(), 7, 10, "active", nil, 3)
	require.ErrorIs(t, err, errEmployeeSeatLimit)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPlatformSeatMetricsUseActiveEmployeesInsteadOfKeys(t *testing.T) {
	item := &PlatformEnterprise{SeatCount: 1, ActiveEmployeeCount: 2, ActiveKeyCount: 0}
	setPlatformEnterpriseSeatMetrics(item)
	require.Equal(t, int64(2), item.SeatUsage)
	require.Equal(t, int64(1), item.SeatOverage)
}
