package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type enterpriseSubscriptionRefresherStub struct {
	calls [][2]int64
	err   error
}

func (s *enterpriseSubscriptionRefresherStub) RefreshPlatformEnterpriseSubscriptionsForDedicatedUser(_ context.Context, dedicatedUpstreamUserID, actorUserID int64) error {
	s.calls = append(s.calls, [2]int64{dedicatedUpstreamUserID, actorUserID})
	return s.err
}

type resetQuotaBillingCacheStub struct {
	BillingCache
	invalidations [][2]int64
	published     []string
}

func (s *resetQuotaBillingCacheStub) InvalidateSubscriptionCache(_ context.Context, userID, groupID int64) error {
	s.invalidations = append(s.invalidations, [2]int64{userID, groupID})
	return nil
}

func (s *resetQuotaBillingCacheStub) PublishSubscriptionCacheInvalidation(_ context.Context, cacheKey string) error {
	s.published = append(s.published, cacheKey)
	return nil
}

func (*resetQuotaBillingCacheStub) SubscribeSubscriptionCacheInvalidation(context.Context, func(string)) error {
	return nil
}

func TestAssignSubscriptionRefreshesEnterpriseMapping(t *testing.T) {
	subscriptions := NewSubscriptionService(&subscriptionGroupRepoStub{
		group: &Group{ID: 7, SubscriptionType: SubscriptionTypeSubscription},
	}, newSubscriptionUserSubRepoStub(), nil, nil, nil)
	refresher := &enterpriseSubscriptionRefresherStub{}
	subscriptions.SetEnterpriseSubscriptionRefresher(refresher)

	subscription, err := subscriptions.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 42, GroupID: 7, ValidityDays: 30, AssignedBy: 9,
	})

	require.NoError(t, err)
	require.Equal(t, int64(42), subscription.UserID)
	require.Equal(t, [][2]int64{{42, 9}}, refresher.calls)
}

func TestBulkAssignSubscriptionReportsEnterpriseMappingRefreshFailure(t *testing.T) {
	subscriptions := NewSubscriptionService(&subscriptionGroupRepoStub{
		group: &Group{ID: 7, SubscriptionType: SubscriptionTypeSubscription},
	}, newSubscriptionUserSubRepoStub(), nil, nil, nil)
	refresher := &enterpriseSubscriptionRefresherStub{err: errors.New("database unavailable")}
	subscriptions.SetEnterpriseSubscriptionRefresher(refresher)

	result, err := subscriptions.BulkAssignSubscription(context.Background(), &BulkAssignSubscriptionInput{
		UserIDs: []int64{42}, GroupID: 7, ValidityDays: 30, AssignedBy: 9,
	})

	require.NoError(t, err)
	require.Equal(t, 0, result.SuccessCount)
	require.Equal(t, 1, result.FailedCount)
	require.Equal(t, "failed", result.Statuses[42])
	require.Equal(t, [][2]int64{{42, 9}}, refresher.calls)
	require.Contains(t, result.Errors[0], "subscription assigned but enterprise mapping refresh failed")
}

