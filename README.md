# Sincronizador SFTP de Arquivos de Balanças

Sincronizador automático, incremental e seguro de arquivos de carga de balanças (ex: Toledo MGV, Filizola, Urano) a partir do servidor SFTP (Totvs Consinco Cloud) para diretórios locais em servidor Windows (`C:\balancas\xx`).

---

## Índice

1. [Visão Geral](#visão-geral)
2. [Estrutura de Diretórios](#estrutura-de-diretórios)
3. [Requisitos do Sistema](#requisitos-do-sistema)
4. [Instalação e Configuração](#instalação-e-configuração)
   - [1. Clonar ou Baixar o Projeto](#1-clonar-ou-baixar-o-projeto)
   - [2. Criar o Ambiente Virtual (venv)](#2-criar-o-ambiente-virtual-venv)
   - [3. Instalar Dependências](#3-instalar-dependências)
   - [4. Configurar Variáveis de Ambiente (.env)](#4-configurar-variáveis-de-ambiente-env)
5. [Modos de Execução](#modos-de-execução)
   - [Execução Única (One-Off)](#execução-única-one-off)
   - [Execução em Lote pelo Windows (run_sync.bat)](#execução-em-lote-pelo-windows-run_syncbat)
   - [Modo Daemon Contínuo (Loop)](#modo-daemon-contínuo-loop)
   - [Modo Simulação (Dry-Run)](#modo-simulação-dry-run)
   - [Opções da Linha de Comando (CLI)](#opções-da-linha-de-comando-cli)
6. [Agendador de Tarefas do Windows (Task Scheduler)](#agendador-de-tarefas-do-windows-task-scheduler)
7. [Logs e Observabilidade](#logs-e-observabilidade)
8. [Códigos de Saída (Exit Codes)](#códigos-de-saída-exit-codes)
9. [Execução de Testes Automatizados](#execução-de-testes-automatizados)
10. [Resolução de Problemas (Troubleshooting)](#resolução-de-problemas-troubleshooting)

---

## Visão Geral

Em redes de varejo integradas com ERP Totvs Consinco Cloud, os arquivos de balança (como `txitens.txt`, `itensmgv.txt`, `precopro.txt`, `infnutri.txt`) são exportados para um servidor SFTP em pastas segregadas por loja ou balança (ex: `BAL01`, `BAL03`, `BAL10`, ..., `BAL92`).

Este aplicativo realiza:
- **Mapeamento dinâmico via Regex**: Detecta pastas no SFTP que correspondam a `BAL<xx>` e extrai o identificador `<xx>`, mapeando para `C:\balancas\<xx>`.
- **Sincronização Incremental Inteligente**: Compara tamanho (`st_size`) e data de modificação (`st_mtime`) com tolerância de 2 segundos. Se o arquivo já estiver atualizado, o download é ignorado, economizando banda e processamento.
- **Download Atômico**: Salva inicialmente em arquivo temporário (`.tmp`) e renomeia apenas após a conclusão da transferência, preservando o `mtime` original. Isso impede que softwares como Toledo MGV tentem ler arquivos corrompidos ou incompletos.
- **Tolerância a Bloqueio de Arquivos**: Se o arquivo local estiver bloqueado exclusivamente pelo Toledo MGV ou outro processo, o script captura o erro de permissão, loga o aviso e continua a sincronização dos demais arquivos.

---

## Estrutura de Diretórios

```text
balancas/
├── .env                  # Arquivo local com credenciais e caminhos (ignorado no Git)
├── .env.example          # Modelo de configuração documentado
├── config.py             # Leitura e validação de configurações
├── folder_mapper.py      # Mapeamento regex BAL<xx> -> <xx>
├── sftp_manager.py       # Gerenciamento de conexões SFTP com retentativas
├── sync_core.py          # Lógica incremental e download atômico
├── sync_balancas.py      # Ponto de entrada CLI e orquestrador principal
├── run_sync.bat          # Script de execução para Windows / Agendador
├── requirements.txt      # Dependências Python (paramiko, python-dotenv, pytest)
├── tests/                # Testes unitários automatizados
│   ├── test_config.py
│   ├── test_folder_mapper.py
│   ├── test_sftp_manager.py
│   ├── test_sync_core.py
│   └── test_orchestrator.py
└── logs/                 # Diretório gerado em tempo de execução
    └── sync_balancas.log
```

---

## Requisitos do Sistema

- **Sistema Operacional**: Windows 10, Windows 11 ou Windows Server (2016, 2019, 2022). Também compatível com Linux e macOS.
- **Python**: Versão 3.10 ou superior.
- **Acesso de Rede**: Saída TCP para o host SFTP na porta indicada (padrão porta 22).
- **Permissões**: Permissão de leitura/escrita no diretório base local (`C:\balancas` por padrão).

---

## Instalação e Configuração

### 1. Clonar ou Baixar o Projeto

Abra o prompt de comando (CMD ou PowerShell) no servidor Windows e clone ou extraia o projeto:

```cmd
cd C:\projetos
git clone <URL_DO_REPOSITORIO> balancas
cd balancas
```

### 2. Criar o Ambiente Virtual (venv)

Recomenda-se utilizar um ambiente virtual isolado:

```cmd
python -m venv venv
```

### 3. Instalar Dependências

Ative o ambiente virtual e instale as bibliotecas necessárias:

```cmd
venv\Scripts\activate
pip install --upgrade pip
pip install -r requirements.txt
```

### 4. Configurar Variáveis de Ambiente (.env)

Copie o modelo de configuração `.env.example` para `.env`:

```cmd
copy .env.example .env
```

Abra o arquivo `.env` em um editor de texto (ex: `notepad .env`) e configure os valores:

```ini
# Configurações do Servidor SFTP (Totvs Cloud)
SFTP_HOST=sftp.provedor.com.br
SFTP_PORT=22
SFTP_USER=usuario_sftp
SFTP_PASSWORD=senha_sftp

# Alternativa: Configurar via SFTP_URL unificada
# SFTP_URL=sftp://usuario_sftp:senha_sftp@sftp.provedor.com.br:22

# Diretórios
LOCAL_BASE_DIR=C:\balancas
REMOTE_BASE_DIR=.

# Expressão regular para identificar as pastas remotas
# Exemplo: BAL01 -> 01, BAL03 -> 03, BAL92 -> 92
FOLDER_PATTERN=(?i)^bal(\d+)$

# Intervalo em segundos para execução contínua em loop
# Deixe 0 se for utilizar o Agendador de Tarefas do Windows
SYNC_INTERVAL_SECONDS=0

# Timeout de rede em segundos
SFTP_TIMEOUT=30

# Simulação sem escrita em disco (true para testar, false para produção)
DRY_RUN=false
```

---

## Modos de Execução

### Execução Única (One-Off)

Executa um único ciclo de sincronização, ideal para rotinas agendadas:

```cmd
venv\Scripts\python.exe sync_balancas.py
```

### Execução em Lote pelo Windows (run_sync.bat)

O script `run_sync.bat` detecta automaticamente o ambiente virtual `venv`, ativa a codificação UTF-8 (`chcp 65001`) e repassa todos os argumentos recebidos:

```cmd
run_sync.bat
```

Para passar argumentos adicionais:

```cmd
run_sync.bat --dry-run --log-level DEBUG
```

### Modo Daemon Contínuo (Loop)

Para manter o sincronizador rodando em segundo plano continuamente a cada intervalo (ex: a cada 300 segundos / 5 minutos):

```cmd
run_sync.bat --loop --interval 300
```

Para interromper, pressione `Ctrl + C`.

### Modo Simulação (Dry-Run)

Simula todo o processo de listagem e comparação sem realizar downloads reais nem alterar arquivos em disco:

```cmd
run_sync.bat --dry-run
```

### Opções da Linha de Comando (CLI)

| Opção | Descrição | Padrão |
| :--- | :--- | :--- |
| `-c`, `--config` | Caminho para o arquivo `.env` customizado | `.env` |
| `--loop` | Executa em modo contínuo (daemon) | `False` |
| `--interval` | Intervalo em segundos entre ciclos (usado com `--loop`) | `SYNC_INTERVAL_SECONDS` ou `300` |
| `--dry-run` | Apenas simula a sincronização sem alterar arquivos locais | `False` |
| `--log-level` | Nível de detalhamento do log (`DEBUG`, `INFO`, `WARNING`, `ERROR`) | `INFO` |

---

## Agendador de Tarefas do Windows (Task Scheduler)

Para automatizar a execução periódica (ex: a cada 5 ou 10 minutos) no servidor Windows:

1. Abra o menu Iniciar e digite **Agendador de Tarefas** (`taskschd.msc`).
2. No painel direito, clique em **Criar Tarefa...** (Create Task).
3. Na aba **Geral**:
   - **Nome**: `Sincronizador SFTP Balancas`
   - Marque a opção: **Executar estando o usuário conectado ou não** (Run whether user is logged on or not).
   - Marque a opção: **Executar com privilégios mais altos** (Run with highest privileges).
   - Configurar para: Selecione a versão do seu Windows Server ou Windows 10/11.
4. Na aba **Disparadores** (Triggers):
   - Clique em **Novo...** (New).
   - Iniciar a tarefa: **Em um agendamento** (On a schedule).
   - Marque: **Diariamente** (Daily).
   - Em Configurações avançadas:
     - Marque: **Repetir a tarefa a cada**: `5 minutos` (ou `10 minutos`).
     - Pelo período de: `Indefinidamente` (Indefinitely).
     - Marque: **Habilitado** (Enabled).
   - Clique em **OK**.
5. Na aba **Ações** (Actions):
   - Clique em **Novo...** (New).
   - Ação: **Iniciar um programa** (Start a program).
   - **Programa/script**: `C:\projetos\balancas\run_sync.bat` (ajuste para o caminho absoluto correto).
   - **Iniciar em (obrigatório)**: `C:\projetos\balancas\` (diretório base onde se encontram o `.bat` e o `.env`).
   - Clique em **OK**.
6. Na aba **Configurações** (Settings):
   - Marque: **Permitir que a tarefa seja executada sob demanda** (Allow task to be run on demand).
   - Marque: **Se a tarefa falhar, reiniciar a cada**: `1 minuto`, tentar até `3 vezes`.
   - Se a tarefa já estiver em execução, selecione: **Não iniciar uma nova instância** (Do not start a new instance).
7. Clique em **OK** e informe a senha do usuário do Windows para salvar a tarefa.

---

## Logs e Observabilidade

- **Saída no Console**: Durante a execução direta, os eventos são exibidos com data e hora.
- **Arquivo de Log**: Localizado em `logs/sync_balancas.log`.
- **Rotação de Arquivos**: Configurado com `RotatingFileHandler` com tamanho máximo de **5 MB** por arquivo e até **5 arquivos de backup** (`sync_balancas.log.1`, `.2`, etc.), totalizando no máximo 25 MB de armazenamento para histórico.
- **Exemplo de Log**:
  ```text
  2026-10-21 20:00:00 [INFO] ============================================================
  2026-10-21 20:00:00 [INFO] Iniciando ciclo de sincronização para C:\balancas...
  2026-10-21 20:00:01 [INFO] Conectando ao SFTP sftp.totvs.com.br:22 (Tentativa 1/3)...
  2026-10-21 20:00:02 [INFO] Conexão SFTP estabelecida com sucesso.
  2026-10-21 20:00:02 [INFO] Encontradas 2 pastas de balanças: BAL01, BAL03
  2026-10-21 20:00:03 [INFO] [BAIXANDO] BAL01/txitens.txt -> txitens.txt (142050 bytes)
  2026-10-21 20:00:04 [DEBUG] [IGNORADO] BAL03/itensmgv.txt já atualizado.
  2026-10-21 20:00:04 [INFO] Ciclo concluído em 4.12s | Pastas: 2 | Baixados: 1 | Já atualizados: 1 | Erros: 0
  2026-10-21 20:00:04 [INFO] ============================================================
  ```

---

## Códigos de Saída (Exit Codes)

O script e o arquivo `run_sync.bat` retornam códigos numéricos de saída que podem ser interpretados pelo Agendador de Tarefas ou ferramentas de monitoramento:

| Código | Significado | Descrição |
| :---: | :--- | :--- |
| `0` | **Sucesso** | O ciclo concluiu e todos os arquivos foram baixados ou verificados sem erros. |
| `1` | **Erro Crítico** | Falha de configuração (ex: host SFTP ausente) ou exceção fatal que impediu a execução. |
| `2` | **Aviso / Erro Parcial** | O ciclo rodou, mas um ou mais arquivos apresentaram erro durante a transferência. |

---

## Execução de Testes Automatizados

O projeto conta com uma suíte abrangente de testes unitários com pytest:

```cmd
venv\Scripts\activate
pytest tests/ -v
```

Para verificar a cobertura:

```cmd
pytest tests/ -v --tb=short
```

---

## Resolução de Problemas (Troubleshooting)

### 1. `PermissionError` ao renomear arquivo (`[BLOQUEADO]`)
- **Causa**: O software de balança (como o Toledo MGV) está lendo ou processando o arquivo local naquele exato momento.
- **Comportamento**: O sincronizador limpa o arquivo temporário `.tmp`, emite uma mensagem de erro e não trava, garantindo que o ciclo continue para os demais arquivos. No próximo ciclo de sincronização agendado, o arquivo será sincronizado normalmente.

### 2. Falha de Conexão SFTP / Timeout
- **Causa**: Firewall bloqueando a porta 22 ou instabilidade temporária do link com a Totvs Cloud.
- **Tratamento**: O cliente SFTP possui retentativa automática configurada para até 3 tentativas com pausa de 5 segundos. Verifique com a equipe de TI ou infraestrutura se o servidor possui liberação de tráfego de saída para o IP/Host do SFTP.

### 3. Acentos ou Caracteres Estranhos no Prompt do Windows
- O script `run_sync.bat` executa `chcp 65001 > nul` para forçar o terminal Windows no padrão UTF-8. Caso execute diretamente via Python, certifique-se de que o terminal suporte UTF-8 ou defina a variável `PYTHONIOENCODING=utf-8`.

### 4. Permissões de Gravação no Destino Local
- Verifique se a conta de serviço que executa a tarefa agendada possui permissão de leitura, escrita e criação de pastas em `C:\balancas`.
