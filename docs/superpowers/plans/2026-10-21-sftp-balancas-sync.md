# SFTP Balanças Synchronization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a robust, production-ready Python script to synchronize scale files from remote `BAL<xx>` folders on an SFTP server to local Windows folders `C:\balancas\<xx>` with atomic downloads, change detection, and Windows Task Scheduler support.

**Architecture:** Pure Python with `paramiko` and `python-dotenv`. A modular design featuring configuration parsing, SFTP client management with reconnection capabilities, regex folder matching (`BAL<xx>` -> `<xx>`), incremental synchronization (comparing size and modification timestamp), safe temporary-file atomic writes, rotating file logs, and Windows batch runner.

**Tech Stack:** Python 3.10+, `paramiko`, `python-dotenv`, `pytest`.

**Spec:** `docs/superpowers/specs/2026-10-21-sftp-balancas-sync-design.md`

## Global Constraints

- Remote folders matching pattern `(?i)^bal(\d+)$` must map to `<LOCAL_BASE_DIR>\<xx>` (e.g. `BAL01` -> `C:\balancas\01`).
- Remote files on SFTP must NOT be deleted or moved; they must remain intact.
- Downloads must be atomic (written to `.tmp` first, then renamed) to prevent scale software from reading incomplete files.
- Modification time (`mtime`) on local files must be updated to match the remote SFTP file `mtime`.
- Unchanged files (matching `st_size` and `st_mtime`) must be skipped without re-downloading.
- Windows file lock errors (`PermissionError`) must be caught and logged without halting execution for other files.
- Configuration must parse `SFTP_URL` (`sftp://host:port`) and/or `SFTP_HOST`/`SFTP_PORT`.

---

### Task 1: Environment Setup, Dependencies & Configuration Parser

**Files:**
- Create: `requirements.txt`
- Create: `.env.example`
- Create: `config.py`
- Test: `tests/test_config.py`

**Interfaces:**
- Produces: `Config` dataclass with fields `sftp_host: str`, `sftp_port: int`, `sftp_user: str`, `sftp_password: str`, `local_base_dir: Path`, `remote_base_dir: str`, `folder_regex: str`, `sync_interval_seconds: int`, `timeout: int`, `dry_run: bool`.
- Function: `load_config(env_file: Optional[str] = None, overrides: Optional[dict] = None) -> Config`

- [ ] **Step 1: Write requirements.txt and .env.example**

Create `requirements.txt`:
```text
paramiko>=3.4.0
python-dotenv>=1.0.0
pytest>=8.0.0
```

Create `.env.example`:
```ini
SFTP_URL=sftp://supermercados212430.consinco.cloudtotvs.com.br:2323
SFTP_USER=ftp_prod_CK5ETD
SFTP_PASSWORD=sua_senha_aqui

# Caminho local no servidor Windows
LOCAL_BASE_DIR=C:\balancas

# Diretório remoto base no SFTP (geralmente . ou /)
REMOTE_BASE_DIR=.

# Regex para casar as pastas (padrão BALxx pegando o número xx)
FOLDER_PATTERN=(?i)^bal(\d+)$

# Intervalo em segundos para modo loop contínuo (0 = executa uma vez e finaliza)
SYNC_INTERVAL_SECONDS=0

# Timeout de conexão SFTP em segundos
SFTP_TIMEOUT=30
```

- [ ] **Step 2: Write failing tests for Config parser**