func TestAssignOrExtendSubscriptionRefreshesOnlyAfterOuterTransactionCommit(t *testing.T) {
	for _, tc := range []struct {
		name       string
		commit     bool
		refreshErr error
		wantCalls  int
	}{
		{name: "commit", commit: true, wantCalls: 1},
		{name: "rollback", wantCalls: 0},
		{name: "committed refresh failure remains diagnosable without undoing mutation", commit: true, refreshErr: errors.New("database unavailable"), wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			mock.ExpectBegin()
			tx, err := client.Tx(context.Background())
			require.NoError(t, err)

			refresher := &enterpriseSubscriptionRefresherStub{err: tc.refreshErr}
			subscriptions := NewSubscriptionService(&subscriptionGroupRepoStub{
				group: &Group{ID: 7, SubscriptionType: SubscriptionTypeSubscription},
			}, newSubscriptionUserSubRepoStub(), nil, client, nil)
			t.Cleanup(subscriptions.Stop)
			subscriptions.SetEnterpriseSubscriptionRefresher(refresher)

			_, _, err = subscriptions.AssignOrExtendSubscription(dbent.NewTxContext(context.Background(), tx), &AssignSubscriptionInput{
				UserID: 42, GroupID: 7, ValidityDays: 30, AssignedBy: 9,
			})
			require.NoError(t, err)
			require.Empty(t, refresher.calls, "refresh must not observe uncommitted subscription state")

			if tc.commit {
				mock.ExpectCommit()
				require.NoError(t, tx.Commit())
			} else {
				mock.ExpectRollback()
				require.NoError(t, tx.Rollback())
			}
			require.Len(t, refresher.calls, tc.wantCalls)
			if tc.wantCalls > 0 {
				require.Equal(t, [2]int64{42, 9}, refresher.calls[0])
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

type enterpriseMappingMutationRepo struct {
	userSubRepoNoop
	sub            UserSubscription
	getActiveCalls int
}

func (r *enterpriseMappingMutationRepo) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	if r.sub.ID != id || r.sub.DeletedAt != nil {
		return nil, ErrSubscriptionNotFound
	}
	copy := r.sub
	return &copy, nil
}

func (r *enterpriseMappingMutationRepo) GetByIDForUpdate(ctx context.Context, id int64) (*UserSubscription, error) {
	return r.GetByID(ctx, id)
}

func (r *enterpriseMappingMutationRepo) GetByIDIncludeDeleted(_ context.Context, id int64) (*UserSubscription, error) {
	if r.sub.ID != id {
		return nil, ErrSubscriptionNotFound
	}
	copy := r.sub
	return &copy, nil
}

func (r *enterpriseMappingMutationRepo) GetByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	if r.sub.UserID != userID || r.sub.GroupID != groupID {
		return nil, ErrSubscriptionNotFound
	}
	copy := r.sub
	return &copy, nil
}

func (r *enterpriseMappingMutationRepo) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	r.getActiveCalls++
	if r.sub.UserID != userID || r.sub.GroupID != groupID || r.sub.DeletedAt != nil {
		return nil, ErrSubscriptionNotFound
	}
	copy := r.sub
	return &copy, nil
}

func (r *enterpriseMappingMutationRepo) Delete(_ context.Context, id int64) error {
	if r.sub.ID != id {
		return ErrSubscriptionNotFound
	}
	now := time.Now()
	r.sub.DeletedAt = &now
	return nil
}

func (r *enterpriseMappingMutationRepo) ExistsActiveByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (r *enterpriseMappingMutationRepo) Restore(_ context.Context, id int64, status string) (*UserSubscription, error) {
	if r.sub.ID != id {
		return nil, ErrSubscriptionNotFound
	}
	r.sub.DeletedAt = nil
	r.sub.Status = status
	copy := r.sub
	return &copy, nil
}

func (r *enterpriseMappingMutationRepo) ExtendExpiry(_ context.Context, id int64, expiresAt time.Time) error {
	if r.sub.ID != id {
		return ErrSubscriptionNotFound
	}
	r.sub.ExpiresAt = expiresAt
	return nil
}

func (r *enterpriseMappingMutationRepo) UpdateStatus(_ context.Context, id int64, status string) error {
	if r.sub.ID != id {
		return ErrSubscriptionNotFound
	}
	r.sub.Status = status
	return nil
}

func (r *enterpriseMappingMutationRepo) UpdateNotes(_ context.Context, id int64, notes string) error {
	if r.sub.ID != id {
		return ErrSubscriptionNotFound
	}
	r.sub.Notes = notes
	return nil
}

func (r *enterpriseMappingMutationRepo) ResetUsageWindows(_ context.Context, id int64, resetDaily, resetWeekly, resetMonthly bool, dailyStart, periodicStart time.Time) error {
	if r.sub.ID != id {
		return ErrSubscriptionNotFound
	}
	if resetDaily {
		r.sub.DailyUsageUSD, r.sub.DailyWindowStart = 0, &dailyStart
	}
	if resetWeekly {
		r.sub.WeeklyUsageUSD, r.sub.WeeklyWindowStart = 0, &periodicStart
	}
	if resetMonthly {
		r.sub.MonthlyUsageUSD, r.sub.MonthlyWindowStart = 0, &periodicStart
	}
	return nil
}

func TestSubscriptionStateChangesRefreshEnterpriseMapping(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name      string
		deletedAt *time.Time
		run       func(*SubscriptionService, *enterpriseMappingMutationRepo) error
		wantActor int64
	}{
		{name: "revoke", run: func(s *SubscriptionService, _ *enterpriseMappingMutationRepo) error {
			return s.RevokeSubscription(context.Background(), 1, 9)
		}, wantActor: 9},
		{name: "restore", deletedAt: &now, run: func(s *SubscriptionService, _ *enterpriseMappingMutationRepo) error {
			_, err := s.RestoreSubscription(context.Background(), 1, 9)
			return err
		}, wantActor: 9},
		{name: "extend", run: func(s *SubscriptionService, _ *enterpriseMappingMutationRepo) error {
			_, err := s.ExtendSubscription(context.Background(), 1, 5, 9)
			return err
		}, wantActor: 9},
		{name: "redeem reduction", run: func(s *SubscriptionService, _ *enterpriseMappingMutationRepo) error {
			redeem := &RedeemService{subscriptionService: s}
			return redeem.reduceOrCancelSubscription(context.Background(), 42, 7, 5, "REDEEM")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &enterpriseMappingMutationRepo{sub: UserSubscription{
				ID: 1, UserID: 42, GroupID: 7, Status: SubscriptionStatusActive,
				ExpiresAt: now.AddDate(0, 0, 30), DeletedAt: tc.deletedAt,
			}}
			refresher := &enterpriseSubscriptionRefresherStub{}
			subscriptions := NewSubscriptionService(nil, repo, nil, nil, nil)
			t.Cleanup(subscriptions.Stop)
			subscriptions.SetEnterpriseSubscriptionRefresher(refresher)

			require.NoError(t, tc.run(subscriptions, repo))
			require.Equal(t, [][2]int64{{42, tc.wantActor}}, refresher.calls)
		})
	}
}

