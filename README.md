# SaaS Multi-Tenant NOC/SOC (Manager of Managers) 🚀

Este repositório contém uma solução completa de **SaaS Multi-tenant para NOC/SOC Unificado (Manager of Managers)**, construída sob os mais rigorosos padrões de confiabilidade (SRE), isolamento estrito de dados e processamento em tempo real.

O sistema integra alertas de infraestrutura (Prometheus), segurança (Wazuh SIEM) e nuvem (Microsoft Sentinel), realiza deduplicação inteligente com Redis, aplica regras de IA/Heurísticas para conter tempestades de alertas (Python), provê auto-cura via SSH e fornece dashboards em tempo real (Next.js) com relatórios SLA em PDF.

---

## 🛠️ Stack Tecnológica

*   **Backend Core (APIs, Ingestão e WS):** Go (Golang) com concorrência escalável.
*   **Frontend Cockpit:** Next.js (React/TypeScript) com TailwindCSS em Dark Mode.
*   **Banco de Dados:** PostgreSQL (com Row-Level Security - RLS).
*   **Mensageria & WebSockets:** Redis (filas FIFO e Pub/Sub multiplexado).
*   **Workers de Automação, IA e Relatórios:** Python 3 (Paramiko, ReportLab, Psycopg2).

---

## 📂 Estrutura do Repositório

```
├── cmd/noc-api/            # Ponto de entrada do backend Go (main.go)
├── internal/               # Pacotes internos do Go
│   ├── api/                # Handlers de ingestão e endpoints HTTP
│   ├── connector/          # Conector de polling para Microsoft Sentinel
│   ├── db/                 # Roteamento e transações de Tenant/RLS
│   ├── executor/           # Motor de execução de Runbooks e mitigação
│   ├── loki/               # Cliente HTTP do Grafana Loki (LogQL)
│   ├── middleware/         # Middleware de JWT, RBAC e API Keys
│   ├── model/              # Modelos estruturais (UnifiedIncident, Alert)
│   ├── repository/         # Camada de banco de dados (Vault, Alerts, etc.)
│   └── ws/                 # WebSocket Hub e Pub/Sub multiplexer
├── workers/                # Scripts de IA e automação em Python
│   ├── ai_worker.py        # Worker de supressão de ruídos e IA
│   ├── ssh_remediation.py  # Script de auto-cura remota via SSH (Paramiko)
│   ├── sla_report_generator.py # Compilador de relatórios SLA em PDF (ReportLab)
│   ├── security_crypto.py  # Criptografia simétrica AES-GCM-256
│   └── test_*.py           # Suítes de testes unitários em Python
├── infra/                  # Arquivos de configuração (Prometheus/Alertmanager)
├── docker-compose.yml      # Provisionamento local do banco, fila e monitoramento
└── README.md               # Este guia passo a passo
```

---

## 🚀 Guia de Configuração e Inicialização

### Passo 1: Subir a Infraestrutura (Docker)
Certifique-se de que possui o Docker e o Docker Compose instalados. No diretório raiz do projeto, execute:
```bash
docker-compose up -d
```
Isso inicializará os seguintes containers:
*   **PostgreSQL (15-alpine):** Banco de dados relacional acessível na porta `5432`.
*   **Redis (7-alpine):** Fila e Broker Pub/Sub acessível na porta `6379`.
*   **Prometheus & Alertmanager:** Servidores de monitoramento locais nas portas `9090` e `9093`.
*   **Loki:** Servidor de agregação de logs na porta `3100`.

### Passo 2: Preparar o Backend Go
1.  Certifique-se de ter o Go instalado (versão 1.20+).
2.  Configure a chave mestre de criptografia no terminal (utilizada para descriptografar dados sensíveis do vault do tenant):
    *   **Windows (PowerShell):** `$env:VAULT_MASTER_KEY="minha-chave-mestre-de-32-bytes!!!"`
    *   **Linux/macOS:** `export VAULT_MASTER_KEY="minha-chave-mestre-de-32-bytes!!!"`