Create `tests/test_config.py`:
```python
import os
import pytest
from pathlib import Path
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `pytest tests/test_config.py -v`  
Expected: FAIL with ModuleNotFoundError: No module named 'config'

- [ ] **Step 4: Implement config.py**

Create `config.py`:
```python
import os
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Optional
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
    vals = {}
    if env_file and os.path.exists(env_file):
        vals.update(dotenv_values(env_file))
    else:
        # Tenta .env no diretório corrente
        vals.update(dotenv_values(".env"))

    # Sobrescreve com variáveis de ambiente do SO se existirem
    for k, v in os.environ.items():
        if v:
            vals[k] = v

    # Sobrescreve com overrides de argumentos
    if overrides:
        vals.update({k: v for k, v in overrides.items() if v is not None})

    sftp_url = vals.get("SFTP_URL", "").strip()
    sftp_host = vals.get("SFTP_HOST", "").strip()
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

    sftp_user = (vals.get("SFTP_USER") or "").strip()
    sftp_password = (vals.get("SFTP_PASSWORD") or "").strip()
    local_base_dir = Path(vals.get("LOCAL_BASE_DIR", r"C:\balancas").strip())
    remote_base_dir = vals.get("REMOTE_BASE_DIR", ".").strip() or "."
    folder_regex = vals.get("FOLDER_PATTERN", r"(?i)^bal(\d+)$").strip()
    sync_interval = int(vals.get("SYNC_INTERVAL_SECONDS", 0))
    timeout = int(vals.get("SFTP_TIMEOUT", 30))
    dry_run = bool(vals.get("DRY_RUN", False))

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
```

- [ ] **Step 5: Run tests and verify they pass**

Run: `pytest tests/test_config.py -v`  
Expected: PASS

- [ ] **Step 6: Commit Task 1**

```bash
git add requirements.txt .env.example config.py tests/test_config.py
git commit -m "feat(config): add configuration loader and validation"
```

---

### Task 2: Folder Regex & Mapping Logic

**Files:**
- Create: `folder_mapper.py`
- Test: `tests/test_folder_mapper.py`

**Interfaces:**
- Produces: `FolderMapper` class with methods:
  - `match_folder(folder_name: str) -> Optional[str]`: returns the extracted target subdirectory name (e.g., `'01'`) or `None` if not a match.
  - `get_local_folder(local_base_dir: Path, remote_folder_name: str) -> Optional[Path]`

- [ ] **Step 1: Write failing tests for FolderMapper**

Create `tests/test_folder_mapper.py`:
```python
from pathlib import Path
from folder_mapper import FolderMapper

def test_folder_mapper_matches():
    mapper = FolderMapper(pattern=r"(?i)^bal(\d+)$")
    assert mapper.match_folder("BAL01") == "01"
    assert mapper.match_folder("bal03") == "03"
    assert mapper.match_folder("BAL10") == "10"
    assert mapper.match_folder("BAL92") == "92"

def test_folder_mapper_ignores_non_matching():
    mapper = FolderMapper(pattern=r"(?i)^bal(\d+)$")
    assert mapper.match_folder("BALANCAS") is None
    assert mapper.match_folder("blackbox.zip") is None
    assert mapper.match_folder("IMPORT") is None
    assert mapper.match_folder(".snapshot") is None

def test_folder_mapper_get_local_folder():
    mapper = FolderMapper(pattern=r"(?i)^bal(\d+)$")
    base = Path("C:/balancas")
    assert mapper.get_local_folder(base, "BAL01") == Path("C:/balancas/01")
    assert mapper.get_local_folder(base, "BAL93") == Path("C:/balancas/93")
    assert mapper.get_local_folder(base, "BALANCAS") is None
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/test_folder_mapper.py -v`  
Expected: FAIL with ModuleNotFoundError: No module named 'folder_mapper'

- [ ] **Step 3: Implement folder_mapper.py**

Create `folder_mapper.py`:
```python
import re
from pathlib import Path
from typing import Optional

class FolderMapper:
    def __init__(self, pattern: str = r"(?i)^bal(\d+)$"):
        self.pattern = re.compile(pattern)

    def match_folder(self, folder_name: str) -> Optional[str]:
        match = self.pattern.match(folder_name)
        if match:
            # Se houver grupos de captura, retorna o primeiro grupo (ex: 01 de BAL01)
            if match.groups():
                return match.group(1)
            return folder_name
        return None

    def get_local_folder(self, local_base_dir: Path, remote_folder_name: str) -> Optional[Path]:
        sub_name = self.match_folder(remote_folder_name)
        if sub_name is not None:
            return local_base_dir / sub_name
        return None
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `pytest tests/test_folder_mapper.py -v`  
Expected: PASS

- [ ] **Step 5: Commit Task 2**

```bash
git add folder_mapper.py tests/test_folder_mapper.py
git commit -m "feat(mapper): add folder regex matching and mapping logic"
```

---

### Task 3: Synchronization Core & Atomic File Download

**Files:**
- Create: `sync_core.py`
- Test: `tests/test_sync_core.py`

**Interfaces:**
- Produces: `FileSyncer` class:
  - `should_download(local_file: Path, remote_size: int, remote_mtime: int) -> bool`
  - `sync_file(sftp_client, remote_path: str, local_path: Path, remote_size: int, remote_mtime: int, dry_run: bool = False) -> str`: returns `'downloaded'`, `'skipped'`, or `'error'`

- [ ] **Step 1: Write failing tests for FileSyncer**

