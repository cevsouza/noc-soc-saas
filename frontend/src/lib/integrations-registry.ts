// Single source of truth for every integration type the backend supports (17 total: the
// original 6 + loki/ssh which were partially wired in the UI + 6 added in a recent backend
// session with zero frontend representation until now). Replaces ~5 scattered hardcoded
// arrays that used to live across page.tsx (the `tools` status-polling array, the integration
// catalog cards, the webhook-vs-vault-form branch arrays, and the vault-field dropdown lists)
// — those get consumed from here once the Settings/Integrations panel itself is migrated in a
// later pass. For THIS pass, only the alerts table's source badge consumes it.

export type IngestionMethod = 'webhook' | 'poll';

export interface VaultField {
  key: string;
  label: string;
  inputType: 'text' | 'password';
}

export interface IntegrationRegistryEntry {
  /** Matches backend tenant_integrations.type exactly. */
  id: string;
  name: string;
  shortDescription: string;
  method: IngestionMethod;
  /** Empty array = this integration needs no vault-stored credential. */
  vaultFields: VaultField[];
  accentColorClass: string;
}

export const INTEGRATIONS_REGISTRY: IntegrationRegistryEntry[] = [
  // --- Original 6 (webhook push) ---
  { id: 'zabbix', name: 'Zabbix Monitor', shortDescription: 'Recepção de incidentes de infraestrutura, hosts e serviços.', method: 'webhook', vaultFields: [], accentColorClass: 'border-rose-500/20 text-rose-400' },
  { id: 'prometheus', name: 'Prometheus Alertmanager', shortDescription: 'Alertas de Kubernetes, contêineres e aplicações cloud-native.', method: 'webhook', vaultFields: [], accentColorClass: 'border-purple-500/20 text-purple-400' },
  { id: 'uptimekuma', name: 'Uptime Kuma', shortDescription: 'Notificações de queda de sites, portas TCP e ping ICMP.', method: 'webhook', vaultFields: [], accentColorClass: 'border-emerald-500/20 text-emerald-400' },
  { id: 'wazuh', name: 'Wazuh SIEM', shortDescription: 'Eventos de segurança, auditoria e ameaças de endpoints.', method: 'webhook', vaultFields: [], accentColorClass: 'border-blue-500/20 text-blue-400' },
  { id: 'grafana', name: 'Grafana Webhook', shortDescription: 'Ingestão de triggers visuais baseados em painéis de métricas.', method: 'webhook', vaultFields: [], accentColorClass: 'border-violet-500/20 text-violet-400' },
  {
    id: 'sentinel', name: 'Microsoft Sentinel', shortDescription: 'Varredura ativa de incidentes de segurança na nuvem do Azure.', method: 'poll',
    vaultFields: [
      { key: 'sentinel_client_secret', label: 'Client Secret (Azure API)', inputType: 'password' },
      { key: 'sentinel_client_id', label: 'Client ID (App Registration)', inputType: 'text' },
      { key: 'sentinel_tenant_id', label: 'Tenant ID (Azure Directory)', inputType: 'text' },
      { key: 'sentinel_subscription_id', label: 'Subscription ID', inputType: 'text' },
      { key: 'sentinel_resource_group', label: 'Resource Group', inputType: 'text' },
      { key: 'sentinel_workspace_name', label: 'Workspace Name', inputType: 'text' },
    ],
    accentColorClass: 'border-cyan-500/20 text-cyan-400',
  },

  // --- Already partially wired ---
  {
    id: 'loki', name: 'Grafana Loki', shortDescription: 'Leitura de logs distribuídos com inteligência AIOps em tempo real.', method: 'poll',
    vaultFields: [
      { key: 'loki_url', label: 'Loki Server URL', inputType: 'text' },
      { key: 'loki_username', label: 'Loki Username', inputType: 'text' },
      { key: 'loki_password', label: 'Loki Password', inputType: 'password' },
    ],
    accentColorClass: 'border-orange-500/20 text-orange-400',
  },
  {
    id: 'ssh', name: 'SSH Auto-Cura (Runbooks)', shortDescription: 'Credenciais para execução remota de scripts de mitigação.', method: 'poll',
    vaultFields: [
      { key: 'ssh_private_key', label: 'SSH Private Key (PEM format)', inputType: 'password' },
      { key: 'ssh_username', label: 'SSH Username', inputType: 'text' },
      { key: 'ssh_password', label: 'SSH Password (Fallback)', inputType: 'password' },
    ],
    accentColorClass: 'border-slate-500/20 text-slate-400',
  },

  // --- 6 new backend-only types, zero frontend representation before this pass ---
  { id: 'otlp', name: 'OpenTelemetry (OTLP)', shortDescription: 'Ingestão de logs via padrão OTLP/HTTP+JSON.', method: 'webhook', vaultFields: [], accentColorClass: 'border-indigo-500/20 text-indigo-400' },
  { id: 'icinga', name: 'Icinga2 / Nagios', shortDescription: 'Monitoramento de infraestrutura via notificação de checks.', method: 'webhook', vaultFields: [], accentColorClass: 'border-sky-500/20 text-sky-400' },
  { id: 'cloudwatch', name: 'AWS CloudWatch', shortDescription: 'Alarmes de métricas e logs da infraestrutura AWS, via SNS.', method: 'webhook', vaultFields: [], accentColorClass: 'border-amber-500/20 text-amber-400' },
  { id: 'azuremonitor', name: 'Azure Monitor', shortDescription: 'Alertas de métricas e logs da infraestrutura Azure, via Action Group.', method: 'webhook', vaultFields: [], accentColorClass: 'border-blue-600/20 text-blue-500' },
  {
    id: 'pagerduty', name: 'PagerDuty', shortDescription: 'Recepção de incidentes + escalonamento outbound via routing key.', method: 'webhook',
    vaultFields: [{ key: 'pagerduty_routing_key', label: 'Routing Key (Events API v2)', inputType: 'password' }],
    accentColorClass: 'border-green-600/20 text-green-500',
  },
  {
    id: 'opsgenie', name: 'Opsgenie', shortDescription: 'Recepção de incidentes + escalonamento outbound via API key.', method: 'webhook',
    vaultFields: [{ key: 'opsgenie_api_key', label: 'API Key (Alert API)', inputType: 'password' }],
    accentColorClass: 'border-teal-500/20 text-teal-400',
  },

  // --- Outbound-only escalation channels (Fase 3 fatia 1) — not yet consumed by
  // legacy-cockpit-panels.tsx's "Central de Conectores" grid, which still uses its own
  // hardcoded tool array; kept here for consistency until that panel is migrated to read from
  // this registry.
  {
    id: 'slack', name: 'Slack', shortDescription: 'Escalonamento outbound de alertas críticos/fatais via webhook de entrada.', method: 'webhook',
    vaultFields: [{ key: 'slack_webhook_url', label: 'Incoming Webhook URL', inputType: 'password' }],
    accentColorClass: 'border-fuchsia-500/20 text-fuchsia-400',
  },
  {
    id: 'teams', name: 'Microsoft Teams', shortDescription: 'Escalonamento outbound de alertas críticos/fatais via webhook de entrada.', method: 'webhook',
    vaultFields: [{ key: 'teams_webhook_url', label: 'Incoming Webhook URL', inputType: 'password' }],
    accentColorClass: 'border-indigo-600/20 text-indigo-500',
  },
  {
    id: 'email', name: 'E-mail (SMTP)', shortDescription: 'Escalonamento outbound de alertas críticos/fatais por e-mail (SMTP da plataforma).', method: 'webhook',
    vaultFields: [{ key: 'email_recipient', label: 'E-mail(s) do destinatário', inputType: 'text' }],
    accentColorClass: 'border-slate-400/20 text-slate-300',
  },
];

export function getIntegrationById(id: string): IntegrationRegistryEntry | undefined {
  return INTEGRATIONS_REGISTRY.find((i) => i.id === id);
}

export const WEBHOOK_INTEGRATIONS = INTEGRATIONS_REGISTRY.filter((i) => i.method === 'webhook');
export const VAULT_CONFIGURABLE_INTEGRATIONS = INTEGRATIONS_REGISTRY.filter((i) => i.vaultFields.length > 0);
