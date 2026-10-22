# Go Windows Service Balanças SFTP Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a production-ready, self-contained Windows Service in Go using Cobra CLI, Viper (with TOML configuration), and SFTP to synchronize Toledo/scale files from `BAL<xx>` remote folders to `C:\balancas\<xx>` local folders with atomic downloads and change detection.

**Architecture:** Pure Go modular service composed of Viper configuration loading (`config.toml`), Cobra CLI with service lifecycle subcommands, `kardianos/service` Windows SCM integration, `pkg/sftp` client with retries, folder mapping regex (`BAL<xx>` -> `<xx>`), incremental synchronization with atomic renaming and timestamp preservation.

**Tech Stack:** Go 1.27, `github.com/spf13/cobra`, `github.com/spf13/viper`, `github.com/kardianos/service`, `github.com/pkg/sftp`, `golang.org/x/crypto/ssh`, `github.com/stretchr/testify`.

**Spec:** `docs/superpowers/specs/2026-10-22-go-windows-service-sync-design.md`

## Global Constraints

- Remote folders matching pattern `(?i)^bal(\d+)$` must map to `<local_base_dir>\<xx>` (e.g. `BAL01` -> `C:\balancas\01`).
- Remote files on SFTP must NOT be deleted or moved; they must remain intact.
- Downloads must be atomic (written to `<filename>.<pid>.tmp` first, then renamed via `os.Rename`) to prevent scale software from reading incomplete files.
- Modification time (`mtime`) on local files must be updated to match the remote SFTP file `mtime` via `os.Chtimes`.
- Unchanged files (matching size in bytes and modification timestamp within tolerance) must be skipped without re-downloading.
- Windows file lock errors (`ERROR_SHARING_VIOLATION` / `os.ErrPermission`) must be caught and logged without halting execution for other files.
- Configuration must parse `config.toml` via Viper, supporting both unified `sftp.url` and discrete `host`/`port` fields, with environment variable overrides (`BALANCAS_`).
- Must cross-compile cleanly to a single standalone Windows executable `bin/balancas-sync.exe` without CGO (`CGO_ENABLED=0 GOOS=windows GOARCH=amd64`).

---

### Task 1: Go Module Initialization, Dependencies & Viper TOML Configuration

**Files:**
- Create: `go.mod`
- Create: `config.toml`
- Create: `config.toml.example`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `type Config struct` with sub-structs `SFTPConfig`, `SyncConfig`, `LoggingConfig`.
- Function: `LoadConfig(configPath string) (*Config, error)`

- [ ] **Step 1: Initialize go.mod and install dependencies**

Run:
```bash
go mod init balancas
go get github.com/spf13/viper@v1.19.0
go get github.com/stretchr/testify@v1.9.0
go get github.com/spf13/cobra@v1.8.1
go get github.com/kardianos/service@v1.2.2
go get github.com/pkg/sftp@v1.13.6
go get golang.org/x/crypto@v0.28.0
```

- [ ] **Step 2: Create config.toml and config.toml.example**

Create `config.toml`:
```toml
[sftp]
url = "sftp://supermercados212430.consinco.cloudtotvs.com.br:2323"
user = "ftp_prod_CK5ETD"
password = "sua_senha_aqui"
timeout_seconds = 30
max_retries = 3

[sync]
local_base_dir = "C:\\balancas"
remote_base_dir = "."
folder_pattern = "(?i)^bal(\\d+)$"
interval_seconds = 300
mtime_tolerance_seconds = 2

[logging]
log_file = "logs/sync_balancas.log"
level = "info"
max_size_mb = 10
max_backups = 5
```

Create `config.toml.example` with documented comments.

- [ ] **Step 3: Write failing tests for Config loading**

Create `internal/config/config_test.go`:
```go
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
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/config/... -v`  
Expected: FAIL (cannot find package `balancas/internal/config`)

- [ ] **Step 5: Implement internal/config/config.go**

Create `internal/config/config.go`:
```go
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

type SFTPConfig struct {
	URL            string `mapstructure:"url"`
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	User           string `mapstructure:"user"`
	Password       string `mapstructure:"password"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
	MaxRetries     int    `mapstructure:"max_retries"`
}