Create `tests/test_sync_core.py`:
```python
import os
from pathlib import Path
from unittest.mock import MagicMock
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/test_sync_core.py -v`  
Expected: FAIL with ModuleNotFoundError: No module named 'sync_core'

- [ ] **Step 3: Implement sync_core.py**

Create `sync_core.py`:
```python
import os
import shutil
import logging
from pathlib import Path
from typing import Any

logger = logging.getLogger("sync_balancas")

class FileSyncer:
    def __init__(self, mtime_tolerance_seconds: int = 2):
        self.tolerance = mtime_tolerance_seconds

    def should_download(self, local_file: Path, remote_size: int, remote_mtime: int) -> bool:
        if not local_file.exists():
            return True
        try:
            stat = local_file.stat()
            # Verifica tamanho em bytes
            if stat.st_size != remote_size:
                return True
            # Verifica data de modificação dentro da tolerância
            if abs(stat.st_mtime - remote_mtime) > self.tolerance:
                return True
            return False
        except OSError as e:
            logger.warning(f"Erro ao verificar atributos de {local_file}: {e}")
            return True

    def sync_file(
        self,
        sftp_client: Any,
        remote_path: str,
        local_path: Path,
        remote_size: int,
        remote_mtime: int,
        dry_run: bool = False
    ) -> str:
        if not self.should_download(local_path, remote_size, remote_mtime):
            logger.debug(f"[IGNORADO] {local_path.name} já atualizado.")
            return "skipped"

        if dry_run:
            logger.info(f"[DRY-RUN] Baixaria {remote_path} -> {local_path} ({remote_size} bytes)")
            return "downloaded"

        tmp_path = local_path.with_suffix(local_path.suffix + ".tmp")
        try:
            local_path.parent.mkdir(parents=True, exist_ok=True)
            logger.info(f"[BAIXANDO] {remote_path} -> {local_path.name} ({remote_size} bytes)")
            
            # Download para arquivo temporário
            sftp_client.get(remote_path, str(tmp_path))

            # Renomeia atomicamente substituindo o arquivo final
            # No Windows, os.replace é atômico se o arquivo não estiver com lock exclusivo
            tmp_path.replace(local_path)

            # Sincroniza a data de modificação local com a data remota
            os.utime(local_path, (remote_mtime, remote_mtime))
            return "downloaded"

        except PermissionError as e:
            logger.error(f"[BLOQUEADO] Arquivo {local_path} está bloqueado por outro processo (ex: Toledo MGV): {e}")
            if tmp_path.exists():
                try:
                    tmp_path.unlink()
                except OSError:
                    pass
            return "error"
        except Exception as e:
            logger.error(f"[ERRO] Falha ao sincronizar {remote_path}: {e}")
            if tmp_path.exists():
                try:
                    tmp_path.unlink()
                except OSError:
                    pass
            return "error"
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `pytest tests/test_sync_core.py -v`  
Expected: PASS

- [ ] **Step 5: Commit Task 3**

```bash
git add sync_core.py tests/test_sync_core.py
git commit -m "feat(sync): implement atomic download and change detection"
```

---

### Task 4: SFTP Manager & Directory Scanner

**Files:**
- Create: `sftp_manager.py`
- Test: `tests/test_sftp_manager.py`

**Interfaces:**
- Produces: `SFTPManager` class:
  - `connect() -> paramiko.SFTPClient`
  - `close()`
  - `list_folders(remote_dir: str = ".") -> list[str]`
  - `list_files(remote_folder: str) -> list[tuple[str, int, int]]` (name, size, mtime)
  - Context manager `with SFTPManager(config) as sftp:`

- [ ] **Step 1: Write failing tests for SFTPManager with mocked paramiko**

Create `tests/test_sftp_manager.py`:
```python
from unittest.mock import MagicMock, patch
from pathlib import Path
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
    attr_file = MagicMock(filename="blackbox.zip", st_mode=0o100644)
    mock_sftp.listdir_attr.return_value = [attr_dir1, attr_dir2, attr_file]
    
    manager._sftp = mock_sftp
    folders = manager.list_folders(".")
    assert "BAL01" in folders
    assert "BAL03" in folders
    assert "blackbox.zip" not in folders

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
    attr_sub = MagicMock(filename="subdir", st_mode=0o40755)
    mock_sftp.listdir_attr.return_value = [attr_file1, attr_sub]
    
    manager._sftp = mock_sftp
    files = manager.list_files("BAL01")
    assert len(files) == 1
    assert files[0] == ("txitens.txt", 1024, 1700000000)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/test_sftp_manager.py -v`  
Expected: FAIL with ModuleNotFoundError: No module named 'sftp_manager'

- [ ] **Step 3: Implement sftp_manager.py**

Create `sftp_manager.py`:
```python
import stat
import time
import logging
from typing import Optional, List, Tuple
import paramiko
from config import Config

