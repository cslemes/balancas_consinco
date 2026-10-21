import sys
from pathlib import Path

# Ensure project root is in sys.path
sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from unittest.mock import MagicMock, patch
import pytest

from config import Config
from sync_balancas import run_sync_cycle, setup_logger, main


def test_run_sync_cycle_orchestration(tmp_path):
    cfg = Config(
        sftp_host="example.com",
        sftp_port=22,
        sftp_user="user",
        sftp_password="password",
        local_base_dir=tmp_path / "balancas",
        folder_regex=r"(?i)^bal(\d+)$",
        dry_run=False
    )

    with patch("sync_balancas.SFTPManager") as MockSFTPManager:
        mock_instance = MockSFTPManager.return_value.__enter__.return_value
        mock_instance.list_folders.return_value = ["BAL01", "BAL03", "OUTRA_PASTA"]
        mock_instance.list_files.side_effect = [
            [("txitens.txt", 100, 1700000000)],   # BAL01
            [("itensmgv.txt", 200, 1700000000)],  # BAL03
        ]

        with patch("sync_balancas.FileSyncer.sync_file") as mock_sync_file:
            mock_sync_file.return_value = "downloaded"
            stats = run_sync_cycle(cfg)

            assert stats["folders_checked"] == 2
            assert stats["downloaded"] == 2
            assert stats["skipped"] == 0
            assert stats["errors"] == 0

            # Verify sync_file arguments
            assert mock_sync_file.call_count == 2
            call1 = mock_sync_file.call_args_list[0]
            assert call1.kwargs["remote_path"] == "BAL01/txitens.txt"
            assert call1.kwargs["local_path"] == tmp_path / "balancas" / "01" / "txitens.txt"
            assert call1.kwargs["dry_run"] is False


def test_run_sync_cycle_handles_skipped_and_error(tmp_path):
    cfg = Config(
        sftp_host="example.com",
        sftp_port=22,
        sftp_user="user",
        sftp_password="password",
        local_base_dir=tmp_path / "balancas",
        folder_regex=r"(?i)^bal(\d+)$",
        dry_run=False
    )

    with patch("sync_balancas.SFTPManager") as MockSFTPManager:
        mock_instance = MockSFTPManager.return_value.__enter__.return_value
        mock_instance.list_folders.return_value = ["BAL01"]
        mock_instance.list_files.return_value = [
            ("file1.txt", 100, 1700000000),
            ("file2.txt", 200, 1700000000),
            ("file3.txt", 300, 1700000000),
        ]

        with patch("sync_balancas.FileSyncer.sync_file") as mock_sync_file:
            mock_sync_file.side_effect = ["downloaded", "skipped", "error"]
            stats = run_sync_cycle(cfg)

            assert stats["folders_checked"] == 1
            assert stats["downloaded"] == 1
            assert stats["skipped"] == 1
            assert stats["errors"] == 1


def test_run_sync_cycle_list_files_exception(tmp_path):
    cfg = Config(
        sftp_host="example.com",
        sftp_port=22,
        sftp_user="user",
        sftp_password="password",
        local_base_dir=tmp_path / "balancas",
        folder_regex=r"(?i)^bal(\d+)$",
        dry_run=False
    )

    with patch("sync_balancas.SFTPManager") as MockSFTPManager:
        mock_instance = MockSFTPManager.return_value.__enter__.return_value
        mock_instance.list_folders.return_value = ["BAL01", "BAL02"]
        mock_instance.list_files.side_effect = [
            IOError("Remote disk read error"),
            [("file.txt", 100, 1700000000)]
        ]

        with patch("sync_balancas.FileSyncer.sync_file") as mock_sync_file:
            mock_sync_file.return_value = "downloaded"
            stats = run_sync_cycle(cfg)

            assert stats["folders_checked"] == 2
            assert stats["downloaded"] == 1
            assert stats["errors"] == 1


def test_run_sync_cycle_custom_remote_base_dir(tmp_path):
    cfg = Config(
        sftp_host="example.com",
        sftp_port=22,
        sftp_user="user",
        sftp_password="password",
        local_base_dir=tmp_path / "balancas",
        remote_base_dir="/exports/totvs",
        folder_regex=r"(?i)^bal(\d+)$",
        dry_run=True
    )

    with patch("sync_balancas.SFTPManager") as MockSFTPManager:
        mock_instance = MockSFTPManager.return_value.__enter__.return_value
        mock_instance.list_folders.return_value = ["BAL05"]
        mock_instance.list_files.return_value = [("itens.txt", 50, 1700000000)]

        with patch("sync_balancas.FileSyncer.sync_file") as mock_sync_file:
            mock_sync_file.return_value = "downloaded"
            stats = run_sync_cycle(cfg)

            mock_instance.list_folders.assert_called_once_with("/exports/totvs")
            mock_instance.list_files.assert_called_once_with("/exports/totvs/BAL05")
            assert mock_sync_file.call_args.kwargs["dry_run"] is True


def test_setup_logger(tmp_path):
    log_dir = tmp_path / "test_logs"
    logger = setup_logger("DEBUG", log_dir=str(log_dir))
    assert logger.name == "sync_balancas"
    assert (log_dir / "sync_balancas.log").exists() or any(
        h.__class__.__name__ == "RotatingFileHandler" for h in logger.handlers
    )


def test_main_single_run_success():
    test_args = ["sync_balancas.py", "--dry-run"]
    with patch.object(sys, "argv", test_args):
        with patch("sync_balancas.load_config") as mock_load_config:
            mock_cfg = MagicMock()
            mock_cfg.sync_interval_seconds = 0
            mock_load_config.return_value = mock_cfg
            with patch("sync_balancas.run_sync_cycle") as mock_run_cycle:
                mock_run_cycle.return_value = {"folders_checked": 1, "downloaded": 1, "skipped": 0, "errors": 0}
                with pytest.raises(SystemExit) as exc_info:
                    main()
                assert exc_info.value.code == 0
                mock_run_cycle.assert_called_once_with(mock_cfg)


def test_main_single_run_with_errors():
    test_args = ["sync_balancas.py"]
    with patch.object(sys, "argv", test_args):
        with patch("sync_balancas.load_config") as mock_load_config:
            mock_cfg = MagicMock()
            mock_cfg.sync_interval_seconds = 0
            mock_load_config.return_value = mock_cfg
            with patch("sync_balancas.run_sync_cycle") as mock_run_cycle:
                mock_run_cycle.return_value = {"folders_checked": 1, "downloaded": 0, "skipped": 0, "errors": 1}
                with pytest.raises(SystemExit) as exc_info:
                    main()
                assert exc_info.value.code == 2


def test_main_config_error():
    test_args = ["sync_balancas.py"]
    with patch.object(sys, "argv", test_args):
        with patch("sync_balancas.load_config", side_effect=ValueError("Host missing")):
            with pytest.raises(SystemExit) as exc_info:
                main()
            assert exc_info.value.code == 1


def test_main_loop_keyboard_interrupt():
    test_args = ["sync_balancas.py", "--loop", "--interval", "10"]
    with patch.object(sys, "argv", test_args):
        with patch("sync_balancas.load_config") as mock_load_config:
            mock_cfg = MagicMock()
            mock_cfg.sync_interval_seconds = 10
            mock_load_config.return_value = mock_cfg
            with patch("sync_balancas.run_sync_cycle") as mock_run_cycle:
                mock_run_cycle.side_effect = KeyboardInterrupt()
                # Should handle KeyboardInterrupt and return normally without exception
                main()
                mock_run_cycle.assert_called_once_with(mock_cfg)
