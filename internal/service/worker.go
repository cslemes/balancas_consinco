package service

import (
	"context"
	"fmt"
	"io"
	"log"
	"path"
	"path/filepath"
	"time"

	"balancas/internal/config"
	"balancas/internal/mapper"
	"balancas/internal/pathguard"
	"balancas/internal/sftpclient"
	"balancas/internal/syncer"
)

// SyncStats consolida as métricas de execução de um ciclo de sincronização.
type SyncStats struct {
	FoldersChecked int
	Downloaded     int
	Planned        int
	Skipped        int
	Errors         int
}

// Worker encapsula a configuração e execução do ciclo de sincronização.
type Worker struct {
	cfg *config.Config
}

// NewWorker cria uma nova instância de Worker associada à configuração fornecida.
func NewWorker(cfg *config.Config) *Worker {
	return &Worker{cfg: cfg}
}

// RunSyncCycle executa o ciclo de sincronização para a configuração associada ao Worker.
func (w *Worker) RunSyncCycle(ctx context.Context) (SyncStats, error) {
	return RunSyncCycle(ctx, w.cfg)
}

// RunSyncCycle executa uma varredura completa nas pastas remotas do SFTP e sincroniza os arquivos localmente.
func RunSyncCycle(ctx context.Context, cfg *config.Config) (SyncStats, error) {
	var stats SyncStats
	if cfg == nil {
		return stats, fmt.Errorf("configuração não informada")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		log.Println("Ciclo de sincronização cancelado")
		return stats, err
	}

	startTime := time.Now()

	log.Println("============================================================")
	log.Printf("Iniciando ciclo de sincronização para %s...", cfg.Sync.LocalBaseDir)

	fMapper, err := mapper.NewFolderMapper(cfg.Sync.FolderPattern)
	if err != nil {
		return stats, err
	}

	fSyncer := syncer.NewFileSyncer(cfg.Sync.MtimeToleranceSeconds)
	sftpMgr := sftpclient.NewSFTPManager(cfg)

	if err := sftpMgr.ConnectContext(ctx); err != nil {
		return stats, err
	}
	defer sftpMgr.Close()
	stopOnCancel := sftpMgr.CloseOnContext(ctx)
	defer stopOnCancel()

	if err := ctx.Err(); err != nil {
		log.Println("Ciclo de sincronização cancelado")
		return stats, err
	}

	remoteFolders, err := sftpMgr.ListFolders(cfg.Sync.RemoteBaseDir)
	if err != nil {
		log.Printf("[ERRO] Falha ao listar pastas remotas em %s: %v", cfg.Sync.RemoteBaseDir, err)
		return stats, err
	}

	for _, rFolder := range remoteFolders {
		if err := ctx.Err(); err != nil {
			log.Println("Ciclo de sincronização cancelado")
			return stats, err
		}

		localFolder, ok := fMapper.GetLocalFolder(cfg.Sync.LocalBaseDir, rFolder)
		if !ok {
			continue
		}

		stats.FoldersChecked++
		rFolderPath := path.Join(cfg.Sync.RemoteBaseDir, rFolder)
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
			if err := ctx.Err(); err != nil {
				log.Println("Ciclo de sincronização cancelado")
				return stats, err
			}
			if !pathguard.IsSafeComponent(file.Name) {
				log.Printf("[AVISO] Nome remoto inválido ignorado em %s: %q", rFolderPath, file.Name)
				continue
			}

			rFilePath := path.Join(rFolderPath, file.Name)
			lFilePath := filepath.Join(localFolder, file.Name)

			downloadFunc := func(dst io.Writer) error {
				return sftpMgr.DownloadFile(rFilePath, dst)
			}

			status, err := fSyncer.SyncFile(downloadFunc, lFilePath, file.Size, file.ModTime, cfg.Sync.DryRun)
			if err != nil {
				log.Printf("[ERRO] Falha ao sincronizar %s: %v", rFilePath, err)
				stats.Errors++
			} else if status == syncer.StatusDownloaded {
				if cfg.Sync.DryRun {
					log.Printf("[DRY-RUN] Planejado: %s -> %s (%d bytes)", rFilePath, file.Name, file.Size)
					stats.Planned++
				} else {
					log.Printf("[BAIXADO] %s -> %s (%d bytes)", rFilePath, file.Name, file.Size)
					stats.Downloaded++
				}
			} else {
				stats.Skipped++
			}
		}
	}

	elapsed := time.Since(startTime)
	log.Printf("Ciclo concluído em %v | Pastas: %d | Baixados: %d | Planejados: %d | Ignorados: %d | Erros: %d",
		elapsed.Round(time.Millisecond), stats.FoldersChecked, stats.Downloaded, stats.Planned, stats.Skipped, stats.Errors)
	log.Println("============================================================")

	return stats, nil
}
