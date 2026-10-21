import os
import sys
from pathlib import Path
import pytest

# Ensure project root is in sys.path
sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from config import load_config
from sftp_manager import SFTPManager
from folder_mapper import FolderMapper
from sync_balancas import run_sync_cycle

pytestmark = [
    pytest.mark.integration,
    pytest.mark.skipif(not os.path.exists(".env"), reason=".env não encontrado"),
]


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


def test_live_sftp_list_files_in_bal01():
    if not os.path.exists(".env"):
        pytest.skip(".env não encontrado para teste de integração real.")

    cfg = load_config(".env")
    with SFTPManager(cfg) as sftp:
        files = sftp.list_files("BAL01")
        assert len(files) > 0, "Deveria listar arquivos dentro de BAL01"
        filenames = [name for name, size, mtime in files]
        assert any(f.endswith(".txt") for f in filenames), "Deveria conter arquivos .txt em BAL01"


def test_live_sftp_dry_run_sync(tmp_path: Path):
    if not os.path.exists(".env"):
        pytest.skip(".env não encontrado para teste de integração real.")

    cfg = load_config(".env", overrides={"DRY_RUN": True, "LOCAL_BASE_DIR": tmp_path})
    stats = run_sync_cycle(cfg)

    assert stats["folders_checked"] >= 1
    assert stats["downloaded"] > 0
    assert stats["errors"] == 0
    # Como foi dry-run, nenhum arquivo deve ter sido baixado de fato no tmp_path
    downloaded_files = list(tmp_path.rglob("*.*"))
    assert len(downloaded_files) == 0, f"Dry-run não deveria criar arquivos locais: {downloaded_files}"
