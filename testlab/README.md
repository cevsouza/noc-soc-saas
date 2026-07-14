# Lab de testes — monitoramento → NOC/SOC

Sobe ferramentas de monitoramento reais (Prometheus, Alertmanager, Grafana, Uptime Kuma e,
opcionalmente, Zabbix) **já apontando para o seu NOC**. Os alertas chegam no tenant de teste
via `?token=SUA_CHAVE`. Requisito: Docker + Docker Compose.

## Passo a passo (3 comandos)

1. **Gere a chave** na plataforma: Cockpit → **Configuração MSP → Como Conectar**, selecione o
   tenant **`noc`** no topo → **Gerar chave** (copie — aparece uma vez). Confira também que os
   conectores estão **ativos** para esse tenant em *Central de Conectores*.

2. **Configure o `.env`**:
   ```bash
   cp .env.example .env      # (Windows: Copy-Item .env.example .env)
   # edite o .env e cole a chave em NOC_TOKEN
   ```

3. **Renderize e suba**:
   ```powershell
   # Windows (PowerShell)
   .\setup.ps1
   docker compose up -d
   ```
   ```bash
   # Linux/Mac
   ./setup.sh
   docker compose up -d
   ```

Pronto. Agora acompanhe no cockpit → **Alertas** e **Incidentes**.

---

## O que esperar de cada ferramenta

| Ferramenta | Acesso local | Ação para gerar alerta | Esforço |
|---|---|---|---|
| **Prometheus + Alertmanager** | http://localhost:9090 · :9093 | **Nenhuma** — `NocLabHeartbeat` dispara em ~1 min; `EndpointDown` abre/fecha sozinho (gerador de problemas) | ✅ automático |
| **Grafana** | http://localhost:3000 (admin/admin) | Alerting → Contact points → **NOC-SOC** → botão **Test** | 1 clique |
| **Uptime Kuma** | http://localhost:3001 | Config manual (abaixo) | poucos cliques |
| **Zabbix** (opcional) | http://localhost:8080 (Admin/zabbix) | Config manual (abaixo) | avançado |

> A **prova mais rápida** de que tudo funciona é o Prometheus: em ~1 minuto você deve ver o alerta
> `NocLabHeartbeat` aparecer em **Alertas**, virar **Incidente** e ganhar um *risk score*. Reenvios
> mostram o dedupe/recorrência.

### Gerador de problemas (automático)
O serviço `flaky-target` fica **90s no ar e 45s fora**, em loop. O Prometheus sonda ele (via
`blackbox-exporter`) e dispara o alerta **`EndpointDown`** quando cai — e o **resolve** quando volta.
Ou seja: incidentes realistas que **abrem e fecham sozinhos** a cada ~2 minutos, sem você forçar nada.
O **Uptime Kuma** também pode monitorar esse mesmo alvo: use a URL `http://flaky-target:8080` ao criar
o monitor (os dois containers estão na mesma rede).

### Uptime Kuma (manual)
O Kuma não tem provisionamento por arquivo, então são alguns cliques na UI:
1. Abra http://localhost:3001 e crie o usuário admin.
2. **Settings → Notifications → Setup Notification** → Notification Type: **Webhook**.
   - Content Type: `application/json`
   - Post URL: `https://noc-soc-saas-production.up.railway.app/api/v1/ingest/uptimekuma?token=SUA_CHAVE`
   *(copie essa URL pronta em Como Conectar → Uptime Kuma)*.
3. **Add New Monitor** → aponte para algo que caia (ex.: `http://localhost:9999`) e marque a
   notificação criada. Quando ficar DOWN, o alerta chega no NOC.

### Zabbix (opcional, `docker compose --profile zabbix up -d`)
Sobe em ~2–3 min. Depois, na UI (http://localhost:8080, **Admin / zabbix**):
1. **Alertas → Tipos de mídia → Criar** (tipo Webhook). Use os parâmetros e o script exatos que a
   plataforma mostra em **Como Conectar → Zabbix** (alert_subject, host, severity, event_value… e
   `req.post(<URL>, value)`).
2. **Alertas → Ações → Ações de trigger** → crie uma ação que use esse tipo de mídia.
3. Force um problema (ex.: pare o `zabbix-agent`) para o trigger disparar.

---

## Parar e limpar
```bash
docker compose down            # para os serviços
docker compose --profile zabbix down -v   # remove tudo, inclusive volumes do Zabbix
```

## Notas
- `./rendered/` e `.env` **não** vão para o git (contêm o token). Rode o `setup` de novo se trocar a chave.
- Todos os endpoints usam `?token=`; se preferir cabeçalho, veja a alternativa `X-API-Key` em *Como Conectar*.
- Rode simulações de ataque só em lab isolado e autorizado.
