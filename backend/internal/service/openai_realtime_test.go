package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
)

type realtimeTestFrameConn struct {
	frames []struct {
		messageType coderws.MessageType
		payload     []byte
	}
}

func (c *realtimeTestFrameConn) ReadFrame(context.Context) (coderws.MessageType, []byte, error) {
	if len(c.frames) == 0 {
		return coderws.MessageText, nil, io.EOF
	}
	frame := c.frames[0]
	c.frames = c.frames[1:]
	return frame.messageType, append([]byte(nil), frame.payload...), nil
}

func (c *realtimeTestFrameConn) WriteFrame(context.Context, coderws.MessageType, []byte) error {
	return nil
}

func (c *realtimeTestFrameConn) Close() error {
	return nil
}

var _ openaiwsv2.FrameConn = (*realtimeTestFrameConn)(nil)

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

func TestRealtimeModelBoundFrameConnAuditsValidatedClientFrames(t *testing.T) {
	t.Parallel()

	frames := &realtimeTestFrameConn{frames: []struct {
		messageType coderws.MessageType
		payload     []byte
	}{
		{messageType: coderws.MessageText, payload: []byte(`{"type":"input_audio_buffer.append","audio":"not-recorded"}`)},
		{messageType: coderws.MessageText, payload: []byte(`{"type":"response.create","model":"gpt-realtime"}`)},
	}}
	var audited [][]byte
	conn := realtimeModelBoundFrameConn{
		upstream: frames,
		model:    "gpt-realtime",
		beforeClientForward: func(_ context.Context, payload []byte) error {
			audited = append(audited, append([]byte(nil), payload...))
			return nil
		},
	}

	for range 2 {
		messageType, payload, err := conn.ReadFrame(context.Background())
		require.NoError(t, err)
		require.Equal(t, coderws.MessageText, messageType)
		require.NotEmpty(t, payload)
	}
	require.Len(t, audited, 2)
}

func TestRealtimeModelBoundFrameConnStopsRejectedClientFrame(t *testing.T) {
	t.Parallel()

	rejected := errors.New("security audit rejected frame")
	conn := realtimeModelBoundFrameConn{
		upstream: &realtimeTestFrameConn{frames: []struct {
			messageType coderws.MessageType
			payload     []byte
		}{
			{messageType: coderws.MessageText, payload: []byte(`{"type":"response.create","model":"gpt-realtime"}`)},
		}},
		model: "gpt-realtime",
		beforeClientForward: func(context.Context, []byte) error {
			return rejected
		},
	}

	messageType, payload, err := conn.ReadFrame(context.Background())
	require.Equal(t, coderws.MessageText, messageType)
	require.Nil(t, payload)
	require.ErrorIs(t, err, rejected)
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
