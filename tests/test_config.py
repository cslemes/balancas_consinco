import os
import sys
from pathlib import Path

# Ensure project root is in sys.path
sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import pytest
from config import load_config, Config

def test_load_config_from_sftp_url(tmp_path):
    env_content = (
        "SFTP_URL=sftp://example.com:2323\n"
        "SFTP_USER=myuser\n"
        "SFTP_PASSWORD=mypassword\n"
        "LOCAL_BASE_DIR=C:\\balancas\n"
    )
    env_file = tmp_path / ".env"
    env_file.write_text(env_content, encoding="utf-8")

    cfg = load_config(env_file=str(env_file))
    assert cfg.sftp_host == "example.com"
    assert cfg.sftp_port == 2323
    assert cfg.sftp_user == "myuser"
    assert cfg.sftp_password == "mypassword"
    assert cfg.local_base_dir == Path("C:/balancas")

def test_load_config_with_individual_vars(tmp_path):
    env_content = (
        "SFTP_HOST=sftp.server.local\n"
        "SFTP_PORT=22\n"
        "SFTP_USER=user2\n"
        "SFTP_PASSWORD=pass2\n"
        "LOCAL_BASE_DIR=/tmp/balancas\n"
        "SYNC_INTERVAL_SECONDS=300\n"
    )
    env_file = tmp_path / ".env"
    env_file.write_text(env_content, encoding="utf-8")

    cfg = load_config(env_file=str(env_file))
    assert cfg.sftp_host == "sftp.server.local"
    assert cfg.sftp_port == 22
    assert cfg.sync_interval_seconds == 300
    assert cfg.local_base_dir == Path("/tmp/balancas")

def test_load_config_missing_host_raises():
    with pytest.raises(ValueError, match="Host do SFTP não configurado"):
        load_config(env_file="non_existent_file_path_12345.env", overrides={"SFTP_HOST": "", "SFTP_URL": ""})

def test_load_config_with_overrides(tmp_path):
    env_content = (
        "SFTP_URL=sftp://example.com:22\n"
        "SFTP_USER=myuser\n"
        "SFTP_PASSWORD=mypassword\n"
    )
    env_file = tmp_path / ".env"
    env_file.write_text(env_content, encoding="utf-8")

    overrides = {
        "SYNC_INTERVAL_SECONDS": 60,
        "DRY_RUN": True,
        "SFTP_PORT": 2222,
    }
    cfg = load_config(env_file=str(env_file), overrides=overrides)
    assert cfg.sync_interval_seconds == 60
    assert cfg.dry_run is True
    assert cfg.sftp_port == 2222

def test_load_config_url_with_credentials(tmp_path):
    env_content = (
        "SFTP_URL=sftp://embedded_user:embedded_pass@remote.domain.com:2200\n"
    )
    env_file = tmp_path / ".env"
    env_file.write_text(env_content, encoding="utf-8")

    cfg = load_config(env_file=str(env_file))
    assert cfg.sftp_host == "remote.domain.com"
    assert cfg.sftp_port == 2200
    assert cfg.sftp_user == "embedded_user"
    assert cfg.sftp_password == "embedded_pass"

def test_load_config_defaults(tmp_path):
    env_content = (
        "SFTP_HOST=default.host.com\n"
    )
    env_file = tmp_path / ".env"
    env_file.write_text(env_content, encoding="utf-8")

    cfg = load_config(env_file=str(env_file))
    assert cfg.sftp_port == 22
    assert cfg.remote_base_dir == "."
    assert cfg.folder_regex == r"(?i)^bal(\d+)$"
    assert cfg.sync_interval_seconds == 0
    assert cfg.timeout == 30
    assert cfg.dry_run is False
    assert cfg.local_base_dir == Path("C:/balancas")