logger = logging.getLogger("sync_balancas")

class SFTPManager:
    def __init__(self, config: Config):
        self.config = config
        self._ssh: Optional[paramiko.SSHClient] = None
        self._sftp: Optional[paramiko.SFTPClient] = None

    def connect(self, max_retries: int = 3, retry_delay: int = 5) -> paramiko.SFTPClient:
        for attempt in range(1, max_retries + 1):
            try:
                logger.info(f"Conectando ao SFTP {self.config.sftp_host}:{self.config.sftp_port} (Tentativa {attempt}/{max_retries})...")
                ssh = paramiko.SSHClient()
                ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
                ssh.connect(
                    hostname=self.config.sftp_host,
                    port=self.config.sftp_port,
                    username=self.config.sftp_user,
                    password=self.config.sftp_password,
                    timeout=self.config.timeout,
                    look_for_keys=False,
                    allow_agent=False,
                )
                self._ssh = ssh
                self._sftp = ssh.open_sftp()
                logger.info("Conexão SFTP estabelecida com sucesso.")
                return self._sftp
            except Exception as e:
                logger.warning(f"Falha na tentativa {attempt} de conexão: {e}")
                self.close()
                if attempt < max_retries:
                    time.sleep(retry_delay)
                else:
                    raise ConnectionError(f"Não foi possível conectar ao servidor SFTP após {max_retries} tentativas: {e}")

    def close(self):
        if self._sftp:
            try:
                self._sftp.close()
            except Exception:
                pass
            self._sftp = None
        if self._ssh:
            try:
                self._ssh.close()
            except Exception:
                pass
            self._ssh = None

    def __enter__(self):
        self.connect()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()

    @property
    def client(self) -> paramiko.SFTPClient:
        if self._sftp is None:
            raise RuntimeError("Cliente SFTP não está conectado. Chame connect() primeiro.")
        return self._sftp

    def list_folders(self, remote_dir: str = ".") -> List[str]:
        folders = []
        for entry in self.client.listdir_attr(remote_dir):
            if stat.S_ISDIR(entry.st_mode):
                if entry.filename not in [".", ".."]:
                    folders.append(entry.filename)
        return sorted(folders)

    def list_files(self, remote_folder: str) -> List[Tuple[str, int, int]]:
        files = []
        for entry in self.client.listdir_attr(remote_folder):
            if not stat.S_ISDIR(entry.st_mode):
                files.append((entry.filename, entry.st_size, entry.st_mtime))
        return sorted(files)
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `pytest tests/test_sftp_manager.py -v`  
Expected: PASS

- [ ] **Step 5: Commit Task 4**

```bash
git add sftp_manager.py tests/test_sftp_manager.py
git commit -m "feat(sftp): implement sftp manager with retries and directory listing"
```

---

### Task 5: Orchestrator, CLI, Windows Batch Script & Documentation

**Files:**
- Create: `sync_balancas.py`
- Create: `run_sync.bat`
- Create: `README.md`
- Test: `tests/test_orchestrator.py`

**Interfaces:**
- CLI arguments: `--loop`, `--interval`, `--dry-run`, `--config`, `--log-level`
- Orchestrator function: `run_sync_cycle(config: Config) -> dict[str, int]` (returns stats: downloaded, skipped, errors)

- [ ] **Step 1: Write tests for orchestrator run_sync_cycle**

Create `tests/test_orchestrator.py`:
```python
from pathlib import Path
from unittest.mock import MagicMock, patch
from config import Config
from sync_balancas import run_sync_cycle

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
            [("txitens.txt", 100, 1700000000)], # BAL01
            [("itensmgv.txt", 200, 1700000000)], # BAL03
        ]

        with patch("sync_balancas.FileSyncer.sync_file") as mock_sync_file:
            mock_sync_file.return_value = "downloaded"
            stats = run_sync_cycle(cfg)

            assert stats["folders_checked"] == 2
            assert stats["downloaded"] == 2
            assert stats["skipped"] == 0
            assert stats["errors"] == 0
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/test_orchestrator.py -v`  
Expected: FAIL with ModuleNotFoundError: No module named 'sync_balancas'

- [ ] **Step 3: Implement sync_balancas.py**

