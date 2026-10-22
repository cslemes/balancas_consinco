package sftpclient

import (
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"strconv"
	"time"

	"balancas/internal/config"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// RemoteFileInfo armazena metadados de um arquivo remoto no SFTP.
type RemoteFileInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// SFTPManager gerencia conexões SSH e cliente SFTP com lógica de retry e operações de leitura/download.
type SFTPManager struct {
	cfg        *config.Config
	sshClient  *ssh.Client
	sftpClient *sftp.Client
	retryDelay time.Duration
}

// NewSFTPManager cria uma nova instância de SFTPManager.
func NewSFTPManager(cfg *config.Config) *SFTPManager {
	return &SFTPManager{
		cfg:        cfg,
		retryDelay: 2 * time.Second,
	}
}

// SetRetryDelay configura o intervalo de espera entre retentativas (útil para testes unitários).
func (m *SFTPManager) SetRetryDelay(d time.Duration) {
	m.retryDelay = d
}

// Connect estabelece a conexão SSH e inicializa o cliente SFTP com retentativas.
func (m *SFTPManager) Connect() error {
	if m == nil || m.cfg == nil {
		return fmt.Errorf("configuração do sftp não informada")
	}

	m.Close()

	var lastErr error
	maxRetries := m.cfg.SFTP.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	timeout := time.Duration(m.cfg.SFTP.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	delay := m.retryDelay
	if delay <= 0 {
		delay = 2 * time.Second
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Printf("Conectando ao SFTP %s:%d (Tentativa %d/%d)...",
			m.cfg.SFTP.Host, m.cfg.SFTP.Port, attempt, maxRetries)

		sshConfig := &ssh.ClientConfig{
			User:            m.cfg.SFTP.User,
			Auth:            []ssh.AuthMethod{ssh.Password(m.cfg.SFTP.Password)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         timeout,
		}

		addr := net.JoinHostPort(m.cfg.SFTP.Host, strconv.Itoa(m.cfg.SFTP.Port))
		conn, err := net.DialTimeout("tcp", addr, sshConfig.Timeout)
		if err != nil {
			lastErr = err
			log.Printf("Falha na conexão TCP: %v", err)
			if attempt < maxRetries {
				time.Sleep(delay)
			}
			continue
		}

		sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
		if err != nil {
			lastErr = err
			_ = conn.Close()
			log.Printf("Falha no handshake SSH: %v", err)
			if attempt < maxRetries {
				time.Sleep(delay)
			}
			continue
		}

		client := ssh.NewClient(sshConn, chans, reqs)
		m.sshClient = client

		sftpClient, err := sftp.NewClient(client)
		if err != nil {
			lastErr = err
			m.Close()
			log.Printf("Falha ao abrir canal SFTP: %v", err)
			if attempt < maxRetries {
				time.Sleep(delay)
			}
			continue
		}

		m.sftpClient = sftpClient
		log.Println("Conexão SFTP estabelecida com sucesso.")
		return nil
	}

	return fmt.Errorf("falha ao conectar ao SFTP após %d tentativas: %w", maxRetries, lastErr)
}

// Close fecha de maneira segura o cliente SFTP e a conexão SSH subjacente.
func (m *SFTPManager) Close() {
	if m == nil {
		return
	}
	if m.sftpClient != nil {
		_ = m.sftpClient.Close()
		m.sftpClient = nil
	}
	if m.sshClient != nil {
		_ = m.sshClient.Close()
		m.sshClient = nil
	}
}

// ListFolders lista todos os diretórios dentro de remoteDir, ordenados alfabeticamente e ignorando "." e "..".
func (m *SFTPManager) ListFolders(remoteDir string) ([]string, error) {
	if m == nil || m.sftpClient == nil {
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
				folders = append(folders, name)
			}
		}
	}
	sort.Strings(folders)
	return folders, nil
}

// ListFiles lista todos os arquivos regulares dentro de remoteDir com seus respectivos metadados.
func (m *SFTPManager) ListFiles(remoteDir string) ([]RemoteFileInfo, error) {
	if m == nil || m.sftpClient == nil {
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

// DownloadFile faz o download de um arquivo remoto gravando o conteúdo diretamente no destino dst io.Writer.
func (m *SFTPManager) DownloadFile(remotePath string, dst io.Writer) error {
	if m == nil || m.sftpClient == nil {
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
