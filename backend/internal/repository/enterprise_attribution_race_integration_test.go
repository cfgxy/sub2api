//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	enterprise "github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type gatedEnterpriseAttributionAPIKeyRepository struct {
	service.APIKeyRepository
	resolver *apiKeyRepository
	started  chan int64
	release  <-chan struct{}
	result   chan error
}

func (r *gatedEnterpriseAttributionAPIKeyRepository) ResolveEnterpriseUsageAttributionIdentity(
	ctx context.Context,
	apiKeyID int64,
	credential string,
	upstreamSubscriptionID *int64,
) (*service.EnterpriseUsageAttributionIdentity, error) {
	var observedSubscriptionID int64
	if upstreamSubscriptionID != nil {
		observedSubscriptionID = *upstreamSubscriptionID
	}
	r.started <- observedSubscriptionID
	select {
	case <-r.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	identity, err := r.resolver.ResolveEnterpriseUsageAttributionIdentity(
		ctx,
		apiKeyID,
		credential,
		upstreamSubscriptionID,
	)
	r.result <- err
	return identity, err
}

func TestEnterpriseAttributionRejectsStaleSubscriptionAfterRebind(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	fixture := seedEnterpriseFixture(t, ctx)
	secondSubscriptionID, secondGroupID := insertEnterpriseUpstreamSubscription(t, ctx, fixture.enterpriseID)
	var secondSubscriptionAnchor time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT weekly_window_start FROM user_subscriptions WHERE id = $1`, secondSubscriptionID).
		Scan(&secondSubscriptionAnchor))
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE groups
		SET subscription_type = 'subscription'
		WHERE id IN ($1, $2)
	`, fixture.groupID, secondGroupID)
	require.NoError(t, err)
	var credential string
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&credential))

	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseResolver := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseResolver)
	realAPIKeyRepo := newAPIKeyRepositoryWithSQL(integrationEntClient, integrationDB)
	gatedAPIKeyRepo := &gatedEnterpriseAttributionAPIKeyRepository{
		APIKeyRepository: realAPIKeyRepo,
		resolver:         realAPIKeyRepo,
		started:          make(chan int64, 1),
		release:          release,
		result:           make(chan error, 1),
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	apiKeyService := service.NewAPIKeyService(
		gatedAPIKeyRepo,
		NewUserRepository(integrationEntClient, integrationDB),
		NewGroupRepository(integrationEntClient, integrationDB),
		NewUserSubscriptionRepository(integrationEntClient),
		nil,
		nil,
		cfg,
	)
	subscriptionService := service.NewSubscriptionService(
		NewGroupRepository(integrationEntClient, integrationDB),
		NewUserSubscriptionRepository(integrationEntClient),
		nil,
		integrationEntClient,
		nil,
	)
	t.Cleanup(subscriptionService.Stop)
	usageRepo := NewUsageLogRepository(integrationEntClient, integrationDB)
	requestID := "shan153-attribution-race-" + time.Now().UTC().Format("20060102150405.000000000")
	var handlerCalls atomic.Int32
	handlerErr := make(chan error, 1)

	router := gin.New()
	router.Use(gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(apiKeyService, subscriptionService, cfg)))
	router.POST("/race", func(c *gin.Context) {
		handlerCalls.Add(1)
		apiKey, apiKeyOK := middleware.GetAPIKeyFromContext(c)
		subscription, subscriptionOK := middleware.GetSubscriptionFromContext(c)
		if !apiKeyOK || !subscriptionOK {
			handlerErr <- service.ErrEnterpriseAttributionUnavailable
			c.Status(http.StatusInternalServerError)
			return
		}
		requestAt := time.Now().UTC()
		log := enterpriseUsageLogForTest(
			fixture,
			requestID,
			requestAt,
			requestAt,
			subscription.DailyWindowStart,
			subscription.WeeklyWindowStart,
			subscription.MonthlyWindowStart,
		)
		log.SubscriptionID = &subscription.ID
		service.ApplyEnterpriseUsageAttribution(log, apiKey, subscription, requestAt)
		_, createErr := usageRepo.Create(c.Request.Context(), log)
		handlerErr <- createErr
		if createErr != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/race", nil).WithContext(ctx)
	request.Header.Set("x-api-key", credential)
	requestDone := make(chan struct{})
	go func() {
		router.ServeHTTP(response, request)
		close(requestDone)
	}()

	select {
	case observedSubscriptionID := <-gatedAPIKeyRepo.started:
		require.Equal(t, fixture.upstreamSubscriptionID, observedSubscriptionID)
	case <-ctx.Done():
		t.Fatal("middleware did not reach enterprise attribution finalization")
	}

	switchAt := time.Now().UTC()
	switchTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = switchTx.Rollback() }()
	_, err = switchTx.ExecContext(ctx, `
		UPDATE enterprise_subscriptions
		SET status = 'ended', ended_at = $2, actor_ref = 'test:attribution-race'
		WHERE id = $1
	`, fixture.subscriptionID, switchAt)
	require.NoError(t, err)
	var secondEnterpriseSubscriptionID int64
	err = switchTx.QueryRowContext(ctx, `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			observed_weekly_window_start, activated_at, actor_ref
		) VALUES ($1, $2, 'active', $3, $4, 'test:attribution-race')
		RETURNING id
	`, fixture.enterpriseID, secondSubscriptionID, secondSubscriptionAnchor, switchAt).
		Scan(&secondEnterpriseSubscriptionID)
	require.NoError(t, err)
	require.NoError(t, switchTx.Commit())

	_, err = enterprise.NewRepository(integrationDB, apiKeyService).RebindKeyAssignment(ctx, enterprise.RebindKeyAssignmentParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID, APIKeyID: fixture.apiKeyID,
		UpstreamSubscriptionID: secondSubscriptionID, ActorRef: "test:attribution-race",
	})
	require.NoError(t, err)
	releaseResolver()

	select {
	case <-requestDone:
	case <-ctx.Done():
		t.Fatal("middleware request did not complete")
	}
	resolveErr := <-gatedAPIKeyRepo.result
	require.ErrorIs(t, resolveErr, service.ErrEnterpriseAttributionUnavailable)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Zero(t, handlerCalls.Load())
	select {
	case unexpectedErr := <-handlerErr:
		t.Fatalf("usage handler unexpectedly ran: %v", unexpectedErr)
	default:
	}

	var usageLogCount, controlledExternalCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM usage_logs WHERE request_id = $1`, requestID).Scan(&usageLogCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM enterprise_usage_attributions AS attribution
		JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
		WHERE usage_log.request_id = $1
		  AND attribution.classification = 'controlled_external'
	`, requestID).Scan(&controlledExternalCount))
	require.Zero(t, usageLogCount)
	require.Zero(t, controlledExternalCount)

	identity, err := realAPIKeyRepo.ResolveEnterpriseUsageAttributionIdentity(
		ctx,
		fixture.apiKeyID,
		credential,
		&secondSubscriptionID,
	)
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.Equal(t, secondEnterpriseSubscriptionID, identity.EnterpriseSubscriptionID)
	require.Equal(t, secondSubscriptionID, identity.UpstreamSubscriptionID)
	require.Equal(t, "employee", identity.Classification)
	require.NotNil(t, identity.EmployeeID)
	require.Equal(t, fixture.employeeID, *identity.EmployeeID)
}

