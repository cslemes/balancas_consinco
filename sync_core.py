import os
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

        tmp_path = local_path.parent / f"{local_path.name}.{os.getpid()}.tmp"
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
