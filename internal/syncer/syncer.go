package syncer

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"
)

type SyncStatus int

const (
	StatusDownloaded SyncStatus = iota
	StatusSkipped
	StatusError
)

type FileSyncer struct {
	Tolerance time.Duration
}

func NewFileSyncer(toleranceSeconds int) *FileSyncer {
	if toleranceSeconds <= 0 {
		toleranceSeconds = 2
	}
	return &FileSyncer{
		Tolerance: time.Duration(toleranceSeconds) * time.Second,
	}
}

func (s *FileSyncer) ShouldDownload(localPath string, remoteSize int64, remoteMtime time.Time) bool {
	info, err := os.Stat(localPath)
	if err != nil {
		return true // Arquivo não existe ou erro ao ler status
	}

	if info.Size() != remoteSize {
		return true
	}

	diff := math.Abs(info.ModTime().Sub(remoteMtime).Seconds())
	return diff > s.Tolerance.Seconds()
}

func (s *FileSyncer) DownloadAtomic(src io.Reader, localPath string, remoteMtime time.Time) error {
	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("falha ao criar diretório local %s: %w", dir, err)
	}

	tmpFile := fmt.Sprintf("%s.%d.tmp", localPath, os.Getpid())
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("falha ao criar arquivo temporário %s: %w", tmpFile, err)
	}

	_, copyErr := io.Copy(f, src)
	closeErr := f.Close()

	if copyErr != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("falha ao gravar dados no arquivo temporário: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("falha ao fechar arquivo temporário: %w", closeErr)
	}

	// Rename atômico
	if err := os.Rename(tmpFile, localPath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("falha ao renomear arquivo temporário para %s: %w", localPath, err)
	}

	// Ajusta mtime
	if err := os.Chtimes(localPath, remoteMtime, remoteMtime); err != nil {
		log.Printf("[AVISO] Falha ao atualizar data de modificação de %s: %v", localPath, err)
	}
	return nil
}

func (s *FileSyncer) SyncFile(
	downloadFunc func(dst io.Writer) error,
	localPath string,
	remoteSize int64,
	remoteMtime time.Time,
	dryRun bool,
) (SyncStatus, error) {
	if !s.ShouldDownload(localPath, remoteSize, remoteMtime) {
		return StatusSkipped, nil
	}

	if dryRun {
		log.Printf("[DRY-RUN] Baixaria para %s (%d bytes)", localPath, remoteSize)
		return StatusDownloaded, nil
	}

	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return StatusError, err
	}

	tmpPath := fmt.Sprintf("%s.%d.tmp", localPath, os.Getpid())
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return StatusError, err
	}

	if err := downloadFunc(tmpFile); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return StatusError, err
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return StatusError, err
	}

	if err := os.Rename(tmpPath, localPath); err != nil {
		_ = os.Remove(tmpPath)
		return StatusError, err
	}

	if err := os.Chtimes(localPath, remoteMtime, remoteMtime); err != nil {
		log.Printf("[AVISO] Falha ao atualizar data de modificação de %s: %v", localPath, err)
	}
	return StatusDownloaded, nil
}
