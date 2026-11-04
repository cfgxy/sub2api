package openai_ws_v2

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseRealtimeTerminalEventPreservesSeparatedAudioUsage(t *testing.T) {
	startedAt := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	payload := []byte(`{"type":"response.done","response":{"id":"resp_123","model":"gpt-realtime","usage":{"input_tokens":12,"output_tokens":7,"input_token_details":{"text_tokens":4,"audio_tokens":8,"cached_tokens":2,"cached_tokens_details":{"text_tokens":0,"audio_tokens":2}},"output_token_details":{"text_tokens":3,"audio_tokens":4}}}}`)

	turn, ok := ParseRealtimeTerminalEvent(payload, startedAt, startedAt.Add(time.Second))

	require.True(t, ok)
	require.Equal(t, "resp_123", turn.RequestID)
	require.Equal(t, "gpt-realtime", turn.ResponseModel)
	require.Equal(t, "response.done", turn.TerminalEventType)
	require.Equal(t, 4, turn.Usage.InputTokens)
	require.Equal(t, 6, turn.Usage.AudioInputTokens)
	require.Equal(t, 2, turn.Usage.AudioCacheReadTokens)
	require.Equal(t, 3, turn.Usage.OutputTokens)
	require.Equal(t, 4, turn.Usage.AudioOutputTokens)
	require.True(t, turn.Usage.TokenBucketsSeparated)
}
