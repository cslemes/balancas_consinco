package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"testing"

	"balancas/internal/config"
	"balancas/internal/service"
	ks "github.com/kardianos/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockService struct {
	status ks.Status
	err    error
}

func (m *mockService) Start() error { return m.err }
func (m *mockService) Stop() error  { return m.err }
func (m *mockService) Restart() error { return m.err }
func (m *mockService) Install() error { return m.err }
func (m *mockService) Uninstall() error { return m.err }
func (m *mockService) Run() error   { return m.err }
func (m *mockService) Status() (ks.Status, error) {
	return m.status, m.err
}
func (m *mockService) Logger(errs chan<- error) (ks.Logger, error) {
	return nil, nil
}
func (m *mockService) SystemLogger(errs chan<- error) (ks.Logger, error) {
	return nil, nil
}
func (m *mockService) String() string {
	return "MockService"
}
func (m *mockService) Platform() string {
	return "mock"
}

func TestCommands_StructureAndFlags(t *testing.T) {
	assert.Equal(t, "balancas-sync", rootCmd.Use)
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("config"))
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("dry-run"))
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("log-level"))

	foundCommands := make(map[string]bool)
	for _, cmd := range rootCmd.Commands() {
		foundCommands[cmd.Name()] = true
	}
	assert.True(t, foundCommands["run"])
	assert.True(t, foundCommands["start"])
	assert.True(t, foundCommands["service"])

	serviceSubcommands := make(map[string]bool)
	for _, cmd := range serviceCmd.Commands() {
		serviceSubcommands[cmd.Name()] = true
	}
	assert.True(t, serviceSubcommands["install"])
	assert.True(t, serviceSubcommands["uninstall"])
	assert.True(t, serviceSubcommands["start"])
	assert.True(t, serviceSubcommands["stop"])
	assert.True(t, serviceSubcommands["restart"])
	assert.True(t, serviceSubcommands["status"])
}

func TestRunCmd_Success(t *testing.T) {
	origExit := exitFunc
	origSync := runSyncCycleFunc
	defer func() {
		exitFunc = origExit
		runSyncCycleFunc = origSync
		closeLogFile()
	}()

	var capturedCode int
	exitFunc = func(code int) {
		capturedCode = code
	}

	runSyncCycleFunc = func(cfg *config.Config) (service.SyncStats, error) {
		return service.SyncStats{FoldersChecked: 1, Downloaded: 1, Errors: 0}, nil
	}

	rootCmd.SetArgs([]string{"run"})
	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, 0, capturedCode)
}

func TestRunCmd_PartialErrors(t *testing.T) {
	origExit := exitFunc
	origSync := runSyncCycleFunc
	defer func() {
		exitFunc = origExit
		runSyncCycleFunc = origSync
		closeLogFile()
	}()

	var capturedCode int
	exitFunc = func(code int) {
		capturedCode = code
	}

	runSyncCycleFunc = func(cfg *config.Config) (service.SyncStats, error) {
		return service.SyncStats{FoldersChecked: 1, Errors: 2}, nil
	}

	rootCmd.SetArgs([]string{"run"})
	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, 2, capturedCode)
}

func TestRunCmd_FatalError(t *testing.T) {
	origExit := exitFunc
	origSync := runSyncCycleFunc
	defer func() {
		exitFunc = origExit
		runSyncCycleFunc = origSync
		closeLogFile()
	}()

	var capturedCode int
	exitFunc = func(code int) {
		capturedCode = code
	}

	runSyncCycleFunc = func(cfg *config.Config) (service.SyncStats, error) {
		return service.SyncStats{}, errors.New("falha grave de conexão")
	}

	rootCmd.SetArgs([]string{"run"})
	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, 1, capturedCode)
}

func TestRunCmd_ConfigError(t *testing.T) {
	origExit := exitFunc
	defer func() {
		exitFunc = origExit
		cfgFile = ""
		closeLogFile()
	}()

	var capturedCode int
	exitFunc = func(code int) {
		capturedCode = code
	}

	cfgFile = "arquivo_de_configuracao_inexistente_123.toml"
	rootCmd.SetArgs([]string{"run"})
	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, 1, capturedCode)
}

func TestLoadAndConfigure_Overrides(t *testing.T) {
	defer func() {
		dryRunFlag = false
		logLevelFlag = ""
		cfgFile = ""
		closeLogFile()
	}()

	dryRunFlag = true
	logLevelFlag = "debug"
	cfgFile = ""

	cfg, err := loadAndConfigure()
	require.NoError(t, err)
	assert.True(t, cfg.Sync.DryRun)
	assert.Equal(t, "debug", cfg.Logging.Level)
}

func TestSetupLogging_CreatesFile(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	cfg := &config.Config{
		Logging: config.LoggingConfig{
			LogFile: logFile,
		},
	}

	setupLogging(cfg)
	log.Println("Teste de log")
	closeLogFile()

	assert.FileExists(t, logFile)
	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Teste de log")
}

func TestHandleServiceAction_Invalid(t *testing.T) {
	err := handleServiceAction("invalid-action-xyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ação desconhecida 'invalid-action-xyz'")
}

func TestHandleServiceAction_Status(t *testing.T) {
	origNewService := newServiceFunc
	defer func() {
		newServiceFunc = origNewService
	}()

	newServiceFunc = func(prg ks.Interface, cfgPath string) (ks.Service, error) {
		return &mockService{status: ks.StatusRunning}, nil
	}

	err := handleServiceAction("status")
	assert.NoError(t, err)

	newServiceFunc = func(prg ks.Interface, cfgPath string) (ks.Service, error) {
		return &mockService{status: ks.StatusStopped}, nil
	}

	err = handleServiceAction("status")
	assert.NoError(t, err)

	newServiceFunc = func(prg ks.Interface, cfgPath string) (ks.Service, error) {
		return &mockService{status: ks.StatusUnknown}, nil
	}

	err = handleServiceAction("status")
	assert.NoError(t, err)
}

func TestNewService_ArgumentsWithConfig(t *testing.T) {
	prg := service.NewWindowsService(nil)
	svc, err := newService(prg, "config.toml")
	require.NoError(t, err)
	assert.NotNil(t, svc)
}
