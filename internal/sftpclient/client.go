package sftpclient

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/cslemes/balancas_consinco/internal/config"
	"github.com/cslemes/balancas_consinco/internal/pathguard"
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
	mu         sync.Mutex
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
	return m.ConnectContext(context.Background())
}

// ConnectContext connects to the configured SFTP server and honors cancellation.
func (m *SFTPManager) ConnectContext(ctx context.Context) error {
	if m == nil || m.cfg == nil {
		return fmt.Errorf("configuração do sftp não informada")
	}
	if ctx == nil {
		ctx = context.Background()
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
		if err := ctx.Err(); err != nil {
			return err
		}
		log.Printf("Conectando ao SFTP %s:%d (Tentativa %d/%d)...",
			m.cfg.SFTP.Host, m.cfg.SFTP.Port, attempt, maxRetries)

		sshConfig := &ssh.ClientConfig{
			User: m.cfg.SFTP.User,
			Auth: []ssh.AuthMethod{ssh.Password(m.cfg.SFTP.Password)},
			HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
				got := ssh.FingerprintSHA256(key)
				if got != m.cfg.SFTP.HostKeySHA256 {
					return fmt.Errorf("fingerprint SSH inesperado para %s: recebido %s", hostname, got)
				}
				return nil
			},
			Timeout: timeout,
		}

		addr := net.JoinHostPort(m.cfg.SFTP.Host, strconv.Itoa(m.cfg.SFTP.Port))
		conn, err := (&net.Dialer{Timeout: sshConfig.Timeout}).DialContext(ctx, "tcp", addr)
		if err != nil {
			lastErr = err
			log.Printf("Falha na conexão TCP: %v", err)
			if attempt < maxRetries {
				if err := waitRetry(ctx, delay); err != nil {
					return err
				}
			}
			continue
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			_ = tcpConn.SetKeepAlive(true)
			_ = tcpConn.SetKeepAlivePeriod(30 * time.Second)
		}
		_ = conn.SetDeadline(time.Now().Add(sshConfig.Timeout))

		stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
		sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
		if err != nil {
			stopClose()
			if ctx.Err() != nil {
				_ = conn.Close()
				return ctx.Err()
			}
			lastErr = err
			_ = conn.Close()
			log.Printf("Falha no handshake SSH: %v", err)
			if attempt < maxRetries {
				if err := waitRetry(ctx, delay); err != nil {
					return err
				}
			}
			continue
		}

		_ = conn.SetDeadline(time.Time{})

		client := ssh.NewClient(sshConn, chans, reqs)
		m.mu.Lock()
		m.sshClient = client
		m.mu.Unlock()

		sftpClient, err := sftp.NewClient(client)
		stopClose()
		if err != nil {
			lastErr = err
			m.Close()
			log.Printf("Falha ao abrir canal SFTP: %v", err)
			if attempt < maxRetries {
				if err := waitRetry(ctx, delay); err != nil {
					return err
				}
			}
			continue
		}

		m.mu.Lock()
		m.sftpClient = sftpClient
		m.mu.Unlock()
		log.Println("Conexão SFTP estabelecida com sucesso.")
		return nil
	}

	return fmt.Errorf("falha ao conectar ao SFTP após %d tentativas: %w", maxRetries, lastErr)
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// CloseOnContext closes active SFTP and SSH connections when ctx is canceled.
func (m *SFTPManager) CloseOnContext(ctx context.Context) func() bool {
	return context.AfterFunc(ctx, m.Close)
}

// Close fecha de maneira segura o cliente SFTP e a conexão SSH subjacente.
func (m *SFTPManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	sftpClient, sshClient := m.sftpClient, m.sshClient
	m.sftpClient, m.sshClient = nil, nil
	m.mu.Unlock()
	if sftpClient != nil {
		_ = sftpClient.Close()
	}
	if sshClient != nil {
		_ = sshClient.Close()
	}
}

// ListFolders lista todos os diretórios dentro de remoteDir, ordenados alfabeticamente e ignorando "." e "..".
func (m *SFTPManager) ListFolders(remoteDir string) ([]string, error) {
	client := m.getSFTPClient()
	if client == nil {
		return nil, fmt.Errorf("sftp não conectado")
	}

	entries, err := client.ReadDir(remoteDir)
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
	client := m.getSFTPClient()
	if client == nil {
		return nil, fmt.Errorf("sftp não conectado")
	}

	entries, err := client.ReadDir(remoteDir)
	if err != nil {
		return nil, err
	}

	var files []RemoteFileInfo
	for _, entry := range entries {
		if !entry.IsDir() && entry.Mode().IsRegular() && pathguard.IsSafeComponent(entry.Name()) {
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
	client := m.getSFTPClient()
	if client == nil {
		return fmt.Errorf("sftp não conectado")
	}

	srcFile, err := client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("erro ao abrir arquivo remoto %s: %w", remotePath, err)
	}
	defer srcFile.Close()

	_, err = io.Copy(dst, srcFile)
	return err
}

// RemoveFile deletes a remote file after the local copy has been committed.
func (m *SFTPManager) RemoveFile(remotePath string) error {
	client := m.getSFTPClient()
	if client == nil {
		return fmt.Errorf("sftp não conectado")
	}
	if err := client.Remove(remotePath); err != nil {
		return fmt.Errorf("erro ao remover arquivo remoto %s: %w", remotePath, err)
	}
	return nil
}

func (m *SFTPManager) getSFTPClient() *sftp.Client {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sftpClient
}
