package enterpriseidentity

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestEnterpriseDeploymentSecretContract(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))

	compose, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "docker-compose.shan-152.yml"))
	require.NoError(t, err)
	content := string(compose)
	jwtContract := "JWT_SECRET: ${SHAN152_JWT_SECRET:?SHAN152_JWT_SECRET is required}"
	totpContract := "TOTP_ENCRYPTION_KEY: ${SHAN152_TOTP_ENCRYPTION_KEY:?SHAN152_TOTP_ENCRYPTION_KEY is required}"
	var config struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(compose, &config))
	appEnvironment := config.Services["app"].Environment
	require.Equal(t, "${SHAN152_JWT_SECRET:?SHAN152_JWT_SECRET is required}", appEnvironment["JWT_SECRET"])
	require.Equal(t, "${SHAN152_TOTP_ENCRYPTION_KEY:?SHAN152_TOTP_ENCRYPTION_KEY is required}", appEnvironment["TOTP_ENCRYPTION_KEY"])
	require.Equal(t, 1, strings.Count(content, jwtContract))
	require.Equal(t, 1, strings.Count(content, totpContract))
	require.Contains(t, content, "name: shan-152-enterprise-postgres")
	require.Contains(t, content, "name: shan-152-enterprise-redis")

	composePath := filepath.Join(repoRoot, "deploy", "docker-compose.shan-152.yml")
	t.Run("all required secrets resolve", func(t *testing.T) {
		output, err := runComposeConfig(composePath, "SHAN152_JWT_SECRET", "SHAN152_TOTP_ENCRYPTION_KEY")
		require.NoError(t, err, string(output))
	})
	t.Run("missing JWT secret fails closed", func(t *testing.T) {
		output, err := runComposeConfig(composePath, "SHAN152_TOTP_ENCRYPTION_KEY")
		require.Error(t, err)
		require.Contains(t, string(output), "SHAN152_JWT_SECRET is required")
	})
	t.Run("missing encryption key fails closed", func(t *testing.T) {
		output, err := runComposeConfig(composePath, "SHAN152_JWT_SECRET")
		require.Error(t, err)
		require.Contains(t, string(output), "SHAN152_TOTP_ENCRYPTION_KEY is required")
	})

	documentation, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "SHAN-152-SECURITY.md"))
	require.NoError(t, err)
	doc := strings.ToLower(string(documentation))
	for _, required := range []string{
		"security_secrets.jwt_secret",
		"persisted value wins",
		"managed secret store",
		"startup fails closed",
		"secretaccesskey",
		"aes-256-gcm",
	} {
		require.Contains(t, doc, required)
	}
}

func runComposeConfig(composePath string, presentSecrets ...string) ([]byte, error) {
	const fixture = "contract-test-only-value"
	excluded := map[string]struct{}{
		"SHAN152_ADMIN_PASSWORD":      {},
		"SHAN152_JWT_SECRET":          {},
		"SHAN152_POSTGRES_PASSWORD":   {},
		"SHAN152_TOTP_ENCRYPTION_KEY": {},
	}
	environment := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, found := excluded[name]; !found {
			environment = append(environment, entry)
		}
	}
	environment = append(environment,
		"SHAN152_ADMIN_PASSWORD="+fixture,
		"SHAN152_POSTGRES_PASSWORD="+fixture,
	)
	for _, name := range presentSecrets {
		environment = append(environment, name+"="+fixture)
	}

	cmd := exec.Command("docker", "compose", "-f", composePath, "config", "--quiet")
	cmd.Env = environment
	return cmd.CombinedOutput()
}
