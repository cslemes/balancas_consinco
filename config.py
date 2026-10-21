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

    sftp_url = str(vals.get("SFTP_URL", "")).strip()
    sftp_host = str(vals.get("SFTP_HOST", "")).strip()
    sftp_port = 22

    if sftp_url:
        parsed = urlsplit(sftp_url if "//" in sftp_url else f"//{sftp_url}")
        if parsed.hostname:
            sftp_host = parsed.hostname
        if parsed.port:
            sftp_port = parsed.port
        if parsed.username and not vals.get("SFTP_USER"):
            vals["SFTP_USER"] = parsed.username
        if parsed.password and not vals.get("SFTP_PASSWORD"):
            vals["SFTP_PASSWORD"] = parsed.password

    if not sftp_host:
        raise ValueError("Host do SFTP não configurado. Defina SFTP_URL ou SFTP_HOST.")

    if vals.get("SFTP_PORT"):
        sftp_port = int(vals["SFTP_PORT"])

    sftp_user = str(vals.get("SFTP_USER") or "").strip()
    sftp_password = str(vals.get("SFTP_PASSWORD") or "").strip()
    local_base_dir_val = vals.get("LOCAL_BASE_DIR", r"C:\balancas")
    local_base_dir = Path(str(local_base_dir_val).strip().replace("\\", "/"))
    remote_base_dir = str(vals.get("REMOTE_BASE_DIR") or ".").strip() or "."
    folder_regex = str(vals.get("FOLDER_PATTERN") or r"(?i)^bal(\d+)$").strip()
    sync_interval = int(vals.get("SYNC_INTERVAL_SECONDS", 0))
    timeout = int(vals.get("SFTP_TIMEOUT", 30))
    
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
