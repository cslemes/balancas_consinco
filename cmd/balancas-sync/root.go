package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/cslemes/balancas_consinco/internal/config"
	"github.com/cslemes/balancas_consinco/internal/service"
	ks "github.com/kardianos/service"
	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

// LogLevel representa os níveis de severidade de log suportados.
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

func parseLogLevel(level string) LogLevel {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return LevelDebug
	case "warn", "warning", "aviso":
		return LevelWarn
	case "error", "erro":
		return LevelError
	default:
		return LevelInfo
	}
}

type levelFilterWriter struct {
	w        io.Writer
	minLevel LogLevel
}

func (f *levelFilterWriter) Write(p []byte) (n int, err error) {
	upper := strings.ToUpper(string(p))
	msgLevel := LevelInfo
	if strings.Contains(upper, "[DEBUG]") {
		msgLevel = LevelDebug
	} else if strings.Contains(upper, "[AVISO]") || strings.Contains(upper, "[WARN]") {
		msgLevel = LevelWarn
	} else if strings.Contains(upper, "[ERRO]") || strings.Contains(upper, "ERRO FATAL") || strings.Contains(upper, "ERROR") {
		msgLevel = LevelError
	}

	if msgLevel < f.minLevel {
		return len(p), nil
	}
	return f.w.Write(p)
}

var (
	cfgFile      string
	dryRunFlag   bool
	logLevelFlag string

	logFileHandle io.Closer

	// Variáveis de injeção para testes unitários
	exitFunc                                                                                  = os.Exit
	runSyncCycleFunc func(ctx context.Context, cfg *config.Config) (service.SyncStats, error) = service.RunSyncCycle
	newServiceFunc                                                                            = newService
)

func newService(prg ks.Interface, cfgPath string) (ks.Service, error) {
	svcConfig := &ks.Config{
		Name:        "BalancasSync",
		DisplayName: "Balanças SFTP Sync Service",
		Description: "Serviço de sincronização de arquivos de balanças Toledo MGV a partir de servidor SFTP.",
	}

	if cfgPath != "" {
		if absPath, err := filepath.Abs(cfgPath); err == nil {
			svcConfig.Arguments = []string{"--config", absPath}
		} else {
			svcConfig.Arguments = []string{"--config", cfgPath}
		}
	}

	return ks.New(prg, svcConfig)
}

func setupLogging(cfg *config.Config) {
	if cfg == nil || cfg.Logging.LogFile == "" {
		return
	}

	logPath := cfg.Logging.LogFile
	if !filepath.IsAbs(logPath) {
		if execPath, err := os.Executable(); err == nil && !ks.Interactive() {
			logPath = filepath.Join(filepath.Dir(execPath), logPath)
		}
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		log.Printf("Aviso: falha ao criar diretório de log %s: %v", filepath.Dir(logPath), err)
		return
	}

	maxSize := cfg.Logging.MaxSizeMB
	if maxSize <= 0 {
		maxSize = 10
	}
	maxBackups := cfg.Logging.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 5
	}

	ljLogger := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		LocalTime:  true,
	}

	logFileHandle = ljLogger
	log.SetFlags(log.Ldate | log.Ltime)

	var baseWriter io.Writer
	if ks.Interactive() {
		baseWriter = io.MultiWriter(os.Stdout, ljLogger)
	} else {
		baseWriter = ljLogger
	}

	minLevel := parseLogLevel(cfg.Logging.Level)
	filteredWriter := &levelFilterWriter{
		w:        baseWriter,
		minLevel: minLevel,
	}

	log.SetOutput(filteredWriter)
}

func closeLogFile() {
	if logFileHandle != nil {
		_ = logFileHandle.Close()
		logFileHandle = nil
	}
}

func loadAndConfigure() (*config.Config, error) {
	cfg, err := config.LoadConfig(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("erro ao carregar arquivo de configuração: %w", err)
	}

	if dryRunFlag {
		cfg.Sync.DryRun = true
	}
	if logLevelFlag != "" {
		cfg.Logging.Level = logLevelFlag
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuração inválida: %w", err)
	}

	setupLogging(cfg)
	return cfg, nil
}

