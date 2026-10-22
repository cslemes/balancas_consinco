package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"balancas/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_FromTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	tomlContent := `
[sftp]
url = "sftp://example.com:2323"
user = "testuser"
password = "secretpassword"
timeout_seconds = 45
max_retries = 5

[sync]
local_base_dir = "C:\\balancas"
remote_base_dir = "."
folder_pattern = "(?i)^bal(\\d+)$"
interval_seconds = 600
mtime_tolerance_seconds = 3

[logging]
log_file = "logs/sync.log"
level = "debug"
max_size_mb = 20
max_backups = 10
`
	err := os.WriteFile(configFile, []byte(tomlContent), 0644)
	require.NoError(t, err)

	cfg, err := config.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Equal(t, "example.com", cfg.SFTP.Host)
	assert.Equal(t, 2323, cfg.SFTP.Port)
	assert.Equal(t, "testuser", cfg.SFTP.User)
	assert.Equal(t, "secretpassword", cfg.SFTP.Password)
	assert.Equal(t, 45, cfg.SFTP.TimeoutSeconds)
	assert.Equal(t, 5, cfg.SFTP.MaxRetries)

	assert.Equal(t, `C:\balancas`, cfg.Sync.LocalBaseDir)
	assert.Equal(t, ".", cfg.Sync.RemoteBaseDir)
	assert.Equal(t, `(?i)^bal(\d+)$`, cfg.Sync.FolderPattern)
	assert.Equal(t, 600, cfg.Sync.IntervalSeconds)
	assert.Equal(t, 3, cfg.Sync.MtimeToleranceSeconds)

	assert.Equal(t, "logs/sync.log", cfg.Logging.LogFile)
	assert.Equal(t, "debug", cfg.Logging.Level)
}

func TestLoadConfig_Defaults(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	err := os.WriteFile(configFile, []byte(""), 0644)
	require.NoError(t, err)

	cfg, err := config.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Equal(t, 22, cfg.SFTP.Port)
	assert.Equal(t, 30, cfg.SFTP.TimeoutSeconds)
	assert.Equal(t, 3, cfg.SFTP.MaxRetries)

	assert.Equal(t, `C:\balancas`, cfg.Sync.LocalBaseDir)
	assert.Equal(t, ".", cfg.Sync.RemoteBaseDir)
	assert.Equal(t, `(?i)^bal(\d+)$`, cfg.Sync.FolderPattern)
	assert.Equal(t, 300, cfg.Sync.IntervalSeconds)
	assert.Equal(t, 2, cfg.Sync.MtimeToleranceSeconds)
	assert.False(t, cfg.Sync.DryRun)

	assert.Equal(t, "logs/sync_balancas.log", cfg.Logging.LogFile)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, 10, cfg.Logging.MaxSizeMB)
	assert.Equal(t, 5, cfg.Logging.MaxBackups)
}

func TestLoadConfig_URLWithCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	tomlContent := `
[sftp]
url = "sftp://sftpuser:sftppass@sftp.example.com:2222"
`
	err := os.WriteFile(configFile, []byte(tomlContent), 0644)
	require.NoError(t, err)

	cfg, err := config.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Equal(t, "sftp.example.com", cfg.SFTP.Host)
	assert.Equal(t, 2222, cfg.SFTP.Port)
	assert.Equal(t, "sftpuser", cfg.SFTP.User)
	assert.Equal(t, "sftppass", cfg.SFTP.Password)
}

func TestLoadConfig_RepoConfigTOML(t *testing.T) {
	// Test loading the project root config.toml
	cfg, err := config.LoadConfig("../../config.toml")
	require.NoError(t, err)

	assert.Equal(t, "supermercados212430.consinco.cloudtotvs.com.br", cfg.SFTP.Host)
	assert.Equal(t, 2323, cfg.SFTP.Port)
	assert.Equal(t, "ftp_prod_CK5ETD", cfg.SFTP.User)
	assert.Equal(t, "sua_senha_aqui", cfg.SFTP.Password)
	assert.Equal(t, 30, cfg.SFTP.TimeoutSeconds)
	assert.Equal(t, 3, cfg.SFTP.MaxRetries)

	assert.Equal(t, `C:\balancas`, cfg.Sync.LocalBaseDir)
	assert.Equal(t, ".", cfg.Sync.RemoteBaseDir)
	assert.Equal(t, `(?i)^bal(\d+)$`, cfg.Sync.FolderPattern)
	assert.Equal(t, 300, cfg.Sync.IntervalSeconds)
	assert.Equal(t, 2, cfg.Sync.MtimeToleranceSeconds)

	assert.Equal(t, "logs/sync_balancas.log", cfg.Logging.LogFile)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, 10, cfg.Logging.MaxSizeMB)
	assert.Equal(t, 5, cfg.Logging.MaxBackups)
}

func TestLoadConfig_EnvOverride(t *testing.T) {
	t.Setenv("BALANCAS_SFTP_HOST", "envhost.local")
	t.Setenv("BALANCAS_SYNC_INTERVAL_SECONDS", "120")

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	err := os.WriteFile(configFile, []byte(""), 0644)
	require.NoError(t, err)

	cfg, err := config.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Equal(t, "envhost.local", cfg.SFTP.Host)
	assert.Equal(t, 120, cfg.Sync.IntervalSeconds)
}

func TestLoadConfig_InvalidTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	err := os.WriteFile(configFile, []byte("[invalid toml"), 0644)
	require.NoError(t, err)

	_, err = config.LoadConfig(configFile)
	require.Error(t, err)
}

func TestLoadConfig_ExplicitFileNotFound(t *testing.T) {
	_, err := config.LoadConfig(filepath.Join(t.TempDir(), "missing_config.toml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erro ao ler arquivo de configuração")
}

func TestLoadConfig_AutoDiscoveryEmptyPath(t *testing.T) {
	cfg, err := config.LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotEmpty(t, cfg.Sync.LocalBaseDir)
}