Create `sync_balancas.py`:
```python
#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Sincronizador SFTP de Arquivos de Balanças
Totvs Consinco Cloud SFTP -> Servidor Windows C:\\balancas\\xx
"""

import os
import sys
import time
import argparse
import logging
from logging.handlers import RotatingFileHandler
from pathlib import Path
from typing import Dict

from config import Config, load_config
from folder_mapper import FolderMapper
from sftp_manager import SFTPManager
from sync_core import FileSyncer

def setup_logger(log_level_name: str = "INFO", log_dir: str = "logs") -> logging.Logger:
    logger = logging.getLogger("sync_balancas")
    level = getattr(logging, log_level_name.upper(), logging.INFO)
    logger.setLevel(level)

    if not logger.handlers:
        formatter = logging.Formatter(
            fmt="%(asctime)s [%(levelname)s] %(message)s",
            datefmt="%Y-%m-%d %H:%M:%S"
        )

        # Handler Console
        console_handler = logging.StreamHandler(sys.stdout)
        console_handler.setFormatter(formatter)
        logger.addHandler(console_handler)

        # Handler Arquivo Rotativo (5MB por arquivo, até 5 backups)
        try:
            Path(log_dir).mkdir(parents=True, exist_ok=True)
            log_path = Path(log_dir) / "sync_balancas.log"
            file_handler = RotatingFileHandler(
                log_path, maxBytes=5 * 1024 * 1024, backupCount=5, encoding="utf-8"
            )
            file_handler.setFormatter(formatter)
            logger.addHandler(file_handler)
        except Exception as e:
            print(f"Aviso: Não foi possível configurar log em arquivo: {e}")

    return logger

def run_sync_cycle(cfg: Config) -> Dict[str, int]:
    logger = logging.getLogger("sync_balancas")
    stats = {"folders_checked": 0, "downloaded": 0, "skipped": 0, "errors": 0}
    mapper = FolderMapper(pattern=cfg.folder_regex)
    syncer = FileSyncer()

    logger.info("=" * 60)
    logger.info(f"Iniciando ciclo de sincronização para {cfg.local_base_dir}...")
    start_time = time.time()

    with SFTPManager(cfg) as sftp_mgr:
        remote_folders = sftp_mgr.list_folders(cfg.remote_base_dir)
        matching_folders = [f for f in remote_folders if mapper.match_folder(f) is not None]

        logger.info(f"Encontradas {len(matching_folders)} pastas de balanças: {', '.join(matching_folders)}")

        for remote_folder in matching_folders:
            local_folder = mapper.get_local_folder(cfg.local_base_dir, remote_folder)
            if not local_folder:
                continue

            stats["folders_checked"] += 1
            remote_folder_path = (
                f"{cfg.remote_base_dir}/{remote_folder}" if cfg.remote_base_dir not in [".", "/"] else remote_folder
            )

            try:
                files = sftp_mgr.list_files(remote_folder_path)
            except Exception as e:
                logger.error(f"Erro ao listar arquivos da pasta remota {remote_folder}: {e}")
                stats["errors"] += 1
                continue

            for filename, size, mtime in files:
                remote_file_path = f"{remote_folder_path}/{filename}"
                local_file_path = local_folder / filename

                status = syncer.sync_file(
                    sftp_client=sftp_mgr.client,
                    remote_path=remote_file_path,
                    local_path=local_file_path,
                    remote_size=size,
                    remote_mtime=mtime,
                    dry_run=cfg.dry_run,
                )

                if status == "downloaded":
                    stats["downloaded"] += 1
                elif status == "skipped":
                    stats["skipped"] += 1
                elif status == "error":
                    stats["errors"] += 1

    elapsed = time.time() - start_time
    logger.info(
        f"Ciclo concluído em {elapsed:.2f}s | Pastas: {stats['folders_checked']} | "
        f"Baixados: {stats['downloaded']} | Já atualizados: {stats['skipped']} | Erros: {stats['errors']}"
    )
    logger.info("=" * 60)
    return stats

def main():
    parser = argparse.ArgumentParser(description="Sincronizador SFTP de Balanças para Servidor Windows")
    parser.add_argument("--config", "-c", default=".env", help="Caminho para arquivo .env")
    parser.add_argument("--loop", action="store_true", help="Executa em loop contínuo (modo daemon)")
    parser.add_argument("--interval", type=int, default=None, help="Intervalo em segundos entre ciclos")
    parser.add_argument("--dry-run", action="store_true", help="Apenas simula os downloads sem alterar arquivos")
    parser.add_argument("--log-level", default="INFO", choices=["DEBUG", "INFO", "WARNING", "ERROR"], help="Nível de log")
    args = parser.parse_args()

    setup_logger(args.log_level)
    logger = logging.getLogger("sync_balancas")

    overrides = {}
    if args.interval is not None:
        overrides["SYNC_INTERVAL_SECONDS"] = args.interval
    if args.dry_run:
        overrides["DRY_RUN"] = True

    try:
        cfg = load_config(env_file=args.config, overrides=overrides)
    except Exception as e:
        logger.critical(f"Erro de configuração: {e}")
        sys.exit(1)

    is_loop = args.loop or (cfg.sync_interval_seconds > 0)

    if not is_loop:
        # Execução única (Agendador de Tarefas do Windows)
        try:
            stats = run_sync_cycle(cfg)
            sys.exit(0 if stats["errors"] == 0 else 2)
        except Exception as e:
            logger.critical(f"Falha crítica na execução: {e}", exc_info=True)
            sys.exit(1)
    else:
        interval = cfg.sync_interval_seconds if cfg.sync_interval_seconds > 0 else 300
        logger.info(f"Iniciando em modo loop contínuo (intervalo: {interval} segundos). Pressione Ctrl+C para parar.")
        while True:
            try:
                run_sync_cycle(cfg)
            except KeyboardInterrupt:
                logger.info("Execução interrompida pelo usuário. Finalizando...")
                break
            except Exception as e:
                logger.error(f"Erro durante o ciclo: {e}", exc_info=True)

            logger.info(f"Aguardando {interval} segundos para o próximo ciclo...")
            time.sleep(interval)

if __name__ == "__main__":
    main()
```