var rootCmd = &cobra.Command{
	Use:   "balancas-sync",
	Short: "Sincronizador SFTP de arquivos de balanças MGV",
	Long:  "Serviço Windows e CLI para sincronização de arquivos de balanças Toledo MGV a partir de servidor SFTP.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadAndConfigure()
		if err != nil {
			return fmt.Errorf("erro fatal ao carregar configuração: %w", err)
		}
		defer closeLogFile()

		prg := service.NewWindowsService(cfg)
		s, err := newServiceFunc(prg, cfgFile)
		if err != nil {
			return fmt.Errorf("erro ao inicializar serviço: %w", err)
		}

		return s.Run()
	},
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Executa um único ciclo de sincronização e finaliza",
	Long:  "Executa uma sincronização única SFTP e finaliza retornando o código de saída: 0 (sucesso), 2 (erros parciais de arquivo) ou 1 (erro fatal).",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := loadAndConfigure()
		if err != nil {
			log.Printf("Erro fatal ao carregar configuração: %v", err)
			closeLogFile()
			exitFunc(1)
			return
		}
		defer closeLogFile()

		stats, err := runSyncCycleFunc(cmd.Context(), cfg)
		if err != nil {
			log.Printf("Erro fatal durante o ciclo de sincronização: %v", err)
			closeLogFile()
			exitFunc(1)
			return
		}

		if stats.Errors > 0 {
			log.Printf("Ciclo finalizado com %d erro(s) parcial(is).", stats.Errors)
			closeLogFile()
			exitFunc(2)
			return
		}

		closeLogFile()
		exitFunc(0)
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Executa o worker de sincronização continuamente no console",
	Long:  "Inicia o worker em primeiro plano (foreground) e executa ciclos periódicos conforme o intervalo configurado. Pressione Ctrl+C para encerrar.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadAndConfigure()
		if err != nil {
			return fmt.Errorf("erro fatal ao carregar configuração: %w", err)
		}
		defer closeLogFile()

		prg := service.NewWindowsService(cfg)
		s, err := newServiceFunc(prg, cfgFile)
		if err != nil {
			return fmt.Errorf("erro ao inicializar serviço: %w", err)
		}

		log.Println("Iniciando worker em primeiro plano (Ctrl+C para encerrar)...")
		return s.Run()
	},
}

var serviceCmd = &cobra.Command{
	Use:   "service [install|uninstall|start|stop|restart|status]",
	Short: "Gerencia o serviço Windows (install, uninstall, start, stop, restart, status)",
	Long:  "Comandos administrativos para instalar, desinstalar, iniciar, parar, reiniciar ou consultar o status do serviço Windows.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		return handleServiceAction(args[0])
	},
}

func handleServiceAction(action string) error {
	cfg, err := config.LoadConfig(cfgFile)
	if err != nil {
		return fmt.Errorf("erro ao carregar configuração: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configuração inválida: %w", err)
	}

	prg := service.NewWindowsService(cfg)
	s, err := newServiceFunc(prg, cfgFile)
	if err != nil {
		return fmt.Errorf("erro ao inicializar configuração do serviço: %w", err)
	}

	switch action {
	case "status":
		status, err := s.Status()
		if err != nil {
			return fmt.Errorf("falha ao consultar status do serviço: %w", err)
		}
		switch status {
		case ks.StatusRunning:
			fmt.Println("Status do serviço: Em execução (Running)")
		case ks.StatusStopped:
			fmt.Println("Status do serviço: Parado (Stopped)")
		default:
			fmt.Println("Status do serviço: Desconhecido ou não instalado (Unknown)")
		}
		return nil
	case "install", "uninstall", "start", "stop", "restart":
		err := ks.Control(s, action)
		if err != nil {
			return fmt.Errorf("falha ao executar '%s' no serviço: %w", action, err)
		}
		fmt.Printf("Comando '%s' executado com sucesso no serviço.\n", action)
		return nil
	default:
		return fmt.Errorf("ação desconhecida '%s'. Ações suportadas: install, uninstall, start, stop, restart, status", action)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "Caminho para o arquivo config.toml")
	rootCmd.PersistentFlags().BoolVar(&dryRunFlag, "dry-run", false, "Executa em modo simulação sem gravar em disco")
	rootCmd.PersistentFlags().StringVar(&logLevelFlag, "log-level", "", "Nível de detalhamento do log (debug, info, warn, error)")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(serviceCmd)

	serviceActions := []string{"install", "uninstall", "start", "stop", "restart", "status"}
	for _, act := range serviceActions {
		action := act
		serviceCmd.AddCommand(&cobra.Command{
			Use:   action,
			Short: fmt.Sprintf("Executa a ação '%s' no serviço Windows", action),
			RunE: func(cmd *cobra.Command, args []string) error {
				return handleServiceAction(action)
			},
		})
	}
}

// Execute executa o comando raiz do CLI.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		closeLogFile()
		os.Exit(1)
	}
}
