package sftpclient_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"balancas/internal/config"
	"balancas/internal/sftpclient"
	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestSFTPManager_UnconnectedErrors(t *testing.T) {
	cfg := &config.Config{}
	mgr := sftpclient.NewSFTPManager(cfg)

	_, err := mgr.ListFolders(".")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sftp não conectado")

	_, err = mgr.ListFiles(".")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sftp não conectado")

	var buf bytes.Buffer
	err = mgr.DownloadFile("dummy.txt", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sftp não conectado")
}

func TestSFTPManager_NilConfig(t *testing.T) {
	var nilMgr *sftpclient.SFTPManager
	err := nilMgr.Connect()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuração do sftp não informada")

	mgr := sftpclient.NewSFTPManager(nil)
	err = mgr.Connect()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuração do sftp não informada")
}

func TestSFTPManager_CloseIdempotent(t *testing.T) {
	cfg := &config.Config{}
	mgr := sftpclient.NewSFTPManager(cfg)

	// Close on unconnected manager should be safe and idempotent
	assert.NotPanics(t, func() {
		mgr.Close()
		mgr.Close()
	})
}

func TestSFTPManager_ConnectFailure_Retries(t *testing.T) {
	// Find an unused port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := &config.Config{
		SFTP: config.SFTPConfig{
			Host:           "127.0.0.1",
			Port:           port,
			User:           "testuser",
			Password:       "testpass",
			TimeoutSeconds: 1,
			MaxRetries:     2,
		},
	}

	mgr := sftpclient.NewSFTPManager(cfg)
	mgr.SetRetryDelay(5 * time.Millisecond)

	err = mgr.Connect()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "falha ao conectar ao SFTP após 2 tentativas")
}

func startTestSFTPServer(t *testing.T, workDir string) (string, int, string, func()) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	signer, err := ssh.NewSignerFromKey(privateKey)
	require.NoError(t, err)

	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "testuser" && string(pass) == "testpass" {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected")
		},
	}
	serverConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go func(c net.Conn) {
				defer c.Close()
				sConn, chans, reqs, err := ssh.NewServerConn(c, serverConfig)
				if err != nil {
					return
				}
				defer sConn.Close()
				go ssh.DiscardRequests(reqs)

				for newChannel := range chans {
					if newChannel.ChannelType() != "session" {
						_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
						continue
					}
					channel, requests, err := newChannel.Accept()
					if err != nil {
						return
					}

					go func(in <-chan *ssh.Request) {
						defer channel.Close()
						for req := range in {
							ok := false
							if req.Type == "subsystem" && len(req.Payload) >= 4 && string(req.Payload[4:]) == "sftp" {
								ok = true
							}
							_ = req.Reply(ok, nil)
							if ok {
								server, err := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(workDir))
								if err == nil {
									_ = server.Serve()
								}
								return
							}
						}
					}(requests)
				}
			}(conn)
		}
	}()

	cleanup := func() {
		_ = listener.Close()
	}

	return "127.0.0.1", port, ssh.FingerprintSHA256(signer.PublicKey()), cleanup
}

func TestSFTPManager_Integration_Connect_List_Download(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup directory structure
	bal1Dir := filepath.Join(tmpDir, "bal1")
	bal2Dir := filepath.Join(tmpDir, "bal2")
	otherDir := filepath.Join(tmpDir, "other")
	require.NoError(t, os.MkdirAll(bal1Dir, 0755))
	require.NoError(t, os.MkdirAll(bal2Dir, 0755))
	require.NoError(t, os.MkdirAll(otherDir, 0755))

	// Create root file (should NOT appear in ListFolders)
	rootFile := filepath.Join(tmpDir, "root_file.txt")
	require.NoError(t, os.WriteFile(rootFile, []byte("root"), 0644))

	// Create files in bal1
	content1 := "linha1\nlinha2\nlinha3"
	file1 := filepath.Join(bal1Dir, "itensmgv.txt")
	require.NoError(t, os.WriteFile(file1, []byte(content1), 0644))

	file2 := filepath.Join(bal1Dir, "test.csv")
	require.NoError(t, os.WriteFile(file2, []byte("a,b,c"), 0644))

	// Create a subfolder inside bal1 (should NOT appear in ListFiles)
	subDirInBal1 := filepath.Join(bal1Dir, "nested_folder")
	require.NoError(t, os.MkdirAll(subDirInBal1, 0755))

	// Start SFTP test server
	host, port, fingerprint, cleanup := startTestSFTPServer(t, tmpDir)
	defer cleanup()

	cfg := &config.Config{
		SFTP: config.SFTPConfig{
			Host:           host,
			Port:           port,
			User:           "testuser",
			Password:       "testpass",
			HostKeySHA256:  fingerprint,
			TimeoutSeconds: 5,
			MaxRetries:     2,
		},
	}

	mgr := sftpclient.NewSFTPManager(cfg)
	mgr.SetRetryDelay(5 * time.Millisecond)

	// Connect
	err := mgr.Connect()
	require.NoError(t, err)
	defer mgr.Close()

	// 1. ListFolders: should list directories in root, excluding . and .. and regular files, sorted
	folders, err := mgr.ListFolders(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, []string{"bal1", "bal2", "other"}, folders)

	// 2. ListFiles: should list regular files in bal1, excluding subdirectories
	files, err := mgr.ListFiles(bal1Dir)
	require.NoError(t, err)
	require.Len(t, files, 2)

	fileNames := []string{files[0].Name, files[1].Name}
	assert.Contains(t, fileNames, "itensmgv.txt")
	assert.Contains(t, fileNames, "test.csv")

	for _, f := range files {
		if f.Name == "itensmgv.txt" {
			assert.Equal(t, int64(len(content1)), f.Size)
			assert.False(t, f.ModTime.IsZero())
		}
	}

	// 3. DownloadFile: successfully downloads remote file content
	var downloadBuf bytes.Buffer
	remoteFilePath := filepath.Join(bal1Dir, "itensmgv.txt")
	err = mgr.DownloadFile(remoteFilePath, &downloadBuf)
	require.NoError(t, err)
	assert.Equal(t, content1, downloadBuf.String())

	// 4. DownloadFile: non-existent file returns error
	var errBuf bytes.Buffer
	nonExistentPath := filepath.Join(bal1Dir, "nao_existe.txt")
	err = mgr.DownloadFile(nonExistentPath, &errBuf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erro ao abrir arquivo remoto")

	// 5. Close cleanly
	mgr.Close()

	// Operations after Close should fail
	_, err = mgr.ListFolders(tmpDir)
	assert.Error(t, err)
}
