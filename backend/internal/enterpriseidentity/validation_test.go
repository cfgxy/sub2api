package enterpriseidentity

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestHostIgnoresForwardedHost(t *testing.T) {
	req := httptest.NewRequest("GET", "https://acme.example.com/api/v1/enterprise/sessions", nil)
	req.Header.Set("X-Forwarded-Host", "other.example.com")
	require.Equal(t, "acme.example.com", requestHost(req))
}

func TestValidateBrandRejectsMarkupAndUnsafeBackground(t *testing.T) {
	tests := []BrandInput{
		{Title: "<script>alert(1)</script>"},
		{Body: "<svg onload=alert(1)>"},
		{Slogan: "<b>unsafe</b>"},
		{BackgroundURL: "javascript:alert(1)", BackgroundContentType: "image/jpeg", BackgroundSHA256: strings.Repeat("a", 64), BackgroundSize: 1},
		{BackgroundURL: "https://cdn.example.com/background.svg", BackgroundContentType: "image/svg+xml", BackgroundSHA256: strings.Repeat("a", 64), BackgroundSize: 1},
		{BackgroundURL: "https://cdn.example.com/background.jpg", BackgroundContentType: "image/jpeg", BackgroundSHA256: strings.Repeat("a", 64), BackgroundSize: maxBrandBackgroundBytes + 1},
		{BackgroundURL: "https://cdn.example.com/background.jpg", BackgroundContentType: "image/jpeg", BackgroundSize: 1},
	}
	for _, input := range tests {
		require.Error(t, validateBrand(input), "%+v", input)
	}

	require.NoError(t, validateBrand(BrandInput{
		Title:                 "Acme",
		Body:                  "Enterprise portal",
		Slogan:                "Build faster",
		BackgroundURL:         publicBrandBackgroundURL,
		BackgroundContentType: "image/webp",
		BackgroundSHA256:      strings.Repeat("a", 64),
		BackgroundSize:        maxBrandBackgroundBytes,
	}))
}

func TestValidateBrandLengthLimits(t *testing.T) {
	require.Error(t, validateBrand(BrandInput{Title: string(make([]byte, 41))}))
	require.Error(t, validateBrand(BrandInput{Body: string(make([]byte, 121))}))
	require.Error(t, validateBrand(BrandInput{Slogan: string(make([]byte, 61))}))
}
