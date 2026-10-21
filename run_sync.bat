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
