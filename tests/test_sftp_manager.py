import sys
from pathlib import Path
from unittest.mock import MagicMock, patch

# Ensure project root is in sys.path
sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import pytest
from config import Config
from sftp_manager import SFTPManager


def test_sftp_manager_list_folders():
    cfg = Config(
        sftp_host="localhost",
        sftp_port=22,
        sftp_user="user",
        sftp_password="pass",
        local_base_dir=Path("/tmp"),
    )
    manager = SFTPManager(cfg)
    mock_sftp = MagicMock()

    # Mock de atributos de diretórios
    attr_dir1 = MagicMock(filename="BAL01", st_mode=0o40755)
    attr_dir2 = MagicMock(filename="BAL03", st_mode=0o40755)
    attr_dot = MagicMock(filename=".", st_mode=0o40755)
    attr_dotdot = MagicMock(filename="..", st_mode=0o40755)
    attr_file = MagicMock(filename="blackbox.zip", st_mode=0o100644)
    mock_sftp.listdir_attr.return_value = [attr_dir2, attr_dot, attr_dir1, attr_dotdot, attr_file]

    manager._sftp = mock_sftp
    folders = manager.list_folders(".")
    assert folders == ["BAL01", "BAL03"]
    assert "blackbox.zip" not in folders
    assert "." not in folders
    assert ".." not in folders


def test_sftp_manager_list_files():
    cfg = Config(
        sftp_host="localhost",
        sftp_port=22,
        sftp_user="user",
        sftp_password="pass",
        local_base_dir=Path("/tmp"),
    )
    manager = SFTPManager(cfg)
    mock_sftp = MagicMock()

    attr_file1 = MagicMock(filename="txitens.txt", st_size=1024, st_mtime=1700000000, st_mode=0o100644)
    attr_file2 = MagicMock(filename="ajustavel.txt", st_size=512, st_mtime=1700000010, st_mode=0o100644)
    attr_sub = MagicMock(filename="subdir", st_mode=0o40755)
    mock_sftp.listdir_attr.return_value = [attr_file1, attr_sub, attr_file2]

    manager._sftp = mock_sftp
    files = manager.list_files("BAL01")
    assert len(files) == 2
    assert files[0] == ("ajustavel.txt", 512, 1700000010)
    assert files[1] == ("txitens.txt", 1024, 1700000000)


def test_sftp_manager_client_property_raises_when_not_connected():
    cfg = Config(
        sftp_host="localhost",
        sftp_port=22,
        sftp_user="user",
        sftp_password="pass",
        local_base_dir=Path("/tmp"),
    )
    manager = SFTPManager(cfg)
    with pytest.raises(RuntimeError, match="Cliente SFTP não está conectado"):
        _ = manager.client


def test_sftp_manager_connect_success():
    cfg = Config(
        sftp_host="localhost",
        sftp_port=2222,
        sftp_user="myuser",
        sftp_password="mypassword",
        local_base_dir=Path("/tmp"),
        timeout=15,
    )
    manager = SFTPManager(cfg)

    with patch("paramiko.SSHClient") as mock_ssh_cls:
        mock_ssh = MagicMock()
        mock_ssh_cls.return_value = mock_ssh
        mock_sftp = MagicMock()
        mock_ssh.open_sftp.return_value = mock_sftp

        client = manager.connect(max_retries=1, retry_delay=0)

        assert client is mock_sftp
        assert manager.client is mock_sftp
        mock_ssh.set_missing_host_key_policy.assert_called_once()
        mock_ssh.connect.assert_called_once_with(
            hostname="localhost",
            port=2222,
            username="myuser",
            password="mypassword",
            timeout=15,
            look_for_keys=False,
            allow_agent=False,
        )
        mock_ssh.get_transport.return_value.set_keepalive.assert_called_once_with(15)
        mock_ssh.open_sftp.assert_called_once()


def test_sftp_manager_connect_retry_and_succeed():
    cfg = Config(
        sftp_host="localhost",
        sftp_port=22,
        sftp_user="user",
        sftp_password="pass",
        local_base_dir=Path("/tmp"),
    )
    manager = SFTPManager(cfg)

    with patch("paramiko.SSHClient") as mock_ssh_cls, patch("time.sleep") as mock_sleep:
        mock_ssh_fail = MagicMock()
        mock_ssh_fail.connect.side_effect = Exception("Connection refused")

        mock_ssh_ok = MagicMock()
        mock_sftp_ok = MagicMock()
        mock_ssh_ok.open_sftp.return_value = mock_sftp_ok

        mock_ssh_cls.side_effect = [mock_ssh_fail, mock_ssh_ok]

        client = manager.connect(max_retries=3, retry_delay=2)

        assert client is mock_sftp_ok
        assert mock_sleep.call_count == 1
        mock_sleep.assert_called_with(2)
        mock_ssh_fail.close.assert_called_once()


def test_sftp_manager_connect_exhaust_retries_raises_connection_error():
    cfg = Config(
        sftp_host="localhost",
        sftp_port=22,
        sftp_user="user",
        sftp_password="pass",
        local_base_dir=Path("/tmp"),
    )
    manager = SFTPManager(cfg)

    with patch("paramiko.SSHClient") as mock_ssh_cls, patch("time.sleep") as mock_sleep:
        mock_ssh_fail = MagicMock()
        mock_ssh_fail.connect.side_effect = Exception("Network unreachable")
        mock_ssh_cls.return_value = mock_ssh_fail

        with pytest.raises(ConnectionError, match="Não foi possível conectar ao servidor SFTP após 2 tentativas"):
            manager.connect(max_retries=2, retry_delay=1)

        assert mock_sleep.call_count == 1
        mock_sleep.assert_called_with(1)
        assert mock_ssh_fail.close.call_count == 2


def test_sftp_manager_close_handles_exceptions():
    cfg = Config(
        sftp_host="localhost",
        sftp_port=22,
        sftp_user="user",
        sftp_password="pass",
        local_base_dir=Path("/tmp"),
    )
    manager = SFTPManager(cfg)
    mock_sftp = MagicMock()
    mock_sftp.close.side_effect = Exception("SFTP close error")
    mock_ssh = MagicMock()
    mock_ssh.close.side_effect = Exception("SSH close error")

    manager._sftp = mock_sftp
    manager._ssh = mock_ssh

    # Should not raise any exception
    manager.close()
    assert manager._sftp is None
    assert manager._ssh is None


def test_sftp_manager_context_manager():
    cfg = Config(
        sftp_host="localhost",
        sftp_port=22,
        sftp_user="user",
        sftp_password="pass",
        local_base_dir=Path("/tmp"),
    )
    manager = SFTPManager(cfg)

    with patch.object(manager, "connect") as mock_connect, patch.object(manager, "close") as mock_close:
        with manager as m:
            assert m is manager
            mock_connect.assert_called_once()
            mock_close.assert_not_called()
        mock_close.assert_called_once()