3.  Inicie a API HTTP e os workers Go:
    ```bash
    go run cmd/noc-api/main.go
    ```
    A API estará rodando na porta `8080`.

### Passo 3: Preparar o Frontend Next.js
1.  Navegue até a pasta frontend: `cd frontend`
2.  Instale as dependências: `npm install`
3.  Inicie o servidor de desenvolvimento: `npm run dev`
    O Cockpit estará acessível em `http://localhost:3000`.

### Passo 4: Inicializar os Workers Python
1.  Navegue até a pasta de workers: `cd workers`
2.  Configure a mesma variável de ambiente da chave mestre:
    *   **Windows (PowerShell):** `$env:VAULT_MASTER_KEY="minha-chave-mestre-de-32-bytes!!!"`
    *   **Linux/macOS:** `export VAULT_MASTER_KEY="minha-chave-mestre-de-32-bytes!!!"`
3.  Inicie o worker de IA para escutar a fila de supressão:
    ```bash
    python ai_worker.py
    ```

---

## 🧪 Roteiro de Testes e Simulações

Como todo o sistema é mockable/simulável por padrão, você pode testar todo o fluxo ponta a ponta localmente.

### 1. Testar Ingestão do Prometheus Alertmanager (Push)
Simule o envio de um alerta do Prometheus disparado pelo Alertmanager para o nosso endpoint HTTP:
```bash
curl -X POST http://localhost:8080/api/v1/ingest/prometheus?token=e1b7c123-1234-4321-abcd-123456789abc \
  -H "Content-Type: application/json" \
  -d '{
    "receiver": "webhook",
    "status": "firing",
    "alerts": [
      {
        "status": "firing",
        "labels": {
          "alertname": "HostHighCpuLoad",
          "instance": "web-server-01",
          "severity": "critical"
        },
        "annotations": {
          "summary": "CPU a 96% no web-server-01",
          "description": "Uso de CPU acima do limite de 90% por mais de 5 minutos."
        },
        "startsAt": "2026-06-30T12:00:00Z",
        "fingerprint": "prom-fingerprint-123"
      }
    ]
  }'
```
*Observe que o alerta aparecerá instantaneamente no cockpit do Tenant A!*

### 2. Testar Ingestão do Wazuh Security Event (Push)
Simule o Wazuh despachando um alerta de segurança contra força bruta SSH:
```bash
curl -X POST http://localhost:8080/api/v1/ingest/wazuh?token=e1b7c123-1234-4321-abcd-123456789abc \
  -H "Content-Type: application/json" \
  -d '{
    "timestamp": "2026-06-30T12:05:00Z",
    "rule": {
      "level": 10,
      "comment": "SSH brute force authentication failed",
      "sid": 5716,
      "id": "5716",
      "groups": ["syslog", "sshd", "security_event"]
    },
    "agent": {
      "id": "002",
      "name": "gateway-router",
      "ip": "192.168.1.254"
    },
    "location": "/var/log/auth.log",
    "full_log": "Failed password for root from 185.190.140.12 port 43210 ssh2"
  }'
```

### 2.1 Testar Outros Coletores (Uptime Kuma, Grafana, Zabbix)

#### A. Simular Alerta do Uptime Kuma (Monitor Down)
```bash
curl -X POST http://localhost:8080/api/v1/ingest/uptimekuma?token=e1b7c123-1234-4321-abcd-123456789abc \
  -H "Content-Type: application/json" \
  -d '{
    "heartbeat": {
      "monitorID": 1,
      "status": 0,
      "time": "2026-06-30 12:00:00.000",
      "msg": "Connection timeout"
    },
    "monitor": {
      "id": 1,
      "name": "Database Principal",
      "hostname": "db-server-01",
      "url": "10.0.0.5",
      "type": "ping"
    },
    "msg": "[Database Principal] [🔴 Down] Connection timeout"
  }'
```