func TestEnterpriseAttributionFailsClosedAcrossLifecycleMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testCases := []struct {
		name   string
		rotate bool
	}{
		{name: "rotation", rotate: true},
		{name: "disable"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			fixture := seedEnterpriseFixture(t, ctx)
			_, err := integrationDB.ExecContext(ctx,
				`UPDATE groups SET subscription_type = 'subscription' WHERE id = $1`, fixture.groupID)
			require.NoError(t, err)
			var credential string
			require.NoError(t, integrationDB.QueryRowContext(ctx,
				`SELECT key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&credential))

			release := make(chan struct{})
			var releaseOnce sync.Once
			releaseResolver := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(releaseResolver)
			realAPIKeyRepo := newAPIKeyRepositoryWithSQL(integrationEntClient, integrationDB)
			gatedAPIKeyRepo := &gatedEnterpriseAttributionAPIKeyRepository{
				APIKeyRepository: realAPIKeyRepo,
				resolver:         realAPIKeyRepo,
				started:          make(chan int64, 1),
				release:          release,
				result:           make(chan error, 1),
			}
			cfg := &config.Config{RunMode: config.RunModeStandard}
			apiKeyService := service.NewAPIKeyService(
				gatedAPIKeyRepo,
				NewUserRepository(integrationEntClient, integrationDB),
				NewGroupRepository(integrationEntClient, integrationDB),
				NewUserSubscriptionRepository(integrationEntClient),
				nil,
				nil,
				cfg,
			)
			subscriptionService := service.NewSubscriptionService(
				NewGroupRepository(integrationEntClient, integrationDB),
				NewUserSubscriptionRepository(integrationEntClient),
				nil,
				integrationEntClient,
				nil,
			)
			t.Cleanup(subscriptionService.Stop)
			usageRepo := NewUsageLogRepository(integrationEntClient, integrationDB)
			requestID := fmt.Sprintf("shan153-attribution-lifecycle-%s-%d", tc.name, time.Now().UnixNano())
			var handlerCalls atomic.Int32

			router := gin.New()
			router.Use(gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(apiKeyService, subscriptionService, cfg)))
			router.POST("/lifecycle", func(c *gin.Context) {
				handlerCalls.Add(1)
				apiKey, apiKeyOK := middleware.GetAPIKeyFromContext(c)
				subscription, subscriptionOK := middleware.GetSubscriptionFromContext(c)
				if !apiKeyOK || !subscriptionOK {
					c.Status(http.StatusInternalServerError)
					return
				}
				requestAt := time.Now().UTC()
				log := enterpriseUsageLogForTest(
					fixture,
					requestID,
					requestAt,
					requestAt,
					subscription.DailyWindowStart,
					subscription.WeeklyWindowStart,
					subscription.MonthlyWindowStart,
				)
				log.SubscriptionID = &subscription.ID
				service.ApplyEnterpriseUsageAttribution(log, apiKey, subscription, requestAt)
				if _, createErr := usageRepo.Create(c.Request.Context(), log); createErr != nil {
					c.Status(http.StatusInternalServerError)
					return
				}
				c.Status(http.StatusNoContent)
			})

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/lifecycle", nil).WithContext(ctx)
			request.Header.Set("x-api-key", credential)
			requestDone := make(chan struct{})
			go func() {
				router.ServeHTTP(response, request)
				close(requestDone)
			}()

			select {
			case observedSubscriptionID := <-gatedAPIKeyRepo.started:
				require.Equal(t, fixture.upstreamSubscriptionID, observedSubscriptionID)
			case <-ctx.Done():
				t.Fatal("middleware did not reach enterprise attribution finalization")
			}

			lifecycleRepo := enterprise.NewRepository(integrationDB, apiKeyService)
			var mutationResult *enterprise.EmployeeKeyMutationResult
			if tc.rotate {
				mutationResult, err = lifecycleRepo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
					EnterpriseID:     fixture.enterpriseID,
					EmployeeID:       fixture.employeeID,
					ExpectedAPIKeyID: fixture.apiKeyID,
					IdempotencyKey:   "review-attribution-lifecycle-rotation",
					Plaintext:        fmt.Sprintf("sk-review-attribution-lifecycle-%d", time.Now().UnixNano()),
					ActorRef:         "test:employee",
				})
			} else {
				mutationResult, err = lifecycleRepo.DisableEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
					EnterpriseID:     fixture.enterpriseID,
					EmployeeID:       fixture.employeeID,
					ExpectedAPIKeyID: fixture.apiKeyID,
					IdempotencyKey:   "review-attribution-lifecycle-disable",
					ActorRef:         "test:employee",
				})
			}
			require.NoError(t, err)
			require.NotNil(t, mutationResult)
			releaseResolver()

			select {
			case <-requestDone:
			case <-ctx.Done():
				t.Fatal("middleware request did not complete")
			}
			var resolveErr error
			select {
			case resolveErr = <-gatedAPIKeyRepo.result:
			case <-ctx.Done():
				t.Fatal("enterprise attribution resolver did not return")
			}
			var expectedStatus int
			switch {
			case errors.Is(resolveErr, service.ErrAPIKeyNotFound):
				expectedStatus = http.StatusUnauthorized
			case errors.Is(resolveErr, service.ErrEnterpriseAttributionUnavailable):
				expectedStatus = http.StatusServiceUnavailable
			default:
				t.Fatalf("enterprise attribution returned unexpected error type %T", resolveErr)
			}
			require.Equal(t, expectedStatus, response.Code)
			require.Zero(t, handlerCalls.Load())

			var usageLogCount, controlledExternalCount int
			require.NoError(t, integrationDB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM usage_logs WHERE request_id = $1`, requestID).Scan(&usageLogCount))
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM enterprise_usage_attributions AS attribution
				JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
				WHERE usage_log.request_id = $1
				  AND attribution.classification = 'controlled_external'
			`, requestID).Scan(&controlledExternalCount))
			require.Zero(t, usageLogCount)
			require.Zero(t, controlledExternalCount)

			if tc.rotate {
				require.NotNil(t, mutationResult.Key)
				identity, resolveSuccessorErr := realAPIKeyRepo.ResolveEnterpriseUsageAttributionIdentity(
					ctx,
					mutationResult.Key.ID,
					mutationResult.Plaintext,
					&fixture.upstreamSubscriptionID,
				)
				require.NoError(t, resolveSuccessorErr)
				require.NotNil(t, identity)
				require.Equal(t, "employee", identity.Classification)
				require.NotNil(t, identity.EmployeeID)
				require.Equal(t, fixture.employeeID, *identity.EmployeeID)
			}
		})
	}
}

func TestEnterpriseAttributionFinalizesWindowAnchorsFromPrimarySnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	var credential string
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&credential))

	realAPIKeyRepo := newAPIKeyRepositoryWithSQL(integrationEntClient, integrationDB)
	subscriptionService := service.NewSubscriptionService(
		NewGroupRepository(integrationEntClient, integrationDB),
		NewUserSubscriptionRepository(integrationEntClient),
		nil,
		integrationEntClient,
		nil,
	)
	t.Cleanup(subscriptionService.Stop)
	cachedSubscription, err := subscriptionService.GetActiveSubscription(ctx, fixture.enterpriseUserID, fixture.groupID)
	require.NoError(t, err)
	require.NotNil(t, cachedSubscription)
	require.Equal(t, fixture.anchor, *cachedSubscription.DailyWindowStart)

	apiKeyService := service.NewAPIKeyService(
		realAPIKeyRepo,
		NewUserRepository(integrationEntClient, integrationDB),
		NewGroupRepository(integrationEntClient, integrationDB),
		NewUserSubscriptionRepository(integrationEntClient),
		nil,
		nil,
		&config.Config{},
	)
	apiKey, err := realAPIKeyRepo.GetByKeyForAuth(ctx, credential)
	require.NoError(t, err)
	require.True(t, apiKey.EnterpriseAttributionCandidate)

	primaryDaily := fixture.anchor.Add(-2 * time.Hour)
	primaryWeekly := fixture.anchor.Add(-3 * time.Hour)
	primaryMonthly := fixture.anchor.Add(-4 * time.Hour)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE user_subscriptions
		SET daily_window_start = $2, weekly_window_start = $3, monthly_window_start = $4
		WHERE id = $1
	`, fixture.upstreamSubscriptionID, primaryDaily, primaryWeekly, primaryMonthly)
	require.NoError(t, err)

	require.NoError(t, apiKeyService.FinalizeEnterpriseUsageAttribution(ctx, apiKey, credential, cachedSubscription))
	require.NotNil(t, apiKey.EnterpriseAttributionIdentity)
	identity := apiKey.EnterpriseAttributionIdentity
	require.True(t, identity.WindowAnchorsResolved)
	require.Equal(t, primaryDaily, *identity.DailyWindowAnchor)
	require.Equal(t, primaryWeekly, *identity.WeeklyWindowAnchor)
	require.Equal(t, primaryMonthly, *identity.MonthlyWindowAnchor)

	requestAt := time.Now().UTC().Truncate(time.Microsecond)
	log := &service.UsageLog{}
	service.ApplyEnterpriseUsageAttribution(log, apiKey, cachedSubscription, requestAt)
	require.NotNil(t, log.EnterpriseAttribution)
	require.Equal(t, primaryDaily, *log.EnterpriseAttribution.DailyWindowAnchor)
	require.Equal(t, primaryWeekly, *log.EnterpriseAttribution.WeeklyWindowAnchor)
	require.Equal(t, primaryMonthly, *log.EnterpriseAttribution.MonthlyWindowAnchor)
}

