package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetConfig(t *testing.T) {
	cfg := InitTestProvider(t)
	assert.NotEqual(t, Config{}, *cfg)
}

func TestLoadConfig_IsolatedInstances(t *testing.T) {
	// YOZORA (fork): test/config.toml is optional (gitignored, holds provider
	// credentials). Without it, LoadConfig fails by design — skip rather than
	// failing environments without the config.
	if _, err := os.Stat(filepath.Join(ProjectRoot(), "test", "config.toml")); os.IsNotExist(err) {
		t.Skip("test/config.toml not present — LoadConfig-isolation assertions need it")
	}

	first := LoadConfig(t)
	second := LoadConfig(t)

	assert.NotSame(t, first, second)
	assert.Equal(t, *first, *second)

	first.Path.DataDir = t.TempDir()
	assert.NotEqual(t, first.Path.DataDir, second.Path.DataDir)
}

func TestInitTestProvider_DefaultsWithoutConfig(t *testing.T) {
	t.Setenv("TEST_CONFIG_PATH", t.TempDir())

	cfg := InitTestProvider(t)

	assert.NotNil(t, cfg)
	assert.Equal(t, defaultTestDatabaseName, cfg.Database.Name)
	assert.Empty(t, cfg.Path.DataDir)
	assert.False(t, cfg.Flags.EnableAnilistTests)
}
