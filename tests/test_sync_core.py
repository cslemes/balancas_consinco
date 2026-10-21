import os
import sys
from pathlib import Path
from unittest.mock import MagicMock

# Ensure project root is in sys.path
sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import pytest
from sync_core import FileSyncer


def test_should_download_when_local_missing(tmp_path):
    syncer = FileSyncer()
    local_file = tmp_path / "itensmgv.txt"
    assert syncer.should_download(local_file, remote_size=100, remote_mtime=1700000000) is True


def test_should_skip_when_size_and_mtime_match(tmp_path):
    syncer = FileSyncer()
    local_file = tmp_path / "itensmgv.txt"
    local_file.write_bytes(b"hello world")
    size = len(b"hello world")
    mtime = 1700000000
    os.utime(local_file, (mtime, mtime))

    assert syncer.should_download(local_file, remote_size=size, remote_mtime=mtime) is False


def test_should_download_when_size_differs(tmp_path):
    syncer = FileSyncer()
    local_file = tmp_path / "itensmgv.txt"
    local_file.write_bytes(b"hello")
    mtime = 1700000000
    os.utime(local_file, (mtime, mtime))

    assert syncer.should_download(local_file, remote_size=999, remote_mtime=mtime) is True


def test_should_download_when_mtime_differs_beyond_tolerance(tmp_path):
    syncer = FileSyncer(mtime_tolerance_seconds=2)
    local_file = tmp_path / "itensmgv.txt"
    local_file.write_bytes(b"hello")
    size = len(b"hello")
    os.utime(local_file, (1700000000, 1700000000))

    # Difference of 3 seconds (> 2s tolerance)
    assert syncer.should_download(local_file, remote_size=size, remote_mtime=1700000003) is True


def test_should_download_when_stat_raises_oserror(tmp_path, monkeypatch):
    syncer = FileSyncer()
    local_file = tmp_path / "itensmgv.txt"
    local_file.write_bytes(b"hello")

    def fake_stat(self):
        raise OSError("Permission denied on stat")

    monkeypatch.setattr(Path, "stat", fake_stat)
    assert syncer.should_download(local_file, remote_size=5, remote_mtime=1700000000) is True


def test_should_skip_when_mtime_within_tolerance(tmp_path):
    syncer = FileSyncer(mtime_tolerance_seconds=2)
    local_file = tmp_path / "itensmgv.txt"
    local_file.write_bytes(b"hello")
    size = len(b"hello")
    os.utime(local_file, (1700000000, 1700000000))

    # Difference of 1 second (<= 2s tolerance)
    assert syncer.should_download(local_file, remote_size=size, remote_mtime=1700000001) is False


def test_sync_file_atomic_download(tmp_path):
    syncer = FileSyncer()
    mock_sftp = MagicMock()
    local_file = tmp_path / "itensmgv.txt"

    def fake_get(rem, loc):
        Path(loc).write_bytes(b"downloaded content")

    mock_sftp.get.side_effect = fake_get

    status = syncer.sync_file(
        sftp_client=mock_sftp,
        remote_path="/BAL01/itensmgv.txt",
        local_path=local_file,
        remote_size=18,
        remote_mtime=1700000000,
        dry_run=False
    )

    assert status == "downloaded"
    assert local_file.exists()
    assert local_file.read_bytes() == b"downloaded content"
    assert int(local_file.stat().st_mtime) == 1700000000
    # Temporary file should not exist
    assert not local_file.with_suffix(local_file.suffix + ".tmp").exists()


def test_sync_file_skipped_when_up_to_date(tmp_path):
    syncer = FileSyncer()
    mock_sftp = MagicMock()
    local_file = tmp_path / "itensmgv.txt"
    local_file.write_bytes(b"content")
    mtime = 1700000000
    os.utime(local_file, (mtime, mtime))

    status = syncer.sync_file(
        sftp_client=mock_sftp,
        remote_path="/BAL01/itensmgv.txt",
        local_path=local_file,
        remote_size=len(b"content"),
        remote_mtime=mtime,
        dry_run=False
    )

    assert status == "skipped"
    mock_sftp.get.assert_not_called()


def test_sync_file_dry_run(tmp_path):
    syncer = FileSyncer()
    mock_sftp = MagicMock()
    local_file = tmp_path / "itensmgv.txt"

    status = syncer.sync_file(
        sftp_client=mock_sftp,
        remote_path="/BAL01/itensmgv.txt",
        local_path=local_file,
        remote_size=100,
        remote_mtime=1700000000,
        dry_run=True
    )

    assert status == "downloaded"
    mock_sftp.get.assert_not_called()
    assert not local_file.exists()


def test_sync_file_permission_error_locked_file(tmp_path, monkeypatch):
    syncer = FileSyncer()
    mock_sftp = MagicMock()
    local_file = tmp_path / "itensmgv.txt"

    def fake_get(rem, loc):
        Path(loc).write_bytes(b"new content")

    mock_sftp.get.side_effect = fake_get

    # Simulate PermissionError on replace (e.g. file locked by Windows process)
    def fake_replace(self, target):
        raise PermissionError("Access is denied (locked by Toledo MGV)")

    monkeypatch.setattr(Path, "replace", fake_replace)

    status = syncer.sync_file(
        sftp_client=mock_sftp,
        remote_path="/BAL01/itensmgv.txt",
        local_path=local_file,
        remote_size=11,
        remote_mtime=1700000000,
        dry_run=False
    )

    assert status == "error"
    # tmp file must be cleaned up
    tmp_path_file = local_file.with_suffix(local_file.suffix + ".tmp")
    assert not tmp_path_file.exists()


def test_sync_file_general_exception_cleans_up(tmp_path):
    syncer = FileSyncer()
    mock_sftp = MagicMock()
    local_file = tmp_path / "itensmgv.txt"

    def fake_get_error(rem, loc):
        Path(loc).write_bytes(b"partial content")
        raise ConnectionResetError("Connection lost mid-download")

    mock_sftp.get.side_effect = fake_get_error

    status = syncer.sync_file(
        sftp_client=mock_sftp,
        remote_path="/BAL01/itensmgv.txt",
        local_path=local_file,
        remote_size=100,
        remote_mtime=1700000000,
        dry_run=False
    )

    assert status == "error"
    tmp_path_file = local_file.with_suffix(local_file.suffix + ".tmp")
    assert not tmp_path_file.exists()
