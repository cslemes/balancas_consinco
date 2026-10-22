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
	v.SetDefault("sftp.url", "")
	v.SetDefault("sftp.host", "")
	v.SetDefault("sftp.port", 22)
	v.SetDefault("sftp.user", "")
	v.SetDefault("sftp.password", "")
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
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("erro ao ler arquivo de configuração: %w", err)
		}
	} else {
		v.SetConfigName("config")
		v.AddConfigPath(".")
		if execPath, err := os.Executable(); err == nil {
			v.AddConfigPath(filepath.Dir(execPath))
		}
		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok && !os.IsNotExist(err) {
				return nil, fmt.Errorf("erro ao ler arquivo de configuração: %w", err)
			}
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
				if portNum, convErr := strconv.Atoi(parsedURL.Port()); convErr == nil && (cfg.SFTP.Port == 22 || cfg.SFTP.Port == 0) {
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

// Validate verifica se as configurações obrigatórias do serviço estão preenchidas e válidas.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("configuração não informada")
	}
	if strings.TrimSpace(c.SFTP.Host) == "" {
		return fmt.Errorf("host do SFTP não configurado (sftp.host)")
	}
	if strings.TrimSpace(c.SFTP.User) == "" {
		return fmt.Errorf("usuário do SFTP não configurado (sftp.user)")
	}
	if strings.TrimSpace(c.SFTP.Password) == "" {
		return fmt.Errorf("senha do SFTP não configurada (sftp.password)")
	}
	if c.Sync.IntervalSeconds <= 0 {
		return fmt.Errorf("intervalo de sincronização deve ser maior que zero (sync.interval_seconds)")
	}
	return nil
}