func TestAdminResetQuotaWeeklyRefreshesEnterpriseMappingAfterOuterTransactionCommit(t *testing.T) {
	for _, tc := range []struct {
		name       string
		commit     bool
		refreshErr error
		wantCalls  int
	}{
		{name: "commit", commit: true, wantCalls: 1},
		{name: "rollback", wantCalls: 0},
		{name: "committed refresh failure does not roll back reset", commit: true, refreshErr: errors.New("database unavailable"), wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			mock.ExpectBegin()
			tx, err := client.Tx(context.Background())
			require.NoError(t, err)

			repo := &enterpriseMappingMutationRepo{sub: UserSubscription{ID: 1, UserID: 42, GroupID: 7, Status: SubscriptionStatusActive, ExpiresAt: time.Now().AddDate(0, 0, 30)}}
			refresher := &enterpriseSubscriptionRefresherStub{err: tc.refreshErr}
			subscriptions := NewSubscriptionService(nil, repo, nil, client, nil)
			t.Cleanup(subscriptions.Stop)
			subscriptions.SetEnterpriseSubscriptionRefresher(refresher)

			_, err = subscriptions.AdminResetQuota(dbent.NewTxContext(context.Background(), tx), 1, false, true, false, 9)
			require.NoError(t, err)
			require.Empty(t, refresher.calls, "refresh must not observe the uncommitted weekly anchor")

			if tc.commit {
				mock.ExpectCommit()
				require.NoError(t, tx.Commit())
			} else {
				mock.ExpectRollback()
				require.NoError(t, tx.Rollback())
			}
			require.Len(t, refresher.calls, tc.wantCalls)
			if tc.wantCalls > 0 {
				require.Equal(t, [2]int64{42, 9}, refresher.calls[0])
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAdminResetQuotaCacheInvalidationOnlyAfterOuterTransactionCommit(t *testing.T) {
	for _, tc := range []struct {
		name       string
		commit     bool
		wantEvents int
	}{
		{name: "commit", commit: true, wantEvents: 1},
		{name: "rollback", commit: false, wantEvents: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			mock.ExpectBegin()
			tx, err := client.Tx(context.Background())
			require.NoError(t, err)

			repo := &enterpriseMappingMutationRepo{sub: UserSubscription{ID: 1, UserID: 42, GroupID: 7, Status: SubscriptionStatusActive, ExpiresAt: time.Now().AddDate(0, 0, 30)}}
			billingCache := &resetQuotaBillingCacheStub{}
			billingService := NewBillingCacheService(billingCache, nil, nil, nil, nil, nil, &config.Config{}, nil)
			t.Cleanup(billingService.Stop)
			subscriptions := NewSubscriptionService(nil, repo, billingService, client, &config.Config{
				SubscriptionCache: config.SubscriptionCacheConfig{L1Size: 16, L1TTLSeconds: 60},
			})
			t.Cleanup(subscriptions.Stop)

			_, err = subscriptions.AdminResetQuota(dbent.NewTxContext(context.Background(), tx), 1, false, true, false, 9)
			require.NoError(t, err)
			require.Empty(t, billingCache.invalidations, "cache events must not precede outer commit")
			require.Empty(t, billingCache.published, "cross-instance events must not precede outer commit")

			if tc.commit {
				mock.ExpectCommit()
				require.NoError(t, tx.Commit())
			} else {
				mock.ExpectRollback()
				require.NoError(t, tx.Rollback())
			}

			require.Len(t, billingCache.invalidations, tc.wantEvents)
			require.Len(t, billingCache.published, tc.wantEvents)
			if tc.wantEvents > 0 {
				require.Equal(t, [2]int64{42, 7}, billingCache.invalidations[0])
				require.Equal(t, subCacheKey(42, 7), billingCache.published[0])
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAdminResetQuotaWeeklyReportsDirectRefreshFailure(t *testing.T) {
	repo := &enterpriseMappingMutationRepo{sub: UserSubscription{ID: 1, UserID: 42, GroupID: 7, Status: SubscriptionStatusActive, ExpiresAt: time.Now().AddDate(0, 0, 30)}}
	refreshErr := errors.New("enterprise mapping unavailable")
	refresher := &enterpriseSubscriptionRefresherStub{err: refreshErr}
	subscriptions := NewSubscriptionService(nil, repo, nil, nil, nil)
	t.Cleanup(subscriptions.Stop)
	subscriptions.SetEnterpriseSubscriptionRefresher(refresher)

	_, err := subscriptions.AdminResetQuota(context.Background(), 1, false, true, false, 9)

	require.ErrorIs(t, err, refreshErr)
	require.Equal(t, [][2]int64{{42, 9}}, refresher.calls)
	require.NotNil(t, repo.sub.WeeklyWindowStart, "the already-applied direct reset remains diagnosable rather than being misreported as rolled back")
}

func TestAdminResetQuotaWeeklyRefreshFailureInvalidatesPrewarmedL1(t *testing.T) {
	oldAnchor := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	resetAt := oldAnchor.Add(7 * 24 * time.Hour)
	repo := &enterpriseMappingMutationRepo{sub: UserSubscription{
		ID:                1,
		UserID:            42,
		GroupID:           7,
		Status:            SubscriptionStatusActive,
		ExpiresAt:         resetAt.AddDate(0, 0, 30),
		WeeklyUsageUSD:    12.5,
		WeeklyWindowStart: &oldAnchor,
	}}
	refreshErr := errors.New("enterprise mapping unavailable")
	billingCache := &resetQuotaBillingCacheStub{}
	billingService := NewBillingCacheService(billingCache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(billingService.Stop)
	subscriptions := NewSubscriptionService(nil, repo, nil, nil, &config.Config{
		SubscriptionCache: config.SubscriptionCacheConfig{L1Size: 16, L1TTLSeconds: 60},
	})
	subscriptions.billingCacheService = billingService
	t.Cleanup(subscriptions.Stop)
	subscriptions.now = func() time.Time { return resetAt }
	subscriptions.SetEnterpriseSubscriptionRefresher(&enterpriseSubscriptionRefresherStub{err: refreshErr})

	stale, err := subscriptions.GetActiveSubscription(context.Background(), 42, 7)
	require.NoError(t, err)
	require.Equal(t, 12.5, stale.WeeklyUsageUSD)
	subscriptions.subCacheL1.Wait()
	require.Equal(t, 1, repo.getActiveCalls, "setup must prewarm L1 with the old weekly window")

	_, err = subscriptions.AdminResetQuota(context.Background(), 1, false, true, false, 9)

	require.ErrorIs(t, err, refreshErr, "mapping failure remains diagnosable")
	require.Equal(t, 0.0, repo.sub.WeeklyUsageUSD, "weekly reset remains persisted")
	require.Equal(t, resetAt, *repo.sub.WeeklyWindowStart)

	refreshed, err := subscriptions.GetActiveSubscription(context.Background(), 42, 7)
	require.NoError(t, err)
	require.Equal(t, 0.0, refreshed.WeeklyUsageUSD)
	require.Equal(t, resetAt, *refreshed.WeeklyWindowStart)
	require.Equal(t, 2, repo.getActiveCalls, "failed mapping refresh must not leave a stale L1 hit")
	require.Equal(t, [][2]int64{{42, 7}}, billingCache.invalidations, "shared billing cache is invalidated before mapping refresh can fail")
	require.Equal(t, []string{subCacheKey(42, 7)}, billingCache.published, "other instances receive the L1 invalidation event")
}
