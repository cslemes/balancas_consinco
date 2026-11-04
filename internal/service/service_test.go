package service_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cslemes/balancas_consinco/internal/config"
	"github.com/cslemes/balancas_consinco/internal/service"
	kservice "github.com/kardianos/service"
	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

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
								srv, err := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(workDir))
								if err == nil {
									_ = srv.Serve()
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

func TestWindowsService_ImplementsInterface(t *testing.T) {
	cfg := &config.Config{}
	svc := service.NewWindowsService(cfg)
	var _ kservice.Interface = svc
	assert.NotNil(t, svc)
}

func TestWindowsService_StartStop(t *testing.T) {
	var runCount int32
	cfg := &config.Config{
		Sync: config.SyncConfig{
			IntervalSeconds: 1, // 1 second
		},
	}

	svc := service.NewWindowsService(cfg)
	svc.SetSyncFunc(func(ctx context.Context, c *config.Config) (service.SyncStats, error) {
		atomic.AddInt32(&runCount, 1)
		return service.SyncStats{}, nil
	})

	err := svc.Start(nil)
	require.NoError(t, err)

	// Wait for immediate first run
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&runCount) >= 1
	}, 1*time.Second, 10*time.Millisecond)

	err = svc.Stop(nil)
	require.NoError(t, err)

	countAfterStop := atomic.LoadInt32(&runCount)
	time.Sleep(100 * time.Millisecond)
	// Verify no more runs after stop
	assert.Equal(t, countAfterStop, atomic.LoadInt32(&runCount))
}

func TestWindowsService_TickerExecution(t *testing.T) {
	var runCount int32
	cfg := &config.Config{
		Sync: config.SyncConfig{
			IntervalSeconds: 0, // default should be used when 0
		},
	}

	svc := service.NewWindowsService(cfg)
	svc.SetInterval(50 * time.Millisecond) // test helper for fast tick
	svc.SetSyncFunc(func(ctx context.Context, c *config.Config) (service.SyncStats, error) {
		atomic.AddInt32(&runCount, 1)
		return service.SyncStats{}, nil
	})

	err := svc.Start(nil)
	require.NoError(t, err)

	// Wait for multiple runs (immediate + ticker)
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&runCount) >= 3
	}, 1*time.Second, 20*time.Millisecond)

	err = svc.Stop(nil)
	require.NoError(t, err)
}

func TestWindowsService_GracefulStopWaitsForCycle(t *testing.T) {
	var inProgress int32
	var finished int32

	cfg := &config.Config{
		Sync: config.SyncConfig{
			IntervalSeconds: 60,
		},
	}

	svc := service.NewWindowsService(cfg)
	svc.SetSyncFunc(func(ctx context.Context, c *config.Config) (service.SyncStats, error) {
		atomic.StoreInt32(&inProgress, 1)
		// Simula processamento que checa contexto
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
		}
		atomic.StoreInt32(&finished, 1)
		atomic.StoreInt32(&inProgress, 0)
		return service.SyncStats{}, ctx.Err()
	})

	require.NoError(t, svc.Start(nil))

	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&inProgress) == 1
	}, 1*time.Second, 10*time.Millisecond)

	// Ao chamar Stop, deve aguardar a goroutine concluir
	require.NoError(t, svc.Stop(nil))
	assert.Equal(t, int32(1), atomic.LoadInt32(&finished))
	assert.Equal(t, int32(0), atomic.LoadInt32(&inProgress))
}

func TestRunSyncCycle_NilConfig(t *testing.T) {
	stats, err := service.RunSyncCycle(context.Background(), nil)
	assert.Error(t, err)
	assert.Equal(t, 0, stats.FoldersChecked)
}

func TestRunSyncCycle_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled ahead of time

	cfg := &config.Config{
		Sync: config.SyncConfig{
			FolderPattern: `(?i)^bal(\d+)$`,
		},
	}
	stats, err := service.RunSyncCycle(ctx, cfg)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, stats.FoldersChecked)
}

func TestRunSyncCycle_InvalidPattern(t *testing.T) {
	cfg := &config.Config{
		Sync: config.SyncConfig{
			FolderPattern: "[invalid(regex",
		},
	}
	stats, err := service.RunSyncCycle(context.Background(), cfg)
	assert.Error(t, err)
	assert.Equal(t, 0, stats.FoldersChecked)
}

func TestRunSyncCycle_SFTPConnectFailure(t *testing.T) {
	cfg := &config.Config{
		SFTP: config.SFTPConfig{
			Host:           "127.0.0.1",
			Port:           1, // Unlikely to have SFTP running on port 1
			User:           "user",
			Password:       "pass",
			TimeoutSeconds: 1,
			MaxRetries:     1,
		},
		Sync: config.SyncConfig{
			FolderPattern: `(?i)^bal(\d+)$`,
		},
	}
	stats, err := service.RunSyncCycle(context.Background(), cfg)
	assert.Error(t, err)
	assert.Equal(t, 0, stats.FoldersChecked)
}

