# Sincronizador SFTP de Arquivos de Balanças (Go Windows Service)

Sincronizador automático, incremental e de alta confiabilidade de arquivos de carga de balanças (ex: Toledo MGV, Filizola, Urano) a partir de servidor SFTP (Totvs Consinco Cloud) para diretórios locais em servidor Windows (`C:\balancas\<xx>`).

Desenvolvido em Go como um binário nativo único, sem dependências de runtime ou interpretadores instalados no servidor, podendo rodar tanto como **Serviço do Windows** nativo (SCM) quanto via linha de comando (**CLI**).

---

## Índice

1. [Visão Geral](#visão-geral)
2. [Estrutura do Projeto](#estrutura-do-projeto)
3. [Compilação](#compilação)
4. [Instalação e Gerenciamento do Serviço Windows](#instalação-e-gerenciamento-do-serviço-windows)
   - [Instalação via CLI](#instalação-via-cli)
   - [Gerenciamento do Serviço via CLI](#gerenciamento-do-serviço-via-cli)
   - [Gerenciamento Visual via services.msc](#gerenciamento-visual-via-servicesmsc)
   - [Desinstalação do Serviço](#desinstalação-do-serviço)
5. [Configuração (`config.toml`)](#configuração-configtoml)
   - [Arquivo de Configuração Exemplo](#arquivo-de-configuração-exemplo)
   - [Sobrescrita por Variáveis de Ambiente](#sobrescrita-por-variáveis-de-ambiente)
6. [Modos de Execução CLI](#modos-de-execução-cli)
   - [Execução Única (`run`)](#execução-única-run)
   - [Execução em Primeiro Plano / Depuração (`start`)](#execução-em-primeiro-plano--depuração-start)
   - [Modo Simulação (`--dry-run`)](#modo-simulação---dry-run)
   - [Opções e Flags Globais](#opções-e-flags-globais)
7. [Códigos de Saída (Exit Codes)](#códigos-de-saída-exit-codes)
8. [Logs e Observabilidade](#logs-e-observabilidade)
9. [Testes Automatizados](#testes-automatizados)
10. [Resolução de Problemas (Troubleshooting)](#resolução-de-problemas-troubleshooting)

---

## Visão Geral

Em redes de varejo integradas com ERP (ex: Totvs Consinco Cloud), os arquivos de balança (como `txitens.txt`, `itensmgv.txt`, `precopro.txt`, `infnutri.txt`) são exportados para um servidor SFTP em pastas segregadas por loja ou balança (ex: `bal1`, `bal02`, `BAL10`, ..., `BAL92`).

O **`balancas-sync`** realiza:
- **Mapeamento dinâmico via Regex**: Detecta pastas no SFTP que correspondam ao padrão (ex: `(?i)^bal(\d+)$`) e extrai o identificador numérico, mapeando para `C:\balancas\<id>`.
- **Sincronização Incremental Inteligente**: Compara tamanho em bytes (`Size`) e data de modificação (`ModTime`) com tolerância configurável (padrão de 2 segundos). Se o arquivo já estiver atualizado, o download é ignorado, economizando banda e I/O de disco.
- **Remoção após cópia**: Depois de confirmar a gravação local completa, remove o arquivo de origem do SFTP para que ele não seja baixado novamente. O usuário SFTP precisa ter permissão para excluir arquivos. No modo `--dry-run`, não há remoção.
- **Download Atômico**: Baixa inicialmente para um arquivo temporário (`.tmp`) e o renomeia de forma atômica apenas após a conclusão com sucesso da transferência, preservando o `mtime` original do servidor SFTP. Isso impede que softwares como Toledo MGV leiam arquivos corrompidos ou parciais.
- **Tolerância a Bloqueio de Arquivos**: Se o arquivo local estiver bloqueado exclusivamente pelo Toledo MGV ou outro processo, o sincronizador registra o aviso no log e continua o ciclo para os demais arquivos sem travar.

---

## Estrutura do Projeto

```text
balancas/
├── bin/                      # Binários compilados (balancas-sync.exe)
├── cmd/
│   └── balancas-sync/        # Ponto de entrada Cobra CLI e Service Manager
│       ├── main.go
│       ├── root.go
│       └── root_test.go
├── internal/
│   ├── config/               # Leitura e decodificação do config.toml e variáveis de ambiente
│   ├── mapper/               # Expressão regular e mapeamento de diretórios bal<xx> -> <xx>
│   ├── service/              # Ciclo de sincronização e implementação do serviço Windows
│   ├── sftpclient/           # Conexão SSH/SFTP com pool e retentativas automáticas
│   └── syncer/               # Comparação incremental e download atômico
├── config.toml.example       # Modelo documentado de configuração TOML
├── Makefile                  # Alvos para build local, cross-compilação e testes
├── go.mod                    # Módulo Go e dependências
└── README.md                 # Esta documentação
```

---

## Compilação

Para compilar o binário Windows (`balancas-sync.exe`) a partir do Linux ou do Windows com Go instalado:

### Cross-compilação para Windows (AMD64):

```bash
make build-windows
```

O binário otimizado sem símbolos de debug (`-ldflags="-s -w"`) será gerado em `bin/balancas-sync.exe`.

### Compilação local (ambiente atual):

```bash
make build
```

O binário será gerado em `bin/balancas-sync`.

### Instalação pelo módulo Go

O repositório usa o módulo `github.com/cslemes/balancas_consinco`. Depois de publicar uma tag de versão em um repositório GitHub público, instale a CLI com:

```powershell
go install github.com/cslemes/balancas_consinco/cmd/balancas-sync@v0.1.0
```

O comando compila para o sistema operacional atual e instala em `GOBIN` (ou em `GOPATH\bin`). Para instalar em um Windows a partir de outro sistema, defina `GOOS=windows` e `GOARCH=amd64` antes de executar `go install`. É necessário ter uma versão do Go compatível com a diretiva `go` do `go.mod`.

---

## Instalação e Gerenciamento do Serviço Windows

O aplicativo implementa nativamente o protocolo de Serviços do Windows (SCM) usando a biblioteca `kardianos/service`.

> [!IMPORTANT]
> A instalação e controle do serviço requerem que o terminal (PowerShell ou Prompt de Comando) seja aberto com privilégios de **Administrador** ("Executar como Administrador").

### Instalação via CLI

Copie o binário `balancas-sync.exe` e o arquivo `config.toml` para o diretório de destino (ex: `C:\balancas\service` ou `C:\balancas`):

```cmd
cd C:\balancas
balancas-sync.exe service install
```

Caso queira especificar explicitamente o caminho do arquivo de configuração para o serviço:

```cmd
balancas-sync.exe service install --config C:\balancas\config.toml
```

### Gerenciamento do Serviço via CLI

O comando `service` disponibiliza as operações completas do ciclo de vida:

| Comando | Descrição |
| :--- | :--- |
| `balancas-sync.exe service start` | Inicia o serviço Windows em segundo plano |
| `balancas-sync.exe service stop` | Solicita a parada graciosa do serviço |
| `balancas-sync.exe service restart` | Reinicia o serviço Windows |
| `balancas-sync.exe service status` | Consulta o status atual (`Em execução`, `Parado` ou `Desconhecido`) |
| `balancas-sync.exe service uninstall` | Remove o registro do serviço no Windows |

Exemplo de consulta de status:
```cmd
balancas-sync.exe service status
# Saída esperada: Status do serviço: Em execução (Running)
```

### Gerenciamento Visual via `services.msc`

Você também pode gerenciar o serviço pela interface gráfica do Windows:

1. Pressione `Win + R`, digite `services.msc` e pressione `Enter`.
2. Localize o serviço na lista:
   - **Nome:** `BalancasSync`
   - **Nome de Exibição:** `Balanças SFTP Sync Service`
3. Clique duas vezes no serviço para abrir as **Propriedades**:
   - **Tipo de inicialização:** Recomendado definir como **Automático** (ou **Automático (Atraso na Inicialização)**).
   - **Aba Recuperação (Recovery):**
     - Primeira falha: **Reiniciar o Serviço** (Restart the Service).
     - Segunda falha: **Reiniciar o Serviço**.
     - Falhas subsequentes: **Reiniciar o Serviço**.
     - Reiniciar o serviço após: `1 minuto`.
4. Clique em **OK**.

### Desinstalação do Serviço

Para desinstalar o serviço:

```cmd
balancas-sync.exe service stop
balancas-sync.exe service uninstall
```

---

## Configuração (`config.toml`)

O serviço busca o arquivo `config.toml` no diretório atual de execução e no mesmo diretório onde o executável `balancas-sync.exe` reside.

### Arquivo de Configuração Exemplo

Crie seu `config.toml` a partir do `config.toml.example`:

```toml
# ==============================================================================
# Configuração do Serviço de Sincronização de Balanças (Go Windows Service)
# ==============================================================================

[sftp]
# URL completa de conexão SFTP no formato: sftp://host:port ou sftp://user:pass@host:port
url = "sftp://supermercados212430.consinco.cloudtotvs.com.br:2323"

# Alternativamente ou sobrescrevendo os dados da URL:
# host = "supermercados212430.consinco.cloudtotvs.com.br"
# port = 2323
user = "seu_usuario"
password = "sua_senha"
# Fingerprint SHA-256 da chave pública SSH (obrigatório; confirme com o provedor por canal confiável)
host_key_sha256 = "SHA256:SUBSTITUA_PELO_FINGERPRINT_CONFIAVEL"

# Timeout em segundos para conexões e operações SFTP (padrão: 30)
timeout_seconds = 30

# Número máximo de tentativas de reconexão em caso de falha (padrão: 3)
max_retries = 3

[sync]
# Diretório raiz local onde as pastas das balanças residem
local_base_dir = "C:\\balancas"

# Diretório raiz remoto no servidor SFTP (geralmente "." ou caminho relativo/absoluto)
remote_base_dir = "."

# Expressão regular para identificar diretórios de balança (ex: bal1, bal02, BAL10)
folder_pattern = "(?i)^bal(\\d+)$"

# Intervalo entre os ciclos de sincronização contínua em segundos (padrão: 300 = 5 min)
interval_seconds = 300

# Tolerância em segundos para comparação de mtime entre arquivos local e remoto (padrão: 2)
mtime_tolerance_seconds = 2

# Modo simulação (apenas exibe o plano sem baixar/modificar arquivos)
dry_run = false

[logging]
# Caminho para gravação de logs
log_file = "logs/sync_balancas.log"

# Nível de log: debug, info, warn, error (padrão: info)
level = "info"

# Tamanho máximo do arquivo de log antes de rotacionar (em MB)
max_size_mb = 10

# Quantidade máxima de backups preservados na rotação
max_backups = 5
```

### Sobrescrita por Variáveis de Ambiente

Qualquer configuração pode ser sobrescrita definindo variáveis de ambiente com o prefixo `BALANCAS_` e substituição de pontos por sublinhados:

| Variável de Ambiente | Campo Correspondente no TOML |
| :--- | :--- |
| `BALANCAS_SFTP_HOST` | `sftp.host` |
| `BALANCAS_SFTP_PORT` | `sftp.port` |
| `BALANCAS_SFTP_USER` | `sftp.user` |
| `BALANCAS_SFTP_PASSWORD` | `sftp.password` |
| `BALANCAS_SFTP_HOST_KEY_SHA256` | `sftp.host_key_sha256` |
| `BALANCAS_SYNC_LOCAL_BASE_DIR` | `sync.local_base_dir` |
| `BALANCAS_SYNC_DRY_RUN` | `sync.dry_run` |

---

## Modos de Execução CLI

### Execução Única (`run`)

Executa um ciclo único de sincronização e finaliza imediatamente. Ideal para rotinas manuais ou integrações via agendador:

```cmd
balancas-sync.exe run
```

Com arquivo de configuração customizado e simulação:
```cmd
balancas-sync.exe run --config C:\balancas\config.toml --dry-run
```

### Execução em Primeiro Plano / Depuração (`start`)

Executa o loop contínuo de sincronização no console (em primeiro plano), exibindo todas as saídas no terminal. É o modo recomendado para depuração e testes em tempo real:

```cmd
balancas-sync.exe start
```

Para encerrar o processo com segurança, pressione `Ctrl + C`. O worker concluirá o ciclo em andamento e finalizará graciosamente.

### Modo Simulação (`--dry-run`)

Adicione a flag `--dry-run` a qualquer comando para que o sincronizador liste as pastas e arquivos e calcule o plano de atualização sem alterar o disco local:

```cmd
balancas-sync.exe run --dry-run
```

### Opções e Flags Globais

| Flag | Descrição | Padrão |
| :--- | :--- | :--- |
| `-c`, `--config` | Caminho do arquivo de configuração `config.toml` | `./config.toml` ou diretório do executável |
| `--dry-run` | Executa em modo de simulação sem gravar em disco | `false` |
| `--log-level` | Nível de detalhamento do log (`debug`, `info`, `warn`, `error`) | `info` |
| `-h`, `--help` | Exibe a ajuda dos comandos | - |

---

## Códigos de Saída (Exit Codes)

O comando `balancas-sync run` retorna códigos de saída numéricos padronizados:

| Código | Significado | Descrição |
| :---: | :--- | :--- |
| `0` | **Sucesso** | Todos os arquivos foram sincronizados ou já estavam atualizados sem erros. |
| `1` | **Erro Fatal** | Falha de inicialização, configuração inválida ou perda irreversível de conexão SFTP. |
| `2` | **Erro Parcial** | O ciclo completou, mas um ou mais arquivos apresentaram erro (ex: arquivo bloqueado localmente). |

---

## Logs e Observabilidade

- **Saída no Console:** Quando executado de forma interativa (`start` ou `run`), os logs são direcionados tanto para o terminal quanto para o arquivo.
- **Arquivo de Log:** Gravado no caminho indicado em `logging.log_file` (ex: `logs/sync_balancas.log`).
- **Formato dos Registros:**
  ```text
  2026/10/22 12:00:00 ============================================================
  2026/10/22 12:00:00 Iniciando ciclo de sincronização para C:\balancas...
  2026/10/22 12:00:01 Conectando ao SFTP sftp.provedor.com.br:2323 (Tentativa 1/3)...
  2026/10/22 12:00:01 Conexão SFTP estabelecida com sucesso.
  2026/10/22 12:00:02 [BAIXADO] bal1/itensmgv.txt -> itensmgv.txt (142050 bytes)
  2026/10/22 12:00:02 Ciclo concluído em 1.42s | Pastas: 5 | Baixados: 1 | Ignorados: 4 | Erros: 0
  2026/10/22 12:00:02 ============================================================
  ```

---

## Testes Automatizados

Para rodar a suíte completa de testes automatizados do projeto:

```bash
make test
```

Ou diretamente pelo Go:

```bash
go test -v ./...
```

---

## Resolução de Problemas (Troubleshooting)

### 1. Erro de permissão ao instalar o serviço (`Access is denied`)
- **Causa:** O terminal não possui permissões administrativas.
- **Solução:** Abra o PowerShell ou Prompt de Comando clicando com o botão direito e selecionando **"Executar como Administrador"**.

### 2. Toledo MGV bloqueando o arquivo local (`Permission Denied`)
- **Causa:** O software de balança (Toledo MGV) pode abrir arquivos como `txitens.txt` com bloqueio exclusivo temporário durante a leitura.
- **Comportamento:** O `balancas-sync` limpa o arquivo temporário `.tmp` e não trava o ciclo. O arquivo será sincronizado no ciclo seguinte após a liberação do arquivo pelo MGV.

### 3. Falha de conexão SFTP / Timeout
- **Causa:** Bloqueio de firewall na porta de saída (geralmente porta 22 ou porta customizada do SFTP) ou indisponibilidade temporária.
- **Solução:** O sincronizador tenta até `max_retries` (padrão 3 vezes) com espera de 5 segundos entre tentativas. Verifique a conectividade de rede com `Test-NetConnection -ComputerName <host> -Port <porta>` no PowerShell.

### 4. O serviço não encontra o `config.toml`
- **Causa:** Quando iniciado pelo Windows SCM, o diretório de trabalho padrão é `C:\Windows\System32`.
- **Solução:** O `balancas-sync` busca automaticamente no diretório onde o `balancas-sync.exe` está localizado. Para fixar um caminho absoluto, instale o serviço passando o caminho:
  ```cmd
  balancas-sync.exe service install --config C:\balancas\config.toml
  ```