type SyncConfig struct {
	LocalBaseDir          string `mapstructure:"local_base_dir"`
	RemoteBaseDir         string `mapstructure:"remote_base_dir"`
	FolderPattern         string `mapstructure:"folder_pattern"`
	IntervalSeconds       int    `mapstructure:"interval_seconds"`
	MtimeToleranceSeconds int    `mapstructure:"mtime_tolerance_seconds"`
	DryRun                bool   `mapstructure:"dry_run"`
}

type LoggingConfig struct {
	LogFile    string `mapstructure:"log_file"`
	Level      string `mapstructure:"level"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
	MaxBackups int    `mapstructure:"max_backups"`
}

type Config struct {
	SFTP    SFTPConfig    `mapstructure:"sftp"`
	Sync    SyncConfig    `mapstructure:"sync"`
	Logging LoggingConfig `mapstructure:"logging"`
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("sftp.port", 22)
	v.SetDefault("sftp.timeout_seconds", 30)
	v.SetDefault("sftp.max_retries", 3)

	v.SetDefault("sync.local_base_dir", `C:\balancas`)
	v.SetDefault("sync.remote_base_dir", ".")
	v.SetDefault("sync.folder_pattern", `(?i)^bal(\d+)$`)
	v.SetDefault("sync.interval_seconds", 300)
	v.SetDefault("sync.mtime_tolerance_seconds", 2)
	v.SetDefault("sync.dry_run", false)

	v.SetDefault("logging.log_file", "logs/sync_balancas.log")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.max_size_mb", 10)
	v.SetDefault("logging.max_backups", 5)
}

func LoadConfig(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("toml")
	setDefaults(v)

	v.SetEnvPrefix("BALANCAS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.AddConfigPath(".")
		if execPath, err := os.Executable(); err == nil {
			v.AddConfigPath(filepath.Dir(execPath))
		}
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok && !os.IsNotExist(err) {
			return nil, fmt.Errorf("erro ao ler arquivo de configuração: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("erro ao decodificar configuração: %w", err)
	}

	// Parse SFTP URL se fornecida
	if cfg.SFTP.URL != "" {
		parsedURL, err := url.Parse(cfg.SFTP.URL)
		if err == nil {
			if parsedURL.Hostname() != "" && cfg.SFTP.Host == "" {
				cfg.SFTP.Host = parsedURL.Hostname()
			}
			if parsedURL.Port() != "" {
				if portNum, convErr := strconv.Atoi(parsedURL.Port()); convErr == nil && cfg.SFTP.Port == 22 {
					cfg.SFTP.Port = portNum
				}
			}
			if parsedURL.User != nil {
				if cfg.SFTP.User == "" {
					cfg.SFTP.User = parsedURL.User.Username()
				}
				if pass, ok := parsedURL.User.Password(); ok && cfg.SFTP.Password == "" {
					cfg.SFTP.Password = pass
				}
			}
		}
	}

	cfg.SFTP.Host = strings.TrimSpace(cfg.SFTP.Host)
	cfg.SFTP.User = strings.TrimSpace(cfg.SFTP.User)
	cfg.SFTP.Password = strings.TrimSpace(cfg.SFTP.Password)

	return &cfg, nil
}
```

- [ ] **Step 6: Run tests and verify they pass**

Run: `go test ./internal/config/... -v`  
Expected: PASS

- [ ] **Step 7: Commit Task 1**

```bash
git add go.mod go.sum config.toml config.toml.example internal/config/
git commit -m "feat(config): add viper toml configuration loader and structs"
```

---

### Task 2: Folder Regex Mapper

**Files:**
- Create: `internal/mapper/mapper.go`
- Test: `internal/mapper/mapper_test.go`

**Interfaces:**
- Produces: `type FolderMapper struct`
- Methods:
  - `NewFolderMapper(pattern string) (*FolderMapper, error)`
  - `MatchFolder(folderName string) (string, bool)`
  - `GetLocalFolder(baseDir, remoteName string) (string, bool)`

- [ ] **Step 1: Write failing tests for FolderMapper**

Create `internal/mapper/mapper_test.go`:
```go
package mapper_test

import (
	"path/filepath"
	"testing"

	"balancas/internal/mapper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFolderMapper_MatchFolder(t *testing.T) {
	m, err := mapper.NewFolderMapper(`(?i)^bal(\d+)$`)
	require.NoError(t, err)

	cases := []struct {
		input    string
		expected string
		matched  bool
	}{
		{"BAL01", "01", true},
		{"bal03", "03", true},
		{"BAL10", "10", true},
		{"BAL92", "92", true},
		{"BALANCAS", "", false},
		{"blackbox.zip", "", false},
		{"IMPORT", "", false},
		{".snapshot", "", false},
	}

	for _, tc := range cases {
		sub, ok := m.MatchFolder(tc.input)
		assert.Equal(t, tc.matched, ok, "Input: %s", tc.input)
		if tc.matched {
			assert.Equal(t, tc.expected, sub, "Input: %s", tc.input)
		}
	}
}

func TestFolderMapper_GetLocalFolder(t *testing.T) {
	m, err := mapper.NewFolderMapper(`(?i)^bal(\d+)$`)
	require.NoError(t, err)

	baseDir := `C:\balancas`
	target, ok := m.GetLocalFolder(baseDir, "BAL01")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join(baseDir, "01"), target)

	_, ok = m.GetLocalFolder(baseDir, "BALANCAS")
	assert.False(t, ok)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mapper/... -v`  
Expected: FAIL (cannot find package `balancas/internal/mapper`)

- [ ] **Step 3: Implement internal/mapper/mapper.go**

Create `internal/mapper/mapper.go`:
```go
package mapper

import (
	"path/filepath"
	"regexp"
)

type FolderMapper struct {
	re *regexp.Regexp
}

func NewFolderMapper(pattern string) (*FolderMapper, error) {
	if pattern == "" {
		pattern = `(?i)^bal(\d+)$`
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &FolderMapper{re: re}, nil
}

func (m *FolderMapper) MatchFolder(folderName string) (string, bool) {
	matches := m.re.FindStringSubmatch(folderName)
	if len(matches) == 0 {
		return "", false
	}
	if len(matches) > 1 {
		return matches[1], true
	}
	return matches[0], true
}

func (m *FolderMapper) GetLocalFolder(baseDir, remoteName string) (string, bool) {
	subName, ok := m.MatchFolder(remoteName)
	if !ok {
		return "", false
	}
	return filepath.Join(baseDir, subName), true
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./internal/mapper/... -v`  
Expected: PASS

- [ ] **Step 5: Commit Task 2**

```bash
git add internal/mapper/
git commit -m "feat(mapper): implement folder regex matching and local path mapping"
```

---

### Task 3: Incremental Sync Core & Atomic File Downloader

**Files:**
- Create: `internal/syncer/syncer.go`
- Test: `internal/syncer/syncer_test.go`

**Interfaces:**
- Produces: `type FileSyncer struct`
- Methods:
  - `NewFileSyncer(toleranceSeconds int) *FileSyncer`
  - `ShouldDownload(localPath string, remoteSize int64, remoteMtime time.Time) bool`
  - `DownloadAtomic(src io.Reader, localPath string, remoteMtime time.Time) error`
  - `SyncFile(downloadFunc func(dst io.Writer) error, localPath string, remoteSize int64, remoteMtime time.Time, dryRun bool) (SyncStatus, error)`
- Types: `type SyncStatus int` (`StatusDownloaded`, `StatusSkipped`, `StatusError`)

- [ ] **Step 1: Write failing tests for FileSyncer**

Create `internal/syncer/syncer_test.go`:
```go
package syncer_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"balancas/internal/syncer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldDownload_WhenFileDoesNotExist(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "itensmgv.txt")

	assert.True(t, s.ShouldDownload(localFile, 100, time.Now()))
}

func TestShouldSkip_WhenSizeAndMtimeMatch(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "itensmgv.txt")

	content := []byte("hello world")
	err := os.WriteFile(localFile, content, 0644)
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second)
	err = os.Chtimes(localFile, now, now)
	require.NoError(t, err)

	assert.False(t, s.ShouldDownload(localFile, int64(len(content)), now))
}

func TestDownloadAtomic_WritesAndSetsTimestamp(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "itensmgv.txt")

	content := []byte("downloaded atomic content")
	targetTime := time.Date(2026, 10, 20, 15, 30, 0, 0, time.Local)

	err := s.DownloadAtomic(bytes.NewReader(content), localFile, targetTime)
	require.NoError(t, err)

	readContent, err := os.ReadFile(localFile)
	require.NoError(t, err)
	assert.Equal(t, content, readContent)

	info, err := os.Stat(localFile)
	require.NoError(t, err)
	assert.WithinDuration(t, targetTime, info.ModTime(), 2*time.Second)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/syncer/... -v`  
Expected: FAIL (cannot find package `balancas/internal/syncer`)

- [ ] **Step 3: Implement internal/syncer/syncer.go**

Create `internal/syncer/syncer.go`:
```go
package syncer

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"
)

type SyncStatus int

const (
	StatusDownloaded SyncStatus = iota
	StatusSkipped
	StatusError
)

type FileSyncer struct {
	Tolerance time.Duration
}

func NewFileSyncer(toleranceSeconds int) *FileSyncer {
	if toleranceSeconds <= 0 {
		toleranceSeconds = 2
	}
	return &FileSyncer{
		Tolerance: time.Duration(toleranceSeconds) * time.Second,
	}
}

func (s *FileSyncer) ShouldDownload(localPath string, remoteSize int64, remoteMtime time.Time) bool {
	info, err := os.Stat(localPath)
	if err != nil {
		return true // Arquivo não existe ou erro ao ler status
	}

	if info.Size() != remoteSize {
		return true
	}

	diff := math.Abs(info.ModTime().Sub(remoteMtime).Seconds())
	return diff > s.Tolerance.Seconds()
}

func (s *FileSyncer) DownloadAtomic(src io.Reader, localPath string, remoteMtime time.Time) error {
	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("falha ao criar diretório local %s: %w", dir, err)
	}

	tmpFile := fmt.Sprintf("%s.%d.tmp", localPath, os.Getpid())
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("falha ao criar arquivo temporário %s: %w", tmpFile, err)
	}

	_, copyErr := io.Copy(f, src)
	closeErr := f.Close()

	if copyErr != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("falha ao gravar dados no arquivo temporário: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("falha ao fechar arquivo temporário: %w", closeErr)
	}

	// Rename atômico
	if err := os.Rename(tmpFile, localPath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("falha ao renomear arquivo temporário para %s: %w", localPath, err)
	}

	// Ajusta mtime
	_ = os.Chtimes(localPath, remoteMtime, remoteMtime)
	return nil
}

func (s *FileSyncer) SyncFile(
	downloadFunc func(dst io.Writer) error,
	localPath string,
	remoteSize int64,
	remoteMtime time.Time,
	dryRun bool,
) (SyncStatus, error) {
	if !s.ShouldDownload(localPath, remoteSize, remoteMtime) {
		return StatusSkipped, nil
	}

	if dryRun {
		log.Printf("[DRY-RUN] Baixaria para %s (%d bytes)", localPath, remoteSize)
		return StatusDownloaded, nil
	}

	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return StatusError, err
	}

	tmpPath := fmt.Sprintf("%s.%d.tmp", localPath, os.Getpid())
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return StatusError, err
	}

	if err := downloadFunc(tmpFile); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return StatusError, err
	}

	_ = tmpFile.Close()

	if err := os.Rename(tmpPath, localPath); err != nil {
		_ = os.Remove(tmpPath)
		return StatusError, err
	}

	_ = os.Chtimes(localPath, remoteMtime, remoteMtime)
	return StatusDownloaded, nil
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./internal/syncer/... -v`  
Expected: PASS

- [ ] **Step 5: Commit Task 3**

```bash
git add internal/syncer/
git commit -m "feat(syncer): implement change detection and atomic file downloader"
```

---

### Task 4: SFTP Client Manager with Retries & Directory Scanner

**Files:**
- Create: `internal/sftpclient/client.go`
- Test: `internal/sftpclient/client_test.go`

**Interfaces:**
- Produces: `type SFTPManager struct`, `type RemoteFileInfo struct`
- Methods:
  - `NewSFTPManager(cfg *config.Config) *SFTPManager`
  - `Connect() error`
  - `Close()`
  - `ListFolders(remoteDir string) ([]string, error)`
  - `ListFiles(remoteDir string) ([]RemoteFileInfo, error)`
  - `DownloadFile(remotePath string, dst io.Writer) error`

- [ ] **Step 1: Implement internal/sftpclient/client.go**

Create `internal/sftpclient/client.go`:
```go
package sftpclient

import (
	"fmt"
	"io"
	"log"
	"net"
	"path/posixpath"
	"sort"
	"strings"
	"time"

	"balancas/internal/config"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type RemoteFileInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
}

type SFTPManager struct {
	cfg        *config.Config
	sshClient  *ssh.Client
	sftpClient *sftp.Client
}

func NewSFTPManager(cfg *config.Config) *SFTPManager {
	return &SFTPManager{cfg: cfg}
}

func (m *SFTPManager) Connect() error {
	var lastErr error
	maxRetries := m.cfg.SFTP.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Printf("Conectando ao SFTP %s:%d (Tentativa %d/%d)...",
			m.cfg.SFTP.Host, m.cfg.SFTP.Port, attempt, maxRetries)

		sshConfig := &ssh.ClientConfig{
			User:            m.cfg.SFTP.User,
			Auth:            []ssh.AuthMethod{ssh.Password(m.cfg.SFTP.Password)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         time.Duration(m.cfg.SFTP.TimeoutSeconds) * time.Second,
		}

		addr := fmt.Sprintf("%s:%d", m.cfg.SFTP.Host, m.cfg.SFTP.Port)
		conn, err := net.DialTimeout("tcp", addr, sshConfig.Timeout)
		if err != nil {
			lastErr = err
			log.Printf("Falha na conexão TCP: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
		if err != nil {
			lastErr = err
			_ = conn.Close()
			log.Printf("Falha no handshake SSH: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		client := ssh.NewClient(sshConn, chans, reqs)
		m.sshClient = client

		sftpClient, err := sftp.NewClient(client)
		if err != nil {
			lastErr = err
			m.Close()
			log.Printf("Falha ao abrir canal SFTP: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		m.sftpClient = sftpClient
		log.Println("Conexão SFTP estabelecida com sucesso.")
		return nil
	}

	return fmt.Errorf("falha ao conectar ao SFTP após %d tentativas: %w", maxRetries, lastErr)
}

func (m *SFTPManager) Close() {
	if m.sftpClient != nil {
		_ = m.sftpClient.Close()
		m.sftpClient = nil
	}
	if m.sshClient != nil {
		_ = m.sshClient.Close()
		m.sshClient = nil
	}
}

func (m *SFTPManager) ListFolders(remoteDir string) ([]string, error) {
	if m.sftpClient == nil {
		return nil, fmt.Errorf("sftp não conectado")
	}

	entries, err := m.sftpClient.ReadDir(remoteDir)
	if err != nil {
		return nil, err
	}

	var folders []string
	for _, entry := range entries {
		if entry.IsDir() {
			name := entry.Name()
			if name != "." && name != ".." {
				folders.append(folders, name)
			}
		}
	}
	sort.Strings(folders)
	return folders, nil
}

func (m *SFTPManager) ListFiles(remoteDir string) ([]RemoteFileInfo, error) {
	if m.sftpClient == nil {
		return nil, fmt.Errorf("sftp não conectado")
	}

	entries, err := m.sftpClient.ReadDir(remoteDir)
	if err != nil {
		return nil, err
	}

	var files []RemoteFileInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, RemoteFileInfo{
				Name:    entry.Name(),
				Size:    entry.Size(),
				ModTime: entry.ModTime(),
			})
		}
	}
	return files, nil
}

func (m *SFTPManager) DownloadFile(remotePath string, dst io.Writer) error {
	if m.sftpClient == nil {
		return fmt.Errorf("sftp não conectado")
	}

	srcFile, err := m.sftpClient.Open(remotePath)
	if err != nil {
		return fmt.Errorf("erro ao abrir arquivo remoto %s: %w", remotePath, err)
	}
	defer srcFile.Close()

	_, err = io.Copy(dst, srcFile)
	return err
}
```

- [ ] **Step 2: Write tests for sftpclient**

Create `internal/sftpclient/client_test.go`:
```go
package sftpclient_test

import (
	"testing"

	"balancas/internal/config"
	"balancas/internal/sftpclient"
	"github.com/stretchr/testify/assert"
)

func TestSFTPManager_UnconnectedErrors(t *testing.T) {
	cfg := &config.Config{}
	mgr := sftpclient.NewSFTPManager(cfg)

	_, err := mgr.ListFolders(".")
	assert.Error(t, err)

	_, err = mgr.ListFiles(".")
	assert.Error(t, err)
}
```

- [ ] **Step 3: Run tests and verify they pass**

Run: `go test ./internal/sftpclient/... -v`  
Expected: PASS

- [ ] **Step 4: Commit Task 4**

```bash
git add internal/sftpclient/
git commit -m "feat(sftp): implement sftp manager with retries and listing"
```

---

### Task 5: Windows Service Implementation & Worker Loop

**Files:**
- Create: `internal/service/worker.go`
- Create: `internal/service/service.go`

**Interfaces:**
- Produces: `type Worker struct`, `type WindowsService struct` implementing `service.Interface`.
- Method: `RunSyncCycle(cfg *config.Config) (int, int, int, error)` (downloaded, skipped, errors)

- [ ] **Step 1: Implement internal/service/worker.go**

Create `internal/service/worker.go` with `RunSyncCycle`:
```go
package service

import (
	"context"
	"log"
	"path/filepath"
	"path/posixpath"
	"time"

	"balancas/internal/config"
	"balancas/internal/mapper"
	"balancas/internal/sftpclient"
	"balancas/internal/syncer"
)

type SyncStats struct {
	FoldersChecked int
	Downloaded     int
	Skipped        int
	Errors         int
}

func RunSyncCycle(cfg *config.Config) (SyncStats, error) {
	var stats SyncStats
	startTime := time.Now()

	log.Println("============================================================")
	log.Printf("Iniciando ciclo de sincronização para %s...", cfg.Sync.LocalBaseDir)

	fMapper, err := mapper.NewFolderMapper(cfg.Sync.FolderPattern)
	if err != nil {
		return stats, err
	}

	fSyncer := syncer.NewFileSyncer(cfg.Sync.MtimeToleranceSeconds)
	sftpMgr := sftpclient.NewSFTPManager(cfg)

	if err := sftpMgr.Connect(); err != nil {
		return stats, err
	}
	defer sftpMgr.Close()

	remoteFolders, err := sftpMgr.ListFolders(cfg.Sync.RemoteBaseDir)
	if err != nil {
		return stats, err
	}

	for _, rFolder := range remoteFolders {
		localFolder, ok := fMapper.GetLocalFolder(cfg.Sync.LocalBaseDir, rFolder)
		if !ok {
			continue
		}

		stats.FoldersChecked++
		rFolderPath := posixpath.Join(cfg.Sync.RemoteBaseDir, rFolder)
		if cfg.Sync.RemoteBaseDir == "." || cfg.Sync.RemoteBaseDir == "" {
			rFolderPath = rFolder
		}

		files, err := sftpMgr.ListFiles(rFolderPath)
		if err != nil {
			log.Printf("Erro ao listar arquivos da pasta remota %s: %v", rFolderPath, err)
			stats.Errors++
			continue
		}

		for _, file := range files {
			rFilePath := posixpath.Join(rFolderPath, file.Name)
			lFilePath := filepath.Join(localFolder, file.Name)

			downloadFunc := func(dst io.Writer) error {
				return sftpMgr.DownloadFile(rFilePath, dst)
			}

			status, err := fSyncer.SyncFile(downloadFunc, lFilePath, file.Size, file.ModTime, cfg.Sync.DryRun)
			if err != nil {
				log.Printf("[ERRO] Falha ao sincronizar %s: %v", rFilePath, err)
				stats.Errors++
			} else if status == syncer.StatusDownloaded {
				log.Printf("[BAIXADO] %s -> %s (%d bytes)", rFilePath, file.Name, file.Size)
				stats.Downloaded++
			} else {
				stats.Skipped++
			}
		}
	}

	elapsed := time.Since(startTime)
	log.Printf("Ciclo concluído em %v | Pastas: %d | Baixados: %d | Ignorados: %d | Erros: %d",
		elapsed.Round(time.Millisecond), stats.FoldersChecked, stats.Downloaded, stats.Skipped, stats.Errors)
	log.Println("============================================================")

	return stats, nil
}
```

- [ ] **Step 2: Implement internal/service/service.go**

Create `internal/service/service.go` implementing `service.Interface`:
```go
package service

import (
	"context"
	"log"
	"time"

	"balancas/internal/config"
	"github.com/kardianos/service"
)

type WindowsService struct {
	cfg    *config.Config
	ctx    context.Context
	cancel context.CancelFunc
}

func NewWindowsService(cfg *config.Config) *WindowsService {
	ctx, cancel := context.WithCancel(context.Background())
	return &WindowsService{
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (s *WindowsService) Start(svc service.Service) error {
	log.Println("Serviço Windows iniciando em background...")
	go s.run()
	return nil
}

func (s *WindowsService) Stop(svc service.Service) error {
	log.Println("Serviço Windows recebendo sinal de parada...")
	s.cancel()
	return nil
}

func (s *WindowsService) run() {
	interval := time.Duration(s.cfg.Sync.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	// Executa primeiro ciclo imediatamente
	_, _ = RunSyncCycle(s.cfg)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			log.Println("Worker encerrado com sucesso.")
			return
		case <-ticker.C:
			_, _ = RunSyncCycle(s.cfg)
		}
	}
}
```

- [ ] **Step 3: Commit Task 5**

```bash
git add internal/service/
git commit -m "feat(service): implement windows service interface and worker loop"
```

---

### Task 6: Cobra CLI Commands, Makefile & Documentation

**Files:**
- Create: `cmd/balancas-sync/main.go`
- Create: `cmd/balancas-sync/root.go`
- Create: `Makefile`
- Update: `README.md`

- [ ] **Step 1: Implement cmd/balancas-sync/root.go & main.go**

Implement Cobra commands:
- `rootCmd`: Executa o serviço ou inicia worker
- `runCmd`: Executa um ciclo único e finaliza com `os.Exit(0)` ou `os.Exit(2)`
- `startCmd`: Executa worker interativo no console
- `serviceCmd`: Comandos `install`, `uninstall`, `start`, `stop`, `restart`, `status`

- [ ] **Step 2: Create Makefile for cross-compiling**

Create `Makefile`:
```makefile
.PHONY: build test build-windows clean

build:
	go build -o bin/balancas-sync ./cmd/balancas-sync

build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/balancas-sync.exe ./cmd/balancas-sync

test:
	go test -v ./...

clean:
	rm -rf bin/
```

- [ ] **Step 3: Update README.md with complete Portuguese instructions**

Document:
- Instalação e desinstalação via CLI (`balancas-sync.exe service install`).
- Configuração do `config.toml`.
- Execução em console para depuração (`balancas-sync.exe start` ou `balancas-sync.exe run`).
- Gerenciamento via `services.msc`.

- [ ] **Step 4: Verify cross-compilation**

Run: `make build-windows`  
Expected: Generates `bin/balancas-sync.exe` cleanly.

- [ ] **Step 5: Commit Task 6**

```bash
git add cmd/ Makefile README.md
git commit -m "feat(cli): add cobra cli, windows service commands, makefile and docs"
```

---

### Task 7: Live SFTP Integration Test

**Files:**
- Create: `internal/sftpclient/live_test.go`

- [ ] **Step 1: Implement live_test.go**

Tests live connection and folder discovery when `config.toml` exists.

- [ ] **Step 2: Run test against live server**

Run: `go test -v ./internal/sftpclient -run TestLiveSFTP`  
Expected: PASS

- [ ] **Step 3: Commit Task 7**

```bash
git add internal/sftpclient/live_test.go
git commit -m "test(integration): add live sftp connection test in Go"
```
