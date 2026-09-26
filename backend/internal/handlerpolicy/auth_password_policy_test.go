//go:build unit

package handlerpolicy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPlatformPasswordRequestsRequireEightCharacters(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		field      string
		newRequest func() any
	}{
		{name: "注册", path: "/api/v1/auth/register", field: "password", newRequest: func() any { return &handler.RegisterRequest{} }},
		{name: "重置", path: "/api/v1/auth/reset-password", field: "new_password", newRequest: func() any { return &handler.ResetPasswordRequest{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, password := range []string{"abcdefg", "abcdefgh"} {
				t.Run(password, func(t *testing.T) {
					body := fmt.Sprintf(`{"email":"user@example.com","token":"reset-token","%s":"%s"}`, tc.field, password)
					ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
					ctx.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))
					ctx.Request.Header.Set("Content-Type", "application/json")
					err := ctx.ShouldBindJSON(tc.newRequest())
					if len(password) == 7 {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
					}
				})
			}
		})
	}
}
