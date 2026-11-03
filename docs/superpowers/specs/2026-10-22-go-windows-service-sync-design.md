# Especificação de Design: Serviço Windows em Go para Sincronização SFTP de Balanças

**Data:** 2026-10-22  
**Autor:** Antigravity AI Pair Programming  
**Status:** Aprovado  

---

## 1. Visão Geral e Objetivo

Este projeto reformula a solução de sincronização de arquivos de balanças de supermercado (Toledo MGV, Filizola, Urano) para uma aplicação nativa e autocontida em **Go (Golang)**. A aplicação é executada como um **Serviço do Windows (Windows Service)** no servidor local, gerenciada através do **Cobra CLI**, configurada via **Viper** com formato **TOML** (`config.toml`), e realiza a sincronização contínua de diretórios `BAL<xx>` do servidor SFTP (Totvs Consinco Cloud) para as respectivas pastas `C:\balancas\<xx>`.

---

## 2. Requisitos e Diretrizes Globais

1. **Autonomia e Compilação Estática:**
   - Binário único `.exe` sem necessidade de instalar interpretadores (Python, Node, etc.) ou bibliotecas externas de CGO.
   - Suporte nativo a compilação cruzada no Linux para Windows (`GOOS=windows GOARCH=amd64`).

2. **Interface CLI com Cobra:**
   - Comando raiz inteligente que detecta se está sendo iniciado pelo Windows Service Control Manager (SCM) ou pelo terminal.
   - Subcomandos de ciclo de vida do serviço:
     - `balancas-sync service install` (registra o serviço com inicialização automática).
     - `balancas-sync service uninstall` (desregistra o serviço).
     - `balancas-sync service start` (inicia o serviço via SCM).
     - `balancas-sync service stop` (para o serviço via SCM).
     - `balancas-sync service restart` (reinicia o serviço).
     - `balancas-sync service status` (informa se o serviço está parado ou em execução).
   - Subcomandos de execução:
     - `balancas-sync start` (executa o worker em primeiro plano/console).
     - `balancas-sync run` (executa um ciclo único e sai com código de status apropriado).
   - Flags globais:
     - `--config` / `-c` (padrão: `./config.toml` ou no mesmo diretório do `.exe`).
     - `--dry-run` (simulação de sincronização sem escrita em disco).
     - `--log-level` (debug, info, warn, error).

3. **Configuração com Viper e TOML:**
   - Carrega `config.toml` usando `github.com/spf13/viper`.
   - Busca o arquivo no diretório de trabalho corrente e no diretório onde o executável reside (`os.Executable()`).
   - Suporte a sobreposição via variáveis de ambiente com prefixo `BALANCAS_` (ex: `BALANCAS_SFTP_PASSWORD`).
   - Mapeamento robusto para struct Go via tags `mapstructure`.

4. **Gerenciamento de Windows Service com `kardianos/service`:**
   - Implementa a interface `service.Interface` (`Start`, `Stop`).
   - O método `Start` lança a goroutine do worker em segundo plano sem bloquear a inicialização do SCM (evitando erro de timeout de 30s do Windows).
   - O método `Stop` sinaliza encerramento gracioso via canal (`context.Context` ou canal `chan struct{}`), aguarda o término seguro de downloads em andamento e fecha conexões.

5. **Regras de Negócio de Sincronização:**
   - **Mapeamento de Pastas:** Mapeia pastas remotas que casam com `(?i)^bal(\d+)$` (ex: `BAL01` -> `01`, `BAL03` -> `03`) para o destino local `<local_base_dir>/<xx>` (ex: `C:\balancas\01`).
   - **Remoção após cópia:** Um arquivo remoto só é removido após a gravação completa e bem-sucedida da cópia local. O modo `dry-run` preserva os arquivos remotos.
   - **Download Atômico:** Baixa para `<local_path>.<pid>.tmp` e substitui o arquivo original apenas com `os.Rename` após conclusão bem-sucedida.
   - **Sincronização de Data:** Preserva o `mtime` do arquivo remoto no arquivo local via `os.Chtimes`.
   - **Detecção de Mudança:** Pula arquivos cujo tamanho em bytes e data de modificação (`mtime` com tolerância de 2s) sejam idênticos.
   - **Resiliência a Travas do Windows:** Se um arquivo estiver com lock exclusivo (ex: Toledo MGV lendo carga), captura o erro de compartilhamento (`ERROR_SHARING_VIOLATION`), registra warning no log, remove o temporário e continua a sincronização dos demais arquivos.

6. **Conexão SFTP:**
   - Usa `golang.org/x/crypto/ssh` e `github.com/pkg/sftp`.
   - Política `ssh.InsecureIgnoreHostKey()`.
   - Retentativas de conexão automáticas com backoff.
   - Desativa explicitamente agentes SSH e chaves locais não configuradas para evitar erros de autenticação excessiva.

---

## 3. Estrutura do Projeto

```text
balancas/
├── cmd/
│   └── balancas-sync/
│       ├── main.go             # Ponto de entrada, integração com Cobra e kardianos/service
│       └── root.go             # Configuração do comando raiz Cobra e flags
├── internal/
│   ├── config/                 # Viper loader e validação de structs
│   │   ├── config.go
│   │   └── config_test.go
│   ├── mapper/                 # Mapeamento regex BAL<xx> -> xx
│   │   ├── mapper.go
│   │   └── mapper_test.go
│   ├── sftpclient/             # Cliente SSH/SFTP com retry, keepalive e listagem
│   │   ├── client.go
│   │   └── client_test.go
│   ├── syncer/                 # Núcleo de download atômico, mtime e detecção
│   │   ├── syncer.go
│   │   └── syncer_test.go
│   └── service/                # Implementação do serviço Windows (kardianos/service)
│       ├── service.go
│       └── worker.go
├── config.toml                 # Configuração padrão ativa
├── config.toml.example         # Exemplo documentado
├── Makefile                    # Comandos de build, cross-compile para windows e testes
├── go.mod
├── go.sum
└── README.md                   # Guia de instalação e operação do serviço
```

---

## 4. Estratégia de Testes

1. **Testes Unitários:**
   - Teste do parser TOML e Viper: validação de defaults, substituição de variáveis e parsing de URL `sftp://`.
   - Teste do mapeador regex: correspondência case-insensitive, extração de dígitos e pastas ignoradas.
   - Teste do núcleo de sincronização: mock de atributos remotos, detecção de alteração por tamanho e mtime, download atômico com rename e recuperação de falhas.
2. **Testes de Integração e Live:**
   - Teste de conexão real contra o servidor SFTP da Totvs Consinco Cloud (`supermercados212430.consinco.cloudtotvs.com.br:2323`).
   - Teste em modo `dry-run` garantindo nenhuma alteração em disco.
3. **Validação de Compilação Cruzada:**
   - `GOOS=windows GOARCH=amd64 go build -o bin/balancas-sync.exe ./cmd/balancas-sync`