#### B. Simular Alerta do Zabbix (Trigger Problem)
```bash
curl -X POST http://localhost:8080/api/v1/ingest/zabbix?token=e1b7c123-1234-4321-abcd-123456789abc \
  -H "Content-Type: application/json" \
  -d '{
    "alert_subject": "PROBLEM: Spikes em Disco no DB-01",
    "alert_message": "Uso de disco em 94% no db-server-01",
    "host": "db-server-01",
    "severity": "High",
    "trigger_id": "12345",
    "event_id": "98765",
    "event_value": "1"
  }'
```

#### C. Simular Alerta do Grafana (Firing Rule)
```bash
curl -X POST http://localhost:8080/api/v1/ingest/grafana?token=e1b7c123-1234-4321-abcd-123456789abc \
  -H "Content-Type: application/json" \
  -d '{
    "receiver": "grafana-webhook",
    "status": "firing",
    "alerts": [
      {
        "status": "firing",
        "labels": {
          "alertname": "Muitas Conexoes no BD",
          "instance": "db-server-01",
          "severity": "critical"
        },
        "annotations": {
          "summary": "Quantidade de conexões ativas alta",
          "description": "Banco de dados atingiu 150 conexões."
        },
        "startsAt": "2026-06-30T12:00:00Z",
        "fingerprint": "grafana-fingerprint-456"
      }
    ],
    "title": "[FIRING:1] Muitas Conexoes (db-server-01)"
  }'
```

### 3. Testar a Supressão Automática de IA (Deduplicação e Flapping)
1.  Dispare o mesmo alerta do Prometheus 9 vezes consecutivas em menos de 1 minuto.
2.  O Go Worker acumulará o número de ocorrências (`occurrences: 9x`) protegendo o banco via Redis.
3.  O AI Worker detectará que o host `web-server-01` está gerando ruído excessivo ("flapping"), alterando o estado do alerta para `SUPPRESSED` com a justificativa: *"Flapping noise: occurred 9 times in the last hour"*.
4.  O painel do Cockpit atualizará em tempo real refletindo o status suprimido e exibindo o diagnóstico de IA.

### 4. Executar os Testes Unitários de Automação
Para rodar a cobertura de testes do motor de IA, criptografia e mitigação remota:
```bash
cd workers
python -m unittest test_security_crypto.py
python -m unittest test_ai_worker.py
python -m unittest test_ssh_remediation.py
python -m unittest test_sla_report_generator.py
```

---

## 📘 Manual do Usuário e Operador

### 1. Seleção de Tenant (Multi-tenancy)
Na barra superior do Cockpit Next.js, há um seletor **Visual Domain**. Ao alterar entre *Telco Global* e *Quantum Cloud*, o painel reconecta o WebSocket utilizando o token de sessão do respectivo cliente, separando logicamente os alertas de forma estrita.

### 2. Ações Rápidas de Operação
Ao selecionar um alerta crítico na fila de triagem:
*   Clique em **Acknowledge** para sinalizar ao time de SRE que o incidente está sob investigação. O status mudará para amarelo.
*   Clique em **Resolve Alert** para finalizar a triagem e arquivar o incidente.

### 3. Inspeção Lateral Tabulada (Cockpit Sidebar)
*   **Aba General:** Veja o resumo, a gravidade e o log analítico gerado pelo robô de IA detalhando se houve rebaixamento de nível devido à frequência.
*   **Aba Loki Logs:** Veja o terminal integrado contendo as últimas 50 linhas de logs do host que gerou o alerta. Facilita o diagnóstico de *OOM Killer* ou *crases* sem sair do dashboard.
*   **Aba Grafana:** Visualize gráficos dinâmicos e gauge simulados de CPU/Memória do host em tempo real.
*   **Aba Raw JSON:** Acesse o JSON cru recebido da integração de origem para análises complexas de cabeçalho.

### 4. Relatório de SLA Mensal em PDF
Para apresentar o progresso do time NOC/SOC ao cliente:
1.  Clique no botão **SLA Report** no cabeçalho.
2.  O sistema gerará instantaneamente um PDF de auditoria contendo as médias de MTTA e MTTR do último mês, os gráficos de conformidade do acordo de serviço (SLA) e uma tabela com os incidentes mais recentes relevantes.
