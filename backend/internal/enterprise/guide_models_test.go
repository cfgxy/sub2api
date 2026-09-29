package enterprise

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestGetEmployeeGuideGroupIDUsesCurrentActiveSubscription(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	r := &Repository{db: db}
	mock.ExpectQuery("FROM enterprise_key_assignments AS assignment").
		WithArgs(int64(5), int64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id"}).AddRow(int64(42)))
	groupID, err := r.GetEmployeeGuideGroupID(context.Background(), 5, 8)
	require.NoError(t, err)
	require.Equal(t, int64(42), *groupID)

	mock.ExpectQuery("FROM enterprise_key_assignments AS assignment").
		WithArgs(int64(5), int64(8)).WillReturnError(sql.ErrNoRows)
	groupID, err = r.GetEmployeeGuideGroupID(context.Background(), 5, 8)
	require.NoError(t, err)
	require.Nil(t, groupID)
	require.NoError(t, mock.ExpectationsWereMet())
}
