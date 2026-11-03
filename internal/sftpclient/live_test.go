package sftpclient_test

import (
	"bytes"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/cslemes/balancas_consinco/internal/config"
	"github.com/cslemes/balancas_consinco/internal/mapper"
	"github.com/cslemes/balancas_consinco/internal/sftpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findConfigFile() string {
	candidates := []string{
		"config.toml",
		"../../config.toml",
		"../config.toml",
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

func loadLiveConfig(t *testing.T) *config.Config {
	t.Helper()

	if os.Getenv("CI") != "" {
		t.Skip("Pulando teste live SFTP em ambiente CI")
	}

	cfgPath := findConfigFile()
	if cfgPath == "" {
		t.Skip("config.toml não encontrado; pulando teste de integração live SFTP")
	}

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Skipf("Erro ao carregar %s: %v; pulando teste", cfgPath, err)
	}

	if cfg.SFTP.Host == "" || cfg.SFTP.User == "" || cfg.SFTP.Password == "" {
		t.Skip("Credenciais de SFTP ausentes no config.toml; pulando teste")
	}

	return cfg
}

func TestLiveSFTP(t *testing.T) {
	cfg := loadLiveConfig(t)

	mgr := sftpclient.NewSFTPManager(cfg)
	err := mgr.Connect()
	require.NoError(t, err, "A conexão ao servidor SFTP live deve ser bem-sucedida")
	defer mgr.Close()

	var balFolders []string

	t.Run("ConnectionAndFolders", func(t *testing.T) {
		folders, err := mgr.ListFolders(cfg.Sync.RemoteBaseDir)
		require.NoError(t, err, "ListFolders deve listar pastas no diretório base remoto")
		require.NotEmpty(t, folders, "Deveria listar pelo menos uma pasta no diretório base remoto")

		fMapper, err := mapper.NewFolderMapper(cfg.Sync.FolderPattern)
		require.NoError(t, err)

		for _, f := range folders {
			if _, ok := fMapper.MatchFolder(f); ok {
				balFolders = append(balFolders, f)
			}
		}

		assert.NotEmpty(t, balFolders, "Deveria encontrar pastas que coincidem com o padrão de balanças")
		assert.Contains(t, balFolders, "BAL01", "Deveria conter a pasta BAL01")
	})

	t.Run("ListFilesInBAL01", func(t *testing.T) {
		files, err := mgr.ListFiles("BAL01")
		require.NoError(t, err, "ListFiles deve listar arquivos de BAL01")
		require.NotEmpty(t, files, "Deveria conter arquivos em BAL01")

		var txtFile *sftpclient.RemoteFileInfo
		for i := range files {
			f := &files[i]
			assert.NotEmpty(t, f.Name)
			assert.GreaterOrEqual(t, f.Size, int64(0))
			assert.False(t, f.ModTime.IsZero())
			if strings.HasSuffix(strings.ToLower(f.Name), ".txt") && txtFile == nil {
				txtFile = f
			}
		}

		require.NotNil(t, txtFile, "Deveria conter pelo menos um arquivo .txt em BAL01")

		// Testa leitura/download de uma amostra do arquivo .txt
		var buf bytes.Buffer
		remoteFilePath := path.Join("BAL01", txtFile.Name)
		err = mgr.DownloadFile(remoteFilePath, &buf)
		require.NoError(t, err, "DownloadFile deve baixar com sucesso o arquivo remoto")
		assert.Equal(t, txtFile.Size, int64(buf.Len()), "Tamanho baixado deve coincidir com o tamanho remoto")
	})
}