func TestRunSyncCycle_Integration_FullCycle(t *testing.T) {
	remoteDir := t.TempDir()
	localDir := t.TempDir()

	// Setup remote folders
	bal1Remote := filepath.Join(remoteDir, "bal1")
	bal2Remote := filepath.Join(remoteDir, "bal2")
	ignoredRemote := filepath.Join(remoteDir, "unrelated_dir")

	require.NoError(t, os.MkdirAll(bal1Remote, 0755))
	require.NoError(t, os.MkdirAll(bal2Remote, 0755))
	require.NoError(t, os.MkdirAll(ignoredRemote, 0755))

	// Write remote files
	file1Content := []byte("conteudo balanca 1")
	file2Content := []byte("conteudo balanca 2")
	require.NoError(t, os.WriteFile(filepath.Join(bal1Remote, "itensmgv.txt"), file1Content, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(bal2Remote, "itensmgv.txt"), file2Content, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(ignoredRemote, "ignored.txt"), []byte("ignored"), 0644))

	host, port, fingerprint, cleanup := startTestSFTPServer(t, remoteDir)
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
		Sync: config.SyncConfig{
			RemoteBaseDir:         ".",
			LocalBaseDir:          localDir,
			FolderPattern:         `(?i)^bal(\d+)$`,
			MtimeToleranceSeconds: 2,
			DryRun:                false,
		},
	}

	// 1. Initial Sync: Should download files from bal1 and bal2, ignore unrelated_dir
	stats, err := service.RunSyncCycle(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.FoldersChecked)
	assert.Equal(t, 2, stats.Downloaded)
	assert.Equal(t, 0, stats.Skipped)
	assert.Equal(t, 0, stats.Errors)

	// Verify local files exist and have correct content
	localBal1File := filepath.Join(localDir, "1", "itensmgv.txt")
	localBal2File := filepath.Join(localDir, "2", "itensmgv.txt")

	data1, err := os.ReadFile(localBal1File)
	require.NoError(t, err)
	assert.Equal(t, file1Content, data1)

	data2, err := os.ReadFile(localBal2File)
	require.NoError(t, err)
	assert.Equal(t, file2Content, data2)
	_, err = os.Stat(filepath.Join(bal1Remote, "itensmgv.txt"))
	assert.True(t, os.IsNotExist(err), "remote file should be removed after a successful download")
	_, err = os.Stat(filepath.Join(bal2Remote, "itensmgv.txt"))
	assert.True(t, os.IsNotExist(err), "remote file should be removed after a successful download")

	// 2. Second Sync: Remote files were removed after successful downloads.
	stats2, err := service.RunSyncCycle(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, 2, stats2.FoldersChecked)
	assert.Equal(t, 0, stats2.Downloaded)
	assert.Equal(t, 0, stats2.Skipped)
	assert.Equal(t, 0, stats2.Errors)

	// 3. Worker struct wrapper test
	worker := service.NewWorker(cfg)
	statsWorker, err := worker.RunSyncCycle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, statsWorker.FoldersChecked)
	assert.Equal(t, 0, statsWorker.Downloaded)
	assert.Equal(t, 0, statsWorker.Skipped)
	assert.Equal(t, 0, statsWorker.Errors)
}

func TestRunSyncCycle_DryRun(t *testing.T) {
	remoteDir := t.TempDir()
	localDir := t.TempDir()

	bal1Remote := filepath.Join(remoteDir, "bal1")
	require.NoError(t, os.MkdirAll(bal1Remote, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bal1Remote, "itensmgv.txt"), []byte("dry run test"), 0644))

	host, port, fingerprint, cleanup := startTestSFTPServer(t, remoteDir)
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
		Sync: config.SyncConfig{
			RemoteBaseDir:         ".",
			LocalBaseDir:          localDir,
			FolderPattern:         `(?i)^bal(\d+)$`,
			MtimeToleranceSeconds: 2,
			DryRun:                true,
		},
	}

	stats, err := service.RunSyncCycle(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.FoldersChecked)
	assert.Equal(t, 0, stats.Downloaded)
	assert.Equal(t, 1, stats.Planned)
	assert.Equal(t, 0, stats.Skipped)
	assert.Equal(t, 0, stats.Errors)

	// Verify local file was NOT created in dry-run
	localBal1File := filepath.Join(localDir, "1", "itensmgv.txt")
	_, err = os.Stat(localBal1File)
	assert.True(t, os.IsNotExist(err), "file should not exist in dry-run mode")
}
