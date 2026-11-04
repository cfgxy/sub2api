package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type realtimeCallTerminatorStub struct {
	events []string
}

func (s *realtimeCallTerminatorStub) ControlOfficialRealtimeCall(context.Context, *service.Account, string, string, string) error {
	s.events = append(s.events, "hangup")
	return nil
}

func (s *realtimeCallTerminatorStub) FinalizeOfficialRealtimeCall(*service.RealtimeCallRecord) {
	s.events = append(s.events, "finalize")
}

func TestOfficialRealtimeSidebandLocation(t *testing.T) {
	require.Equal(t, "/v1/realtime/calls/call_123", officialRealtimeSidebandLocation("call_123"))
	require.Equal(t, "/v1/realtime/calls/call%2F123", officialRealtimeSidebandLocation("call/123"))
}

func TestRealtimeResponseCreateFrame(t *testing.T) {
	require.True(t, realtimeResponseCreateFrame([]byte(`{"type":"response.create"}`)))
	require.False(t, realtimeResponseCreateFrame([]byte(`{"type":"session.update"}`)))
	require.False(t, realtimeResponseCreateFrame([]byte(`not-json`)))
}

func TestRealtimeWSEmployeeTurnBindingsKeepVADAndExplicitResponsesIndependent(t *testing.T) {
	bindings := &realtimeWSEmployeeTurnBindings{}
	explicitReservation := &service.InflightReservation{}
	vadReservation := &service.InflightReservation{}

	// server_vad.create_response may emit its response.created before an earlier
	// client response.create finishes. Provider IDs, never a pending FIFO, own
	// the reservation mapping.
	require.True(t, bindings.bindReservation("response-vad", vadReservation))
	require.True(t, bindings.bindReservation("response-explicit", explicitReservation))
	require.Same(t, explicitReservation, bindings.take("response-explicit"))
	require.Same(t, vadReservation, bindings.take("response-vad"))
}

func TestRealtimeWSEmployeeTurnBindingsBindPreflightBeforeResponseCreated(t *testing.T) {
	bindings := &realtimeWSEmployeeTurnBindings{}
	reservation := &service.InflightReservation{}
	require.True(t, bindings.prepare(reservation))
	require.True(t, bindings.hasPrepared())

	bound, ok := bindings.bindPrepared("response-1")
	require.True(t, ok)
	require.Same(t, reservation, bound)
	require.False(t, bindings.hasPrepared())
	require.Same(t, reservation, bindings.take("response-1"))
}

func TestRealtimeCallIdentityFreezesAuthenticatedFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/v1/realtime/calls", nil)
	request.Header.Set("User-Agent", "realtime-test")
	request.RemoteAddr = "203.0.113.10:4321"
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	groupID := int64(7)
	subscriptionID := int64(11)
	at := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	identity := realtimeCallIdentity(context, &service.APIKey{
		ID:      3,
		UserID:  5,
		GroupID: &groupID,
	}, 5, &service.UserSubscription{ID: subscriptionID}, "gpt-realtime", at)

	require.Equal(t, int64(3), identity.APIKeyID)
	require.Equal(t, int64(5), identity.UserID)
	require.Equal(t, &groupID, identity.GroupID)
	require.Equal(t, &subscriptionID, identity.SubscriptionID)
	require.Equal(t, "gpt-realtime", identity.Model)
	require.Equal(t, at, identity.PricingAt)
	require.Equal(t, "realtime-test", identity.UserAgent)
	require.Equal(t, "203.0.113.10", identity.IPAddress)
	require.Equal(t, "/v1/realtime/calls", identity.InboundEndpoint)
}

func TestVerifyOfficialRealtimeWebhook(t *testing.T) {
	key := []byte("realtime-webhook-test-key")
	secret := "whsec_" + base64.StdEncoding.EncodeToString(key)
	webhookID := "evt_123"
	timestamp := "1793793600"
	body := []byte(`{"type":"realtime.call.incoming","data":{"call_id":"call_123"}}`)
	mac := hmac.New(sha256.New, key)
	_, err := mac.Write([]byte(webhookID + "." + timestamp + "."))
	require.NoError(t, err)
	_, err = mac.Write(body)
	require.NoError(t, err)
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	now := time.Unix(1793793600, 0)

	require.True(t, verifyOfficialRealtimeWebhook(secret, webhookID, timestamp, []string{"v1,invalid", "v1," + signature}, body, 5*time.Minute, now))
	require.True(t, verifyOfficialRealtimeWebhook(secret, webhookID, timestamp, []string{"v1,invalid v1," + signature}, body, 5*time.Minute, now))
	require.True(t, verifyOfficialRealtimeWebhook(string(key), webhookID, timestamp, []string{"v1," + signature}, body, 5*time.Minute, now))
	require.False(t, verifyOfficialRealtimeWebhook(secret, webhookID, timestamp, []string{"v1," + signature}, append(body, 'x'), 5*time.Minute, now))
	require.False(t, verifyOfficialRealtimeWebhook(secret, webhookID, timestamp, []string{"v1," + signature}, body, 5*time.Minute, now.Add(5*time.Minute+time.Second)))
	require.False(t, verifyOfficialRealtimeWebhook(secret, webhookID, timestamp, []string{"v1," + signature}, body, 5*time.Minute, now.Add(-5*time.Minute-time.Second)))
}

func TestCleanupAcceptedRealtimeCallAfterWebhookCommitFailureHangsUpBeforeFinalizing(t *testing.T) {
	stub := &realtimeCallTerminatorStub{}
	cleanupAcceptedRealtimeCallAfterWebhookCommitFailure(stub, &service.Account{ID: 1}, &service.RealtimeCallRecord{CallID: "call"})
	require.Equal(t, []string{"hangup", "finalize"}, stub.events)
}