func TestEnterpriseAttributionPreservesFrozenIdentityAcrossLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type frozenIdentity struct {
		employeeID           int64
		assignmentGeneration int64
		subscriptionID       int64
		classification       string
	}
	testCases := []struct {
		name   string
		rotate bool
	}{
		{name: "rotation", rotate: true},
		{name: "disable"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			t.Cleanup(cancel)
			fixture := seedEnterpriseFixture(t, ctx)
			_, err := integrationDB.ExecContext(ctx,
				`UPDATE groups SET subscription_type = 'subscription' WHERE id = $1`, fixture.groupID)
			require.NoError(t, err)
			var credential string
			require.NoError(t, integrationDB.QueryRowContext(ctx,
				`SELECT key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&credential))

			realAPIKeyRepo := newAPIKeyRepositoryWithSQL(integrationEntClient, integrationDB)
			originalIdentity, err := realAPIKeyRepo.ResolveEnterpriseUsageAttributionIdentity(
				ctx,
				fixture.apiKeyID,
				credential,
				&fixture.upstreamSubscriptionID,
			)
			require.NoError(t, err)
			require.NotNil(t, originalIdentity)
			require.NotNil(t, originalIdentity.EmployeeID)
			require.Equal(t, "employee", originalIdentity.Classification)
			require.True(t, originalIdentity.WindowAnchorsResolved)
			var expectedDailyAnchor, expectedWeeklyAnchor, expectedMonthlyAnchor time.Time
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
					SELECT daily_window_start, weekly_window_start, monthly_window_start
					FROM user_subscriptions WHERE id = $1
				`, fixture.upstreamSubscriptionID).Scan(
				&expectedDailyAnchor, &expectedWeeklyAnchor, &expectedMonthlyAnchor,
			))
			require.Equal(t, expectedDailyAnchor, *originalIdentity.DailyWindowAnchor)
			require.Equal(t, expectedWeeklyAnchor, *originalIdentity.WeeklyWindowAnchor)
			require.Equal(t, expectedMonthlyAnchor, *originalIdentity.MonthlyWindowAnchor)
			originalEmployeeID := *originalIdentity.EmployeeID

			cfg := &config.Config{RunMode: config.RunModeStandard}
			apiKeyService := service.NewAPIKeyService(
				realAPIKeyRepo,
				NewUserRepository(integrationEntClient, integrationDB),
				NewGroupRepository(integrationEntClient, integrationDB),
				NewUserSubscriptionRepository(integrationEntClient),
				nil,
				nil,
				cfg,
			)
			subscriptionService := service.NewSubscriptionService(
				NewGroupRepository(integrationEntClient, integrationDB),
				NewUserSubscriptionRepository(integrationEntClient),
				nil,
				integrationEntClient,
				nil,
			)
			t.Cleanup(subscriptionService.Stop)
			require.NoError(t, apiKeyService.TouchLastUsed(ctx, fixture.apiKeyID))

			lifecycleApplication := fmt.Sprintf("shan153-attribution-freeze-%s-%d", tc.name, time.Now().UnixNano())
			gate := time.Now().UnixNano()
			lifecycleDB := openPartitionCleanupDB(t, ctx, "public", lifecycleApplication)
			cleanupTrigger := installEmployeeKeyBillingGate(t, ctx, fixture.apiKeyID, gate, lifecycleApplication)
			t.Cleanup(cleanupTrigger)
			releaseLifecycleGate := holdEmployeeKeyGate(t, ctx, gate)
			lifecycleRepo := enterprise.NewRepository(lifecycleDB, apiKeyService)
			usageRepo := NewUsageLogRepository(integrationEntClient, integrationDB)
			requestID := fmt.Sprintf("shan153-attribution-frozen-%s-%d", tc.name, time.Now().UnixNano())

			releaseUsage := make(chan struct{})
			var releaseUsageOnce sync.Once
			releaseUsageWrite := func() { releaseUsageOnce.Do(func() { close(releaseUsage) }) }
			identityFrozen := make(chan frozenIdentity, 1)
			handlerResult := make(chan error, 1)
			requestDone := make(chan struct{})
			lifecycleDone := make(chan error, 1)
			var mutationResult *enterprise.EmployeeKeyMutationResult
			lifecycleStarted := false
			requestStarted := false
			t.Cleanup(func() {
				releaseUsageWrite()
				releaseLifecycleGate()
				cancel()
				if requestStarted {
					select {
					case <-requestDone:
					case <-time.After(2 * time.Second):
					}
				}
				if lifecycleStarted {
					select {
					case <-lifecycleDone:
					case <-time.After(2 * time.Second):
					}
				}
			})

			router := gin.New()
			router.Use(gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(apiKeyService, subscriptionService, cfg)))
			router.POST("/lifecycle-frozen-attribution", func(c *gin.Context) {
				apiKey, apiKeyOK := middleware.GetAPIKeyFromContext(c)
				subscription, subscriptionOK := middleware.GetSubscriptionFromContext(c)
				if !apiKeyOK || !subscriptionOK || apiKey.EnterpriseAttributionIdentity == nil ||
					apiKey.EnterpriseAttributionIdentity.EmployeeID == nil {
					handlerResult <- service.ErrEnterpriseAttributionUnavailable
					c.Status(http.StatusInternalServerError)
					return
				}
				identity := apiKey.EnterpriseAttributionIdentity
				requestAt := time.Now().UTC().Truncate(time.Microsecond)
				log := enterpriseUsageLogForTest(
					fixture,
					requestID,
					requestAt,
					requestAt,
					subscription.DailyWindowStart,
					subscription.WeeklyWindowStart,
					subscription.MonthlyWindowStart,
				)
				log.SubscriptionID = &subscription.ID
				service.ApplyEnterpriseUsageAttribution(log, apiKey, subscription, requestAt)
				if log.EnterpriseAttribution == nil || log.EnterpriseAttribution.EmployeeID == nil {
					handlerResult <- service.ErrEnterpriseAttributionUnavailable
					c.Status(http.StatusInternalServerError)
					return
				}
				identityFrozen <- frozenIdentity{
					employeeID:           *identity.EmployeeID,
					assignmentGeneration: identity.AssignmentGeneration,
					subscriptionID:       identity.EnterpriseSubscriptionID,
					classification:       identity.Classification,
				}
				select {
				case <-releaseUsage:
				case <-c.Request.Context().Done():
					handlerResult <- c.Request.Context().Err()
					c.Status(http.StatusInternalServerError)
					return
				}
				inserted, createErr := usageRepo.Create(c.Request.Context(), log)
				if createErr != nil {
					handlerResult <- createErr
					c.Status(http.StatusInternalServerError)
					return
				}
				if !inserted {
					handlerResult <- errors.New("usage log was not inserted")
					c.Status(http.StatusInternalServerError)
					return
				}
				handlerResult <- nil
				c.Status(http.StatusNoContent)
			})

			lifecycleStarted = true
			go func() {
				params := enterprise.EmployeeKeyMutationParams{
					EnterpriseID:     fixture.enterpriseID,
					EmployeeID:       fixture.employeeID,
					ExpectedAPIKeyID: fixture.apiKeyID,
					IdempotencyKey:   fmt.Sprintf("attribution-frozen-%s-%d", tc.name, time.Now().UnixNano()),
					ActorRef:         "test:attribution-frozen",
				}
				var mutationErr error
				if tc.rotate {
					params.Plaintext = fmt.Sprintf("sk-attribution-frozen-%d", time.Now().UnixNano())
					mutationResult, mutationErr = lifecycleRepo.RotateEmployeeKey(ctx, params)
				} else {
					mutationResult, mutationErr = lifecycleRepo.DisableEmployeeKey(ctx, params)
				}
				lifecycleDone <- mutationErr
				close(lifecycleDone)
			}()
			requireEmployeeKeyApplicationWaitingOnLock(t, ctx, lifecycleApplication, lifecycleDone)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/lifecycle-frozen-attribution", nil).WithContext(ctx)
			request.Header.Set("x-api-key", credential)
			requestStarted = true
			go func() {
				router.ServeHTTP(response, request)
				close(requestDone)
			}()

			var observed frozenIdentity
			select {
			case observed = <-identityFrozen:
			case <-requestDone:
				var handlerErr error
				select {
				case handlerErr = <-handlerResult:
				default:
				}
				t.Fatalf("request completed before identity was frozen: %v", handlerErr)
			case <-ctx.Done():
				t.Fatal("handler did not observe the frozen enterprise identity")
			}
			require.Equal(t, originalEmployeeID, observed.employeeID)
			require.Equal(t, originalIdentity.AssignmentGeneration, observed.assignmentGeneration)
			require.Equal(t, originalIdentity.EnterpriseSubscriptionID, observed.subscriptionID)
			require.Equal(t, originalIdentity.Classification, observed.classification)

			var prematureUsageCount int
			require.NoError(t, integrationDB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM usage_logs WHERE request_id = $1`, requestID).Scan(&prematureUsageCount))
			require.Zero(t, prematureUsageCount)

			releaseLifecycleGate()
			select {
			case mutationErr := <-lifecycleDone:
				require.NoError(t, mutationErr)
			case <-ctx.Done():
				t.Fatal("lifecycle mutation did not commit")
			}
			require.NotNil(t, mutationResult)
			releaseUsageWrite()

			select {
			case <-requestDone:
			case <-ctx.Done():
				t.Fatal("router request did not complete")
			}
			select {
			case handlerErr := <-handlerResult:
				require.NoError(t, handlerErr)
			case <-ctx.Done():
				t.Fatal("usage handler did not report its result")
			}
			require.Equal(t, http.StatusNoContent, response.Code)

			var usageLogID int64
			require.NoError(t, integrationDB.QueryRowContext(ctx,
				`SELECT id FROM usage_logs WHERE request_id = $1 AND api_key_id = $2`,
				requestID, fixture.apiKeyID).Scan(&usageLogID))
			var totalAttributions, matchingAttributions, matchingWindowAnchors, controlledExternalAttributions int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT COUNT(*),
				       COUNT(*) FILTER (
				           WHERE employee_id = $2
				             AND assignment_generation = $3
				             AND subscription_id = $4
				             AND classification = $5
				       ),
				       COUNT(*) FILTER (WHERE window_anchor = $6),
				       COUNT(*) FILTER (WHERE classification = 'controlled_external')
				FROM enterprise_usage_attributions
				WHERE usage_log_id = $1
			`, usageLogID, originalEmployeeID, originalIdentity.AssignmentGeneration,
				originalIdentity.EnterpriseSubscriptionID, originalIdentity.Classification, expectedDailyAnchor).
				Scan(&totalAttributions, &matchingAttributions, &matchingWindowAnchors, &controlledExternalAttributions))
			require.Equal(t, 3, totalAttributions)
			require.Equal(t, totalAttributions, matchingAttributions)
			require.Equal(t, totalAttributions, matchingWindowAnchors)
			require.Zero(t, controlledExternalAttributions)

			if tc.rotate {
				require.NotNil(t, mutationResult.Key)
				successorIdentity, resolveErr := realAPIKeyRepo.ResolveEnterpriseUsageAttributionIdentity(
					ctx,
					mutationResult.Key.ID,
					mutationResult.Plaintext,
					&fixture.upstreamSubscriptionID,
				)
				require.NoError(t, resolveErr)
				require.NotNil(t, successorIdentity)
				require.Equal(t, "employee", successorIdentity.Classification)
				require.NotNil(t, successorIdentity.EmployeeID)
				require.Equal(t, originalEmployeeID, *successorIdentity.EmployeeID)
				require.Equal(t, originalIdentity.AssignmentGeneration+1, successorIdentity.AssignmentGeneration)
				require.Equal(t, originalIdentity.EnterpriseSubscriptionID, successorIdentity.EnterpriseSubscriptionID)
			}
		})
	}
}

