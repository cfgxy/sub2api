package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfficialRealtimeCapabilityRequiresOpenAIAPIKey(t *testing.T) {
	t.Parallel()

	apiKey := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":                      "test-key",
			"openai_endpoint_capabilities": []any{"realtime"},
		},
	}
	require.True(t, apiKey.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityRealtime))

	oauth := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	require.False(t, oauth.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityRealtime))

	grok := &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
	require.False(t, grok.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityRealtime))
}

func TestOfficialRealtimeEndpointUsesPublicAPIPath(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":                      "test-key",
			"openai_endpoint_capabilities": []any{"realtime"},
			"base_url":                     "https://api.openai.com/v1",
		},
	}

	wsEndpoint, err := officialRealtimeEndpoint(account, true, "")
	require.NoError(t, err)
	require.Equal(t, "wss://api.openai.com/v1/realtime", wsEndpoint)

	callsEndpoint, err := officialRealtimeEndpoint(account, false, "/calls")
	require.NoError(t, err)
	require.Equal(t, "https://api.openai.com/v1/realtime/calls", callsEndpoint)
}

func TestValidateRealtimeCallRequest(t *testing.T) {
	t.Parallel()
	validSession, err := json.Marshal(map[string]string{"model": "gpt-realtime"})
	require.NoError(t, err)
	require.NoError(t, ValidateRealtimeCallRequest(&RealtimeCallRequest{SDP: "v=0", Session: validSession}))
	require.Error(t, ValidateRealtimeCallRequest(&RealtimeCallRequest{Session: validSession}))
	require.Error(t, ValidateRealtimeCallRequest(&RealtimeCallRequest{SDP: "v=0", Session: json.RawMessage(`[]`)}))
}

func TestValidateRealtimeClientFrameModel(t *testing.T) {
	t.Parallel()

	for _, payload := range []string{
		`{"type":"session.update","session":{"model":"gpt-realtime"}}`,
		`{"type":"response.create","model":"gpt-realtime"}`,
		`{"type":"input_audio_buffer.append","audio":"..."}`,
	} {
		require.NoError(t, ValidateRealtimeClientFrameModel([]byte(payload), "gpt-realtime"))
	}
	require.Error(t, ValidateRealtimeClientFrameModel(
		[]byte(`{"type":"session.update","session":{"model":"gpt-realtime-other"}}`),
		"gpt-realtime",
	))
	require.Error(t, ValidateRealtimeClientFrameModel(
		[]byte(`{"type":"response.create","model":"gpt-realtime-other"}`),
		"gpt-realtime",
	))
	require.Error(t, ValidateRealtimeClientFrameModel([]byte(`not json`), "gpt-realtime"))
}

func TestRealtimeRequestType(t *testing.T) {
	t.Parallel()
	require.True(t, RequestTypeRealtime.IsValid())
	require.Equal(t, "realtime", RequestTypeRealtime.String())
	parsed, err := ParseUsageRequestType("realtime")
	require.NoError(t, err)
	require.Equal(t, RequestTypeRealtime, parsed)
}

func TestRealtimeCallIDFromLocation(t *testing.T) {
	t.Parallel()
	callID, err := realtimeCallIDFromLocation("https://api.openai.com/v1/realtime/calls/call_123")
	require.NoError(t, err)
	require.Equal(t, "call_123", callID)
	_, err = realtimeCallIDFromLocation("")
	require.Error(t, err)
}
