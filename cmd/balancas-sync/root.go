package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"balancas/internal/config"
	"balancas/internal/service"
	ks "github.com/kardianos/service"
	"github.com/spf13/cobra"
)

var (
	cfgFile      string
	dryRunFlag   bool
	logLevelFlag string

	logFileHandle *os.File

	// Variáveis de injeção para testes unitários
	exitFunc         = os.Exit
	runSyncCycleFunc = service.RunSyncCycle
	newServiceFunc   = newService
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

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Aviso: falha ao abrir arquivo de log %s: %v", logPath, err)
		return
	}

	logFileHandle = f
	log.SetFlags(log.Ldate | log.Ltime)

	if ks.Interactive() {
		log.SetOutput(io.MultiWriter(os.Stdout, f))
	} else {
		log.SetOutput(f)
	}
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

		stats, err := runSyncCycleFunc(cfg)
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
	cfg, _ := config.LoadConfig(cfgFile)
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
