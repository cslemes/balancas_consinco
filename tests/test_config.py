import os
import sys
from pathlib import Path

# Ensure project root is in sys.path
sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import pytest
from config import load_config, Config

@pytest.fixture(autouse=True)
def clean_env(monkeypatch):
    """Isolate tests from ambient environment variables."""
    keys_to_clear = [
        k for k in os.environ.keys()
        if k.startswith("SFTP_") or k in (
            "LOCAL_BASE_DIR",
            "REMOTE_BASE_DIR",
            "FOLDER_PATTERN",
            "SYNC_INTERVAL_SECONDS",
            "DRY_RUN",
        )
    ]
    for key in keys_to_clear:
        monkeypatch.delenv(key, raising=False)

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

def test_sftp_host_overrides_sftp_url(tmp_path):
    env_content = (
        "SFTP_URL=sftp://url-host.com:2323\n"
        "SFTP_HOST=explicit-host.com\n"
    )
    env_file = tmp_path / ".env"
    env_file.write_text(env_content, encoding="utf-8")

    cfg = load_config(env_file=str(env_file))
    assert cfg.sftp_host == "explicit-host.com"
    assert cfg.sftp_port == 2323

def test_overrides_sftp_host_with_sftp_url(tmp_path):
    env_content = "SFTP_URL=sftp://url-host.com:2323\n"
    env_file = tmp_path / ".env"
    env_file.write_text(env_content, encoding="utf-8")

    cfg = load_config(env_file=str(env_file), overrides={"SFTP_HOST": "override-host.com"})
    assert cfg.sftp_host == "override-host.com"

def test_empty_string_env_values_fallback_to_defaults(tmp_path):
    env_content = (
        "SFTP_HOST=example.com\n"
        "LOCAL_BASE_DIR=\n"
        "SYNC_INTERVAL_SECONDS=\n"
        "SFTP_TIMEOUT=\n"
        "SFTP_PORT=\n"
        "REMOTE_BASE_DIR=\n"
        "FOLDER_PATTERN=\n"
    )
    env_file = tmp_path / ".env"
    env_file.write_text(env_content, encoding="utf-8")

    cfg = load_config(env_file=str(env_file))
    assert cfg.local_base_dir == Path("C:/balancas")
    assert cfg.sync_interval_seconds == 0
    assert cfg.timeout == 30
    assert cfg.sftp_port == 22
    assert cfg.remote_base_dir == "."
    assert cfg.folder_regex == r"(?i)^bal(\d+)$"

def test_config_password_not_in_repr():
    cfg = Config(
        sftp_host="localhost",
        sftp_port=22,
        sftp_user="user",
        sftp_password="super_secret_password_123",
        local_base_dir=Path("/tmp"),
    )
    repr_str = repr(cfg)
    assert "super_secret_password_123" not in repr_str
    assert "sftp_password" not in repr_str

