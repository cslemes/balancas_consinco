package syncer_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cslemes/balancas_consinco/internal/syncer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileSyncer_DefaultTolerance(t *testing.T) {
	sDefault := syncer.NewFileSyncer(0)
	assert.Equal(t, 2*time.Second, sDefault.Tolerance)

	sNegative := syncer.NewFileSyncer(-5)
	assert.Equal(t, 2*time.Second, sNegative.Tolerance)

	sCustom := syncer.NewFileSyncer(10)
	assert.Equal(t, 10*time.Second, sCustom.Tolerance)
}

func TestShouldDownload_WhenFileDoesNotExist(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "itensmgv.txt")

	assert.True(t, s.ShouldDownload(localFile, 100, time.Now()))
}

func TestShouldSkip_WhenSizeAndMtimeMatch(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "itensmgv.txt")

	content := []byte("hello world")
	err := os.WriteFile(localFile, content, 0644)
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second)
	err = os.Chtimes(localFile, now, now)
	require.NoError(t, err)

	assert.False(t, s.ShouldDownload(localFile, int64(len(content)), now))
}

func TestShouldDownload_WhenSizeDiffers(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "itensmgv.txt")

	content := []byte("hello world")
	err := os.WriteFile(localFile, content, 0644)
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second)
	err = os.Chtimes(localFile, now, now)
	require.NoError(t, err)

	// Remote size is different (e.g. 50 instead of 11)
	assert.True(t, s.ShouldDownload(localFile, 50, now))
}

func TestShouldDownload_WhenMtimeDiffersBeyondTolerance(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "itensmgv.txt")

	content := []byte("hello world")
	err := os.WriteFile(localFile, content, 0644)
	require.NoError(t, err)

	localTime := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	err = os.Chtimes(localFile, localTime, localTime)
	require.NoError(t, err)

	remoteTime := localTime.Add(10 * time.Second) // 10s difference > 2s tolerance
	assert.True(t, s.ShouldDownload(localFile, int64(len(content)), remoteTime))
}

func TestShouldSkip_WhenMtimeWithinTolerance(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "itensmgv.txt")

	content := []byte("hello world")
	err := os.WriteFile(localFile, content, 0644)
	require.NoError(t, err)

	localTime := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	err = os.Chtimes(localFile, localTime, localTime)
	require.NoError(t, err)

	remoteTime := localTime.Add(1 * time.Second) // 1s diff <= 2s tolerance
	assert.False(t, s.ShouldDownload(localFile, int64(len(content)), remoteTime))
}

func TestDownloadAtomic_WritesAndSetsTimestamp(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "subdir", "itensmgv.txt") // tests dir creation too

	content := []byte("downloaded atomic content")
	targetTime := time.Date(2026, 10, 20, 15, 30, 0, 0, time.Local)

	err := s.DownloadAtomic(bytes.NewReader(content), localFile, targetTime)
	require.NoError(t, err)

	readContent, err := os.ReadFile(localFile)
	require.NoError(t, err)
	assert.Equal(t, content, readContent)

	info, err := os.Stat(localFile)
	require.NoError(t, err)
	assert.WithinDuration(t, targetTime, info.ModTime(), 2*time.Second)
}

type errReader struct{}

func (e *errReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read failure")
}

func TestDownloadAtomic_CleansUpOnReadError(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "itensmgv.txt")

	err := s.DownloadAtomic(&errReader{}, localFile, time.Now())
	require.Error(t, err)

	// Ensure localFile was not created
	_, statErr := os.Stat(localFile)
	assert.True(t, os.IsNotExist(statErr))

	// Ensure no tmp files left in directory
	entries, readDirErr := os.ReadDir(tmpDir)
	require.NoError(t, readDirErr)
	assert.Empty(t, entries)
}

func TestSyncFile_WhenNoDownloadNeeded(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "itensmgv.txt")

	content := []byte("hello world")
	err := os.WriteFile(localFile, content, 0644)
	require.NoError(t, err)
	now := time.Now().Truncate(time.Second)
	err = os.Chtimes(localFile, now, now)
	require.NoError(t, err)

	called := false
	downloadFunc := func(dst io.Writer) error {
		called = true
		return nil
	}

	status, err := s.SyncFile(downloadFunc, localFile, int64(len(content)), now, false)
	require.NoError(t, err)
	assert.Equal(t, syncer.StatusSkipped, status)
	assert.False(t, called)
}

func TestSyncFile_DryRun(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "itensmgv.txt")

	called := false
	downloadFunc := func(dst io.Writer) error {
		called = true
		return nil
	}

	status, err := s.SyncFile(downloadFunc, localFile, 100, time.Now(), true)
	require.NoError(t, err)
	assert.Equal(t, syncer.StatusDownloaded, status)
	assert.False(t, called)

	// Local file must NOT exist after dry-run
	_, statErr := os.Stat(localFile)
	assert.True(t, os.IsNotExist(statErr))
}

func TestSyncFile_DownloadSuccess(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "newdir", "itensmgv.txt")

	content := []byte("sync file content payload")
	remoteTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.Local)

	downloadFunc := func(dst io.Writer) error {
		_, writeErr := dst.Write(content)
		return writeErr
	}

	status, err := s.SyncFile(downloadFunc, localFile, int64(len(content)), remoteTime, false)
	require.NoError(t, err)
	assert.Equal(t, syncer.StatusDownloaded, status)

	readContent, err := os.ReadFile(localFile)
	require.NoError(t, err)
	assert.Equal(t, content, readContent)

	info, err := os.Stat(localFile)
	require.NoError(t, err)
	assert.WithinDuration(t, remoteTime, info.ModTime(), 2*time.Second)
}

func TestSyncFile_DownloadFuncError(t *testing.T) {
	s := syncer.NewFileSyncer(2)
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "itensmgv.txt")

	expectedErr := errors.New("download connection dropped")
	downloadFunc := func(dst io.Writer) error {
		_, _ = dst.Write([]byte("partial"))
		return expectedErr
	}

	status, err := s.SyncFile(downloadFunc, localFile, 100, time.Now(), false)
	require.ErrorIs(t, err, expectedErr)
	assert.Equal(t, syncer.StatusError, status)

	// Ensure local file not created
	_, statErr := os.Stat(localFile)
	assert.True(t, os.IsNotExist(statErr))

	// Ensure no .tmp files remain
	entries, readDirErr := os.ReadDir(tmpDir)
	require.NoError(t, readDirErr)
	assert.Empty(t, entries)
}
