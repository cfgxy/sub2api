package enterprise

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMaskEmployeeKeyNeverReturnsPlaintext(t *testing.T) {
	plain := "sk-" + strings.Repeat("a", 64)
	masked := MaskEmployeeKey(plain)
	require.NotEqual(t, plain, masked)
	require.NotContains(t, masked, strings.Repeat("a", 16))
	require.Equal(t, "sk-aaa...aaaa", masked)
	require.Equal(t, "********", MaskEmployeeKey("short"))
}

func TestEmployeeKeyRequestHashBindsOperationAndExpectedKey(t *testing.T) {
	base := EmployeeKeyRequestHash("rotate", 42)
	require.Len(t, base, 64)
	require.NotEqual(t, base, EmployeeKeyRequestHash("disable", 42))
	require.NotEqual(t, base, EmployeeKeyRequestHash("rotate", 43))
}

func TestNormalizeEmployeeKeyPresentationUsesGatewayExpiryAndWindowRules(t *testing.T) {
	now := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(-time.Second)
	window5h := now.Add(-5*time.Hour - time.Second)
	window1d := now.Add(-24*time.Hour - time.Second)
	window7d := now.Add(-7*24*time.Hour - time.Second)
	key := EmployeeKey{
		Status: "active", ExpiresAt: &expiresAt,
		Usage5h: 1.25, Usage1d: 2.5, Usage7d: 6.75,
		Window5h: &window5h, Window1d: &window1d, Window7d: &window7d,
	}

	normalizeEmployeeKeyPresentation(&key, now)

	require.Equal(t, "expired", key.Status)
	require.Zero(t, key.Usage5h)
	require.Zero(t, key.Usage1d)
	require.Zero(t, key.Usage7d)
	require.Nil(t, key.Window5h)
	require.Nil(t, key.Window1d)
	require.Nil(t, key.Window7d)
	require.Nil(t, key.Reset5h)
	require.Nil(t, key.Reset1d)
	require.Nil(t, key.Reset7d)
}
