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
            if cfg.remote_base_dir in [".", "/", ""]:
                remote_folder_path = remote_folder
            else:
                remote_folder_path = f"{cfg.remote_base_dir.rstrip('/')}/{remote_folder}"

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
            try:
                time.sleep(interval)
            except KeyboardInterrupt:
                logger.info("Execução interrompida pelo usuário. Finalizando...")
                break


if __name__ == "__main__":
    main()
