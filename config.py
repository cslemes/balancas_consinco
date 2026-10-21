import os
from dataclasses import dataclass
from pathlib import Path
from typing import Optional, Any
from urllib.parse import urlsplit
from dotenv import dotenv_values

@dataclass
class Config:
    sftp_host: str
    sftp_port: int
    sftp_user: str
    sftp_password: str
    local_base_dir: Path
    remote_base_dir: str = "."
    folder_regex: str = r"(?i)^bal(\d+)$"
    sync_interval_seconds: int = 0
    timeout: int = 30
    dry_run: bool = False

def load_config(env_file: Optional[str] = None, overrides: Optional[dict] = None) -> Config:
    vals: dict[str, Any] = {}
    if env_file is not None:
        if os.path.exists(env_file):
            vals.update(dotenv_values(env_file))
    else:
        # Tenta .env no diretório corrente se existir
        if os.path.exists(".env"):
            vals.update(dotenv_values(".env"))

    # Sobrescreve com variáveis de ambiente do SO se existirem
    for k, v in os.environ.items():
        if v:
            vals[k] = v

    # Sobrescreve com overrides de argumentos
    if overrides:
        vals.update({k: v for k, v in overrides.items() if v is not None})

    sftp_url = str(vals.get("SFTP_URL") or "").strip()
    sftp_host = str(vals.get("SFTP_HOST") or "").strip()
    sftp_port = 22

    if sftp_url:
        parsed = urlsplit(sftp_url if "//" in sftp_url else f"//{sftp_url}")
        # Only populate from URL if not already explicitly provided in vals
        if parsed.hostname and not sftp_host:
            sftp_host = parsed.hostname
        if parsed.port and not (vals.get("SFTP_PORT") and str(vals.get("SFTP_PORT")).strip()):
            sftp_port = parsed.port
        if parsed.username and not (vals.get("SFTP_USER") and str(vals.get("SFTP_USER")).strip()):
            vals["SFTP_USER"] = parsed.username
        if parsed.password and not (vals.get("SFTP_PASSWORD") and str(vals.get("SFTP_PASSWORD")).strip()):
            vals["SFTP_PASSWORD"] = parsed.password

    if not sftp_host:
        raise ValueError("Host do SFTP não configurado. Defina SFTP_URL ou SFTP_HOST.")

    raw_port = vals.get("SFTP_PORT")
    if raw_port is not None and str(raw_port).strip():
        sftp_port = int(str(raw_port).strip())

    sftp_user = str(vals.get("SFTP_USER") or "").strip()
    sftp_password = str(vals.get("SFTP_PASSWORD") or "").strip()

    raw_local_dir = vals.get("LOCAL_BASE_DIR")
    if raw_local_dir is None or not str(raw_local_dir).strip():
        raw_local_dir = r"C:\balancas"
    local_base_dir = Path(str(raw_local_dir).strip().replace("\\", "/"))

    raw_remote_dir = vals.get("REMOTE_BASE_DIR")
    remote_base_dir = str(raw_remote_dir).strip() if raw_remote_dir is not None and str(raw_remote_dir).strip() else "."

    raw_folder_pattern = vals.get("FOLDER_PATTERN")
    folder_regex = str(raw_folder_pattern).strip() if raw_folder_pattern is not None and str(raw_folder_pattern).strip() else r"(?i)^bal(\d+)$"

    raw_interval = vals.get("SYNC_INTERVAL_SECONDS")
    sync_interval = int(str(raw_interval).strip()) if raw_interval is not None and str(raw_interval).strip() else 0

    raw_timeout = vals.get("SFTP_TIMEOUT")
    timeout = int(str(raw_timeout).strip()) if raw_timeout is not None and str(raw_timeout).strip() else 30
    
    dry_run_val = vals.get("DRY_RUN", False)
    if isinstance(dry_run_val, str):
        dry_run = dry_run_val.strip().lower() in ("true", "1", "yes")
    else:
        dry_run = bool(dry_run_val)

    return Config(
        sftp_host=sftp_host,
        sftp_port=sftp_port,
        sftp_user=sftp_user,
        sftp_password=sftp_password,
        local_base_dir=local_base_dir,
        remote_base_dir=remote_base_dir,
        folder_regex=folder_regex,
        sync_interval_seconds=sync_interval,
        timeout=timeout,
        dry_run=dry_run,
    )