func TestEnterpriseAttributionClassifiesCandidateWithoutActiveAssignmentAsControlledExternal(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE enterprise_key_assignments
		SET status = 'ended', ended_at = NOW(), actor_ref = 'test:attribution-no-active-assignment'
		WHERE api_key_id = $1 AND status = 'active'
	`, fixture.apiKeyID)
	require.NoError(t, err)

	var credential string
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&credential))
	identity, err := newAPIKeyRepositoryWithSQL(integrationEntClient, integrationDB).
		ResolveEnterpriseUsageAttributionIdentity(ctx, fixture.apiKeyID, credential, &fixture.upstreamSubscriptionID)
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.Equal(t, "controlled_external", identity.Classification)
	require.Nil(t, identity.EmployeeID)
	require.Zero(t, identity.AssignmentGeneration)
}

func TestEnterpriseCandidateAuthWithoutSubscriptionContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testCases := []struct {
		name                    string
		runMode                 string
		path                    string
		withSubscriptionService bool
	}{
		{
			name:                    "standard_billing_info",
			runMode:                 config.RunModeStandard,
			path:                    "/v1/sub2api/billing",
			withSubscriptionService: true,
		},
		{
			name:    "simple_mode",
			runMode: config.RunModeSimple,
			path:    "/simple-auth",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			t.Cleanup(cancel)

			fixture := seedEnterpriseFixture(t, ctx)
			_, err := integrationDB.ExecContext(ctx,
				`UPDATE groups SET subscription_type = 'subscription' WHERE id = $1`, fixture.groupID)
			require.NoError(t, err)

			var credential string
			var enterpriseCandidate bool
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT key, enterprise_attribution_candidate
				FROM api_keys
				WHERE id = $1
			`, fixture.apiKeyID).Scan(&credential, &enterpriseCandidate))
			require.True(t, enterpriseCandidate)
			var activeAssignments int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM enterprise_key_assignments
				WHERE api_key_id = $1 AND status = 'active'
			`, fixture.apiKeyID).Scan(&activeAssignments))
			require.Equal(t, 1, activeAssignments)

			cfg := &config.Config{RunMode: tc.runMode}
			apiKeyService := service.NewAPIKeyService(
				newAPIKeyRepositoryWithSQL(integrationEntClient, integrationDB),
				NewUserRepository(integrationEntClient, integrationDB),
				NewGroupRepository(integrationEntClient, integrationDB),
				NewUserSubscriptionRepository(integrationEntClient),
				nil,
				nil,
				cfg,
			)
			var subscriptionService *service.SubscriptionService
			if tc.withSubscriptionService {
				subscriptionService = service.NewSubscriptionService(
					NewGroupRepository(integrationEntClient, integrationDB),
					NewUserSubscriptionRepository(integrationEntClient),
					nil,
					integrationEntClient,
					nil,
				)
				t.Cleanup(subscriptionService.Stop)
			}

			var handlerCalls atomic.Int32
			router := gin.New()
			router.Use(gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(apiKeyService, subscriptionService, cfg)))
			router.GET(tc.path, func(c *gin.Context) {
				handlerCalls.Add(1)
				c.Status(http.StatusNoContent)
			})

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tc.path, nil).WithContext(ctx)
			request.Header.Set("x-api-key", credential)
			router.ServeHTTP(response, request)

			require.NotEqual(t, http.StatusServiceUnavailable, response.Code)
			require.Equal(t, http.StatusNoContent, response.Code)
			require.Equal(t, int32(1), handlerCalls.Load())
		})
	}
}
