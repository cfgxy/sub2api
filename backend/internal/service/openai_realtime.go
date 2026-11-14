package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
)

const realtimeUpstreamBodyLimit = 1 << 20

var ErrRealtimeUnavailable = errors.New("official realtime is unavailable")

// RealtimeCallRequest is the public Platform Calls API request. Session is
// passed through unchanged because its fields are owned by OpenAI's protocol.
type RealtimeCallRequest struct {
	SDP     string          `json:"sdp"`
	Session json.RawMessage `json:"session"`
}

type RealtimeCallCreated struct {
	SDP        []byte
	CallID     string
	Location   string
	StatusCode int
}

type realtimeFrameConn struct {
	conn *coderws.Conn
}

func (c realtimeFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	return c.conn.Read(ctx)
}

func (c realtimeFrameConn) WriteFrame(ctx context.Context, messageType coderws.MessageType, payload []byte) error {
	return c.conn.Write(ctx, messageType, payload)
}

func (c realtimeFrameConn) Close() error {
	return c.conn.Close(coderws.StatusNormalClosure, "")
}

// realtimeModelBoundFrameConn keeps every model-bearing client event bound to
// the model that passed group policy and account scheduling. Realtime permits
// both response.create.model and session.update.session.model, so validating
// only the HTTP query would allow a client to switch models after admission.
type realtimeModelBoundFrameConn struct {
	upstream            openaiwsv2.FrameConn
	model               string
	beforeClientForward func(ctx context.Context, payload []byte) error
}

func (c realtimeModelBoundFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	messageType, payload, err := c.upstream.ReadFrame(ctx)
	if err != nil || messageType != coderws.MessageText {
		return messageType, payload, err
	}
	if err := ValidateRealtimeClientFrameModel(payload, c.model); err != nil {
		return messageType, nil, err
	}
	if c.beforeClientForward != nil {
		if err := c.beforeClientForward(ctx, payload); err != nil {
			return messageType, nil, err
		}
	}
	return messageType, payload, nil
}

func (c realtimeModelBoundFrameConn) WriteFrame(ctx context.Context, messageType coderws.MessageType, payload []byte) error {
	return c.upstream.WriteFrame(ctx, messageType, payload)
}

func (c realtimeModelBoundFrameConn) Close() error {
	return c.upstream.Close()
}

func ValidateRealtimeCallRequest(request *RealtimeCallRequest) error {
	if request == nil || strings.TrimSpace(request.SDP) == "" {
		return errors.New("sdp is required")
	}
	if !json.Valid(request.Session) || len(bytes.TrimSpace(request.Session)) == 0 {
		return errors.New("session must be a JSON object")
	}
	if !bytes.HasPrefix(bytes.TrimSpace(request.Session), []byte("{")) {
		return errors.New("session must be a JSON object")
	}
	return nil
}

// ValidateRealtimeClientFrameModel rejects a client-side model override. Empty
// model fields are valid: OpenAI then retains the already-configured session
// model. The caller must have independently required a non-empty admitted model.
func ValidateRealtimeClientFrameModel(payload []byte, admittedModel string) error {
	admittedModel = strings.TrimSpace(admittedModel)
	if admittedModel == "" {
		return errors.New("realtime model is required")
	}
	if !json.Valid(payload) {
		return errors.New("realtime client frame must be valid JSON")
	}
	var frame struct {
		Model   string `json:"model"`
		Session struct {
			Model string `json:"model"`
		} `json:"session"`
	}
	if err := json.Unmarshal(payload, &frame); err != nil {
		return errors.New("realtime client frame must be a JSON object")
	}
	for _, model := range []string{frame.Model, frame.Session.Model} {
		if model != "" && strings.TrimSpace(model) != admittedModel {
			return errors.New("realtime model must match the model query parameter")
		}
	}
	return nil
}

func officialRealtimeBaseURL(account *Account) (*url.URL, error) {
	if account == nil || !account.IsOpenAIApiKey() || !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityRealtime) {
		return nil, ErrRealtimeUnavailable
	}
	baseURL := strings.TrimSpace(account.GetOpenAIBaseURL())
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid official realtime base URL")
	}
	return parsed, nil
}

func officialRealtimeEndpoint(account *Account, websocket bool, suffix string) (string, error) {
	base, err := officialRealtimeBaseURL(account)
	if err != nil {
		return "", err
	}
	if websocket {
		switch base.Scheme {
		case "https":
			base.Scheme = "wss"
		case "http":
			base.Scheme = "ws"
		default:
			return "", errors.New("official realtime WebSocket requires http or https upstream")
		}
	}
	base.Path = realtimeUpstreamPath(base.Path, suffix)
	base.RawPath = ""
	return base.String(), nil
}

