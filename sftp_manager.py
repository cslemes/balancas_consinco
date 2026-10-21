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
                logger.info(
                    f"Conectando ao SFTP {self.config.sftp_host}:{self.config.sftp_port} "
                    f"(Tentativa {attempt}/{max_retries})..."
                )
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
                    raise ConnectionError(
                        f"Não foi possível conectar ao servidor SFTP após {max_retries} tentativas: {e}"
                    )

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
