package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadEnterpriseEmployeeWeeklyAllocationOverage(t *testing.T) {
	t.Run("defaults to ten cents", func(t *testing.T) {
		resetViperWithJWTSecret(t)
		cfg, err := Load()
		require.NoError(t, err)
		require.InDelta(t, 0.10, cfg.Billing.EnterpriseEmployeeWeeklyAllocationOverageUSD, 0.000000001)
	})

	t.Run("environment overrides YAML", func(t *testing.T) {
		resetViperWithJWTSecret(t)
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte("billing:\n  enterprise_employee_weekly_allocation_overage_usd: 0.05\n"), 0o600))
		t.Setenv("CONFIG_FILE", path)
		t.Setenv("BILLING_ENTERPRISE_EMPLOYEE_WEEKLY_ALLOCATION_OVERAGE_USD", "0.08")

		cfg, err := Load()
		require.NoError(t, err)
		require.InDelta(t, 0.08, cfg.Billing.EnterpriseEmployeeWeeklyAllocationOverageUSD, 0.000000001)
	})

	t.Run("rejects negative overage", func(t *testing.T) {
		resetViperWithJWTSecret(t)
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte("billing:\n  enterprise_employee_weekly_allocation_overage_usd: -0.01\n"), 0o600))
		t.Setenv("CONFIG_FILE", path)

		_, err := Load()
		require.ErrorContains(t, err, "billing.enterprise_employee_weekly_allocation_overage_usd")
	})
}