func realtimeUpstreamPath(basePath, suffix string) string {
	basePath = strings.TrimSuffix(strings.TrimSpace(basePath), "/")
	if basePath == "/v1" {
		return "/v1/realtime" + suffix
	}
	return path.Join(basePath, "/v1/realtime"+suffix)
}

// ProxyOfficialRealtimeWebSocket proxies raw official Realtime frames. The
// caller owns authentication, admission, and billing; this method only holds
// the server-side OpenAI key while relaying client/upstream traffic.
func (s *OpenAIGatewayService) ProxyOfficialRealtimeWebSocket(
	ctx context.Context,
	client *coderws.Conn,
	account *Account,
	model string,
	beforeClientFrame func(ctx context.Context, payload []byte) error,
	onTurnComplete func(openaiwsv2.RelayTurnResult),
) error {
	if client == nil {
		return errors.New("realtime client websocket is nil")
	}
	endpoint, err := officialRealtimeEndpoint(account, true, "")
	if err != nil {
		return err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	query := parsed.Query()
	query.Set("model", model)
	parsed.RawQuery = query.Encode()
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil || strings.TrimSpace(token) == "" {
		return fmt.Errorf("get official realtime API key: %w", err)
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	upstream, status, _, err := s.getOpenAIWSPassthroughDialer().Dial(ctx, parsed.String(), headers, resolveAccountProxyURL(account))
	if err != nil {
		return fmt.Errorf("dial official realtime upstream (status %d): %w", status, err)
	}
	defer upstream.Close()
	upstreamFrames, ok := upstream.(openaiwsv2.FrameConn)
	if !ok {
		return errors.New("official realtime transport does not support raw frames")
	}
	clientFrames := realtimeModelBoundFrameConn{
		upstream:            realtimeFrameConn{conn: client},
		model:               model,
		beforeClientForward: beforeClientFrame,
	}
	_, relayExit := openaiwsv2.Relay(ctx, clientFrames, upstreamFrames, nil, openaiwsv2.RelayOptions{
		StartClientAfterFirstDownstream: true,
		OnTurnComplete:                  onTurnComplete,
	})
	if relayExit != nil && relayExit.Err != nil {
		return relayExit.Err
	}
	return nil
}

// CreateOfficialRealtimeCall proxies an SDP offer to the public Calls API.
// It intentionally does not issue a client secret or expose the selected
// account key.
func (s *OpenAIGatewayService) CreateOfficialRealtimeCall(
	ctx context.Context,
	account *Account,
	request *RealtimeCallRequest,
) (*RealtimeCallCreated, error) {
	if err := ValidateRealtimeCallRequest(request); err != nil {
		return nil, err
	}
	endpoint, err := officialRealtimeEndpoint(account, false, "/calls")
	if err != nil {
		return nil, err
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("get official realtime API key: %w", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("sdp", request.SDP); err != nil {
		return nil, err
	}
	if err := writer.WriteField("session", string(request.Session)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	reqCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI))
	upstreamReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, err
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
	upstreamReq.Header.Set("Content-Type", writer.FormDataContentType())
	upstreamReq.Header.Set("Accept", "application/sdp")
	response, err := s.doOpenAIUpstream(upstreamReq, resolveAccountProxyURL(account), account)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, realtimeUpstreamBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(responseBody) > realtimeUpstreamBodyLimit {
		return nil, errors.New("official realtime call response is too large")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &UpstreamFailoverError{StatusCode: response.StatusCode, ResponseBody: responseBody, ResponseHeaders: response.Header.Clone()}
	}
	location := strings.TrimSpace(response.Header.Get("Location"))
	callID, err := realtimeCallIDFromLocation(location)
	if err != nil {
		return nil, err
	}
	return &RealtimeCallCreated{SDP: responseBody, CallID: callID, Location: location, StatusCode: response.StatusCode}, nil
}

func realtimeCallIDFromLocation(location string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(location))
	if err != nil || strings.TrimSpace(parsed.Path) == "" {
		return "", errors.New("official realtime Calls response has no valid Location")
	}
	callID := strings.TrimSpace(path.Base(strings.TrimSuffix(parsed.Path, "/")))
	if callID == "" || callID == "." || callID == "/" {
		return "", errors.New("official realtime Calls Location has no call id")
	}
	return callID, nil
}
