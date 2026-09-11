//go:build unit

package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestS3ImageStorageLoadUsesAuthenticatedClient(t *testing.T) {
	payload := []byte("stored image")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/brand-assets/enterprises/7/branding/background.png", r.URL.Path)
		require.NotEmpty(t, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "image/png")
		_, err := w.Write(payload)
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	storage, err := NewS3ImageStorage(context.Background(), &config.ImageStorageConfig{
		Endpoint:        server.URL,
		Region:          "auto",
		Bucket:          "brand-assets",
		AccessKeyID:     "test-ak",
		SecretAccessKey: "test-sk",
		ForcePathStyle:  true,
	})
	require.NoError(t, err)

	data, contentType, err := storage.Load(context.Background(), "enterprises/7/branding/background.png")
	require.NoError(t, err)
	require.Equal(t, payload, data)
	require.Equal(t, "image/png", contentType)
}

func TestS3ImageStorageLoadPropagatesReadFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "short")
	}))
	t.Cleanup(server.Close)

	storage, err := NewS3ImageStorage(context.Background(), &config.ImageStorageConfig{
		Endpoint:        server.URL,
		Region:          "auto",
		Bucket:          "brand-assets",
		AccessKeyID:     "test-ak",
		SecretAccessKey: "test-sk",
		ForcePathStyle:  true,
	})
	require.NoError(t, err)

	_, _, err = storage.Load(context.Background(), "enterprises/7/branding/background.png")
	require.Error(t, err)
}
