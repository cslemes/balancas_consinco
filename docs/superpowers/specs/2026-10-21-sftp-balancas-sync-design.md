# Especificação de Design: Sincronizador SFTP para Pastas de Balanças

**Data:** 2026-10-21  
**Autor:** Antigravity AI Pair Programming  
**Status:** Aprovado  

---

## 1. Contexto e Objetivo

Em ambientes de varejo e supermercados (especialmente integrados com ERPs como Consinco / Totvs Cloud), arquivos de carga de balanças (como `txitens.txt`, `itensmgv.txt`, `precopro.txt`, `infnutri.txt` para Toledo MGV, Filizola, Urano, etc.) são gerados e disponibilizados em um servidor SFTP em diretórios segregados por loja/balança (ex: `BAL01`, `BAL03`, `BAL04`, ..., `BAL92`, `BAL93`).

O objetivo deste projeto é disponibilizar um script Python autônomo, robusto e de fácil manutenção para executar em um servidor Windows, sincronizando os arquivos de cada pasta `BAL<xx>` do SFTP diretamente para as respectivas pastas locais `C:\balancas\<xx>` (ou caminho configurável).

---

## 2. Requisitos

### 2.1 Requisitos Funcionais
1. **Conexão SFTP Flexível:**
   - Obter credenciais prioritariamente a partir do arquivo `.env`.
   - Suportar a sintaxe `SFTP_URL=sftp://<host>:<port>` ou campos separados (`SFTP_HOST`, `SFTP_PORT`).
   - Autenticação via usuário e senha (`SFTP_USER`, `SFTP_PASSWORD`).
   - Timeout de conexão e política de reconexão configuráveis.

2. **Mapeamento Dinâmico de Diretórios:**
   - Varrer a raiz remota do SFTP e identificar pastas que casem com a expressão regular `(?i)^bal(\d+)$` (ou padrão configurável).
   - Extrair o sufixo numérico `<xx>` (ex.: `BAL01` -> `01`, `BAL03` -> `03`, `BAL10` -> `10`).
   - Mapear para o destino local `<LOCAL_BASE_DIR>\<xx>` (padrão: `C:\balancas\<xx>`).
   - Criar automaticamente os diretórios locais de destino caso ainda não existam.

3. **Sincronização Incremental Eficiente:**
   - Manter os arquivos remotos no servidor SFTP intactos.
   - Antes de baixar, verificar se o arquivo já existe no destino local.
   - Se o arquivo local tiver exatamente o mesmo tamanho em bytes (`st_size`) e data de modificação (`st_mtime`), ignorar o download.
   - Se o arquivo for novo ou tiver tamanho/mtime divergentes, realizar o download.

4. **Download Seguro e Atômico:**
   - Realizar o download para um arquivo temporário (`.tmp`) no destino local.
   - Renomear o arquivo `.tmp` para o nome final apenas após a conclusão bem-sucedida da transferência.
   - Atualizar a data de modificação local (`os.utime`) para refletir o `mtime` do SFTP, garantindo que a próxima verificação reconheça que o arquivo está sincronizado.
   - Evita que softwares de balança (MGV6 / MGV7 da Toledo) tentem ler arquivos que ainda estão sendo gravados.

5. **Modos de Execução:**
   - **Modo Pontual (One-off / Task Scheduler):** Executa uma única passada de sincronização e finaliza com código de saída 0 (ou código de erro caso ocorra falha crítica). Ideal para ser agendado no Agendador de Tarefas do Windows a cada N minutos.
   - **Modo Contínuo (Loop / Daemon):** Quando executado com a flag `--loop` ou se a variável de ambiente `SYNC_INTERVAL_SECONDS > 0`, roda em loop infinito, aguardando o intervalo configurado entre as sincronizações.
   - **Dry-run:** Parâmetro `--dry-run` para simular o processo sem baixar ou alterar arquivos em disco.

6. **Observabilidade e Logs:**
   - Geração de logs tanto no console (com formatação limpa e timestamps) quanto em arquivo com rotação diária ou por tamanho (`logs/sync_balancas.log`).
   - Nível de log configurável (`INFO`, `DEBUG`, `WARNING`, `ERROR`).
   - Resumo ao término de cada ciclo: total de pastas verificadas, arquivos baixados, arquivos ignorados (já atualizados) e erros encontrados.

7. **Tratamento de Bloqueios no Windows:**
   - Se um arquivo local estiver aberto com bloqueio exclusivo por outro processo (ex: Toledo MGV processando a carga), capturar `PermissionError`, emitir aviso e prosseguir com os demais arquivos sem travar ou abortar o script.

---

## 3. Arquitetura e Estrutura dos Arquivos

```text
balancas/
├── .env                     # Variáveis de ambiente e credenciais (ignorado no git)
├── .env.example             # Exemplo de configuração
├── .gitignore               # Ignora .env, venv, logs e temporários
├── requirements.txt         # Dependências do Python (paramiko, python-dotenv)
├── sync_balancas.py         # Script principal de sincronização
├── run_sync.bat             # Atalho batch para Windows / Agendador de Tarefas
├── README.md                # Guia de instalação e configuração no Windows
└── docs/
    └── superpowers/
        └── specs/
            └── 2026-10-21-sftp-balancas-sync-design.md
```

---

## 4. Componentes de Software

### 4.1 Módulo de Configuração (`Config`)
- Carrega variáveis do arquivo `.env` com fallback para variáveis do sistema e parâmetros de linha de comando (`argparse`).
- Parse da URL `SFTP_URL` via `urllib.parse.urlsplit` para extração de host, porta (default 22 ou 2323), e suporte a credenciais embutidas se houver.
- Normalização de caminhos no Windows usando `pathlib.Path`.

### 4.2 Cliente SFTP (`SFTPClientManager`)
- Gerenciamento de conexão SSH e cliente SFTP usando `paramiko.SSHClient`.
- Configuração de `AutoAddPolicy` ou `RejectPolicy` para chaves de host.
- Método de contexto (`contextmanager`) para garantir fechamento apropriado da conexão SSH e do canal SFTP.
- Retentativa automática com backoff exponencial caso a conexão falhe por instabilidade de rede.

### 4.3 Sincronizador de Balanças (`BalancasSynchronizer`)
- `list_bal_folders()`: Varrer o diretório remoto raiz procurando pastas compatíveis com `BAL<xx>`.
- `sync_folder(remote_folder, local_folder)`: Varrer arquivos dentro da pasta remota, comparar atributos (`st_size`, `st_mtime`) e disparar download se necessário.
- `download_file_atomic(sftp, remote_file_path, local_target_path, remote_mtime)`: Download para `.tmp` e rename atômico.
- `run_cycle()`: Orquestra a sincronização de todas as pastas encontradas e compila as estatísticas.

---

## 5. Estratégia de Testes

1. **Testes Unitários:**
   - Teste de parsing de configuração e URL SFTP (`sftp://user:pass@host:port` ou variáveis separadas).
   - Teste de regex e mapeamento de nomes de pastas (`BAL01` -> `01`, `BAL03` -> `03`, `BALANCAS` -> ignorado).
   - Teste da lógica de decisão de sincronização (arquivo ausente vs existente com mesmo tamanho/data vs modificado).

2. **Testes de Integração:**
   - Conexão e listagem real contra o servidor SFTP configurado no `.env`.
   - Download de arquivo de teste para diretório local temporário e validação de integridade.