- [ ] **Step 4: Create run_sync.bat for Windows**

Create `run_sync.bat`:
```bat
@echo off
chcp 65001 > nul
setlocal

:: Navega para o diretório do script
cd /d "%~dp0"

echo ========================================================
echo Sincronizador de Balancas SFTP - Servidor Windows
echo ========================================================

:: Verifica se o ambiente virtual existe, se existir utiliza
if exist "venv\Scripts\python.exe" (
    set "PYTHON_EXE=venv\Scripts\python.exe"
) else (
    set "PYTHON_EXE=python"
)

:: Executa o script de sincronizacao
"%PYTHON_EXE%" sync_balancas.py %*

set EXIT_CODE=%ERRORLEVEL%
if %EXIT_CODE% NEQ 0 (
    echo [ERRO] A sincronizacao terminou com codigo de erro %EXIT_CODE%.
)

exit /b %EXIT_CODE%
```

- [ ] **Step 5: Create README.md with complete instructions**

Create `README.md` in Portuguese explaining how to setup Python, create venv, install requirements, test, and configure Windows Task Scheduler.

- [ ] **Step 6: Run tests and verify they pass**

Run: `pytest tests/ -v`  
Expected: PASS

- [ ] **Step 7: Commit Task 5**

```bash
git add sync_balancas.py run_sync.bat README.md tests/test_orchestrator.py
git commit -m "feat(orchestration): add main orchestrator, CLI, Windows batch script and docs"
```

---

### Task 6: Live SFTP Verification Test

**Files:**
- Create: `tests/test_live_sftp.py` (marked with `@pytest.mark.skipif` or integration check)

- [ ] **Step 1: Write live SFTP test**

Create `tests/test_live_sftp.py`:
```python
import os
import pytest
from config import load_config
from sftp_manager import SFTPManager
from folder_mapper import FolderMapper

def test_live_sftp_connection_and_folders():
    if not os.path.exists(".env"):
        pytest.skip(".env não encontrado para teste de integração real.")
    
    cfg = load_config(".env")
    mapper = FolderMapper(cfg.folder_regex)
    
    with SFTPManager(cfg) as sftp:
        folders = sftp.list_folders(cfg.remote_base_dir)
        bal_folders = [f for f in folders if mapper.match_folder(f)]
        assert len(bal_folders) > 0, "Deveria encontrar pastas de balanças como BAL01"
        assert "BAL01" in bal_folders
```

- [ ] **Step 2: Run test against real server**

Run: `pytest tests/test_live_sftp.py -v`  
Expected: PASS

- [ ] **Step 3: Commit Task 6**

```bash
git add tests/test_live_sftp.py
git commit -m "test(integration): add live sftp connection and folder discovery test"
```
