'use client';

import { useEffect, useState } from 'react';
import { Bar, BarChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import {
  Activity,
  Ban,
  BellOff,
  Check,
  Copy,
  Cpu,
  Eye,
  EyeOff,
  Boxes,
  FileText,
  HelpCircle,
  Layers,
  Lock,
  Palette,
  Radar,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  Terminal,
  TrendingUp,
  User,
} from 'lucide-react';
import { useAuth } from '@/lib/auth-context';
import { useTenantSelection } from '@/lib/tenant-context';
import { apiFetch } from '@/lib/api-client';
import { API_BASE_URL } from '@/lib/env';
import { RunbookApprovalsPanel } from '@/components/settings/runbook-approvals-panel';
import { ResponseActionsPanel } from '@/components/settings/response-actions-panel';
import { AccessControlPanel } from '@/components/settings/access-control-panel';
import { OperationalKpisPanel } from '@/components/settings/operational-kpis-panel';
import { TopologyView } from '@/components/settings/topology-view';
import { NetworkDiscoveryPanel } from '@/components/settings/network-discovery-panel';
import { AssetsPanel } from '@/components/settings/assets-panel';
import { SuppressionRulesPanel } from '@/components/settings/suppression-rules-panel';
import type { SLAExecutiveStats } from '@/types';

type AsyncStatus = { status: 'idle' | 'saving' | 'success' | 'error'; message?: string };

interface LegacyCockpitPanelsProps {
  /** Only 'topology' and 'settings' render here — 'alerts' is handled by the new components. */
  cockpitTab: 'topology' | 'settings';
  /** Topology nodes filter the (Alerts tab's) search term on click — ported from the original
   * single-component behavior where both lived in the same `searchTerm` state. */
  onSearchTermChange: (term: string) => void;
  /** Cross-link from the Topology tab to the Descoberta de Rede panel (which lives under Settings):
   * switch the cockpit tab to 'settings'. Combined with selecting the network_discovery sub-panel. */
  onNavigateSettings?: () => void;
}

/**
 * Everything from the original monolithic page.tsx that isn't the login screen or the Alerts
 * tab (both already migrated to dedicated routes/components). Ported as close to verbatim as
 * possible from page.tsx's Topology tab (lines 2481-2584) and Settings/"Configuração MSP" tab
 * (lines 2586-3958, itself a split-pane with ~8 sub-panels switched via `selectedIntegrationTool`).
 *
 * Dropped in this pass (all were only reachable via the old inline header, which no longer
 * exists now that `AppHeader` owns that space): shift handover overlay/modal, active-users
 * modal, TV wallboard mode, SLA PDF report button, and the alert drill-down summary modal.
 * Also dropped: several handlers/state that were already dead in the original file (unused
 * MSP per-tenant integration management via `selectedAdminTenant`, the mock-alerts cleanup
 * button, and the event simulator) — confirmed via grep that none of them were referenced by
 * the JSX being ported. All of this is deferred to a future session, not silently lost.
 */
export function LegacyCockpitPanels({ cockpitTab, onSearchTermChange, onNavigateSettings }: LegacyCockpitPanelsProps) {
  const { token, user } = useAuth();
  const { tenants, setTenants, selectedTenant, setSelectedTenantIds, refetchTenants } = useTenantSelection();

  // Tenant management (Settings > Clientes)
  const [newTenantName, setNewTenantName] = useState('');
  const [tenantCreateStatus, setTenantCreateStatus] = useState<AsyncStatus>({ status: 'idle' });

  // Unified Connectors Health Center: tracks which connector card is expanded (write-only in
  // the original too — kept for parity, nothing currently reads it back).
  const [activeSubTool, setActiveSubTool] = useState<string | null>(null);

  // Admin user creation (Settings > Usuários)
  const [adminUserEmail, setAdminUserEmail] = useState('');
  const [adminUserPassword, setAdminUserPassword] = useState('');
  const [adminUserName, setAdminUserName] = useState('');
  const [adminUserRole, setAdminUserRole] = useState('operator');
  const [adminUserTenantIds, setAdminUserTenantIds] = useState<string[]>([]);
  const [adminUserStatus, setAdminUserStatus] = useState<AsyncStatus>({ status: 'idle' });
  const [showAdminUserPassword, setShowAdminUserPassword] = useState(false);
  const [adminUsers, setAdminUsers] = useState<any[]>([]);
  const [isLoadingAdminUsers, setIsLoadingAdminUsers] = useState(false);

  // Settings left-nav (which of the ~8 sub-panels is active)
  const [selectedIntegrationTool, setSelectedIntegrationTool] = useState<string>('integrations_admin');
  const [copiedText, setCopiedText] = useState(false);

  // Vault secret storage (Settings > Credenciais)
  const [vaultKey, setVaultKey] = useState('ssh_private_key');
  const [vaultValue, setVaultValue] = useState('');
  const [saveStatus, setSaveStatus] = useState<AsyncStatus>({ status: 'idle' });
  const [vaultSecrets, setVaultSecrets] = useState<any[]>([]);
  const [isLoadingVaultSecrets, setIsLoadingVaultSecrets] = useState(false);

  // Tenant's own active integrations (Settings > Integrações)
  const [integrations, setIntegrations] = useState<any[]>([]);
  const [integrationName, setIntegrationName] = useState('');
  const [integrationStatus, setIntegrationStatus] = useState<AsyncStatus>({ status: 'idle' });
  const [validationResult, setValidationResult] = useState<any>(null);
  const [isValidating, setIsValidating] = useState(false);
  const [connectorStatuses, setConnectorStatuses] = useState<Record<string, any>>({});

  // Runbook audits (Settings > Auditoria)
  const [runbookAudits, setRunbookAudits] = useState<any[]>([]);
  const [isLoadingRunbookAudits, setIsLoadingRunbookAudits] = useState(false);

  // SRE Playbooks admin (Settings > Playbooks)
  const [settingsPlaybooks, setSettingsPlaybooks] = useState<any[]>([]);
  const [isLoadingSettingsPlaybooks, setIsLoadingSettingsPlaybooks] = useState(false);
  const [playbookName, setPlaybookName] = useState('');
  const [playbookTrigger, setPlaybookTrigger] = useState('');
  const [playbookScript, setPlaybookScript] = useState('');
  const [playbookVaultKey, setPlaybookVaultKey] = useState('ssh');
  const [playbookStatus, setPlaybookStatus] = useState<AsyncStatus>({ status: 'idle' });
  const [playbookTargetTenantId, setPlaybookTargetTenantId] = useState<string>('all');
  const [playbooksFilter, setPlaybooksFilter] = useState<'all' | 'tenant'>('all');

  // SLA report (Settings > Relatórios)
  const [slaData, setSlaData] = useState<SLAExecutiveStats | null>(null);
  const [isLoadingSla, setIsLoadingSla] = useState(false);
  const [reportMode, setReportMode] = useState<'executive' | 'technical'>('executive');

  const fetchIntegrations = async () => {
    if (!token) return;
    try {
      const response = await apiFetch('/api/v1/integrations');
      if (response.ok) {
        setIntegrations(await response.json());
      }
    } catch (err) {
      console.error('Falha ao buscar integrações:', err);
    }
  };

  const fetchAdminUsers = async () => {
    if (!token || user?.role !== 'admin') return;
    setIsLoadingAdminUsers(true);
    try {
      const response = await apiFetch('/api/v1/admin/users');
      if (response.ok) {
        setAdminUsers(await response.json());
      }
    } catch (err) {
      console.error('Falha ao buscar usuários:', err);
    } finally {
      setIsLoadingAdminUsers(false);
    }
  };

  const handleDeleteUser = async (id: string) => {
    if (!token) return;
    if (!confirm('Deseja excluir este usuário do NOC permanentemente?')) return;
    try {
      const response = await apiFetch(`/api/v1/admin/users?id=${id}`, { method: 'DELETE' });
      if (response.ok) {
        fetchAdminUsers();
      } else {
        alert((await response.text()) || 'Falha ao excluir usuário.');
      }
    } catch (err) {
      console.error(err);
    }
  };

  const handleDeleteTenant = async (id: string) => {
    if (!token) return;
    if (!confirm('ATENÇÃO: A exclusão do tenant removerá todos os alertas, regras e conectores associados permanentemente! Deseja continuar?')) return;
    try {
      const response = await apiFetch(`/api/v1/tenants?id=${id}`, { method: 'DELETE' });
      if (response.ok) {
        await refetchTenants();
      } else {
        alert('Falha ao excluir tenant.');
      }
    } catch (err) {
      console.error(err);
    }
  };

  const handleCreateTenant = async (e: React.FormEvent) => {
    e.preventDefault();
    setTenantCreateStatus({ status: 'saving' });
    try {
      const response = await apiFetch('/api/v1/tenants', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newTenantName }),
      });
      if (response.ok) {
        const data = await response.json();
        setTenantCreateStatus({ status: 'success', message: `Tenant '${data.name}' criado com sucesso!` });
        setNewTenantName('');
        await refetchTenants();
      } else {
        const msg = await response.text();
        setTenantCreateStatus({ status: 'error', message: msg || 'Falha ao criar tenant.' });
      }
    } catch (err) {
      setTenantCreateStatus({ status: 'error', message: 'Erro ao conectar ao backend.' });
    }
  };

  const handleCreateIntegrationSetting = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setIntegrationStatus({ status: 'saving' });
    try {
      const response = await apiFetch('/api/v1/integrations', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: integrationName, type: selectedIntegrationTool, status: 'active' }),
      });
      if (response.ok) {
        setIntegrationStatus({ status: 'success', message: 'Integração ativada com sucesso!' });
        setIntegrationName('');
        fetchIntegrations();
        setTimeout(() => setIntegrationStatus({ status: 'idle' }), 3000);
      } else {
        const msg = await response.text();
        setIntegrationStatus({ status: 'error', message: msg || 'Falha ao ativar integração.' });
      }
    } catch (err) {
      setIntegrationStatus({ status: 'error', message: 'Erro de conectividade com a API.' });
    }
  };

  const handleDeleteIntegrationSetting = async (id: string) => {
    if (!token) return;
    if (!confirm('Deseja desativar esta integração para o tenant atual?')) return;
    try {
      const response = await apiFetch(`/api/v1/integrations?id=${id}`, { method: 'DELETE' });
      if (response.ok) {
        fetchIntegrations();
      } else {
        alert('Falha ao desativar integração.');
      }
    } catch (err) {
      console.error(err);
    }
  };

  const handleAdminCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    setAdminUserStatus({ status: 'saving' });
    try {
      // Admins see every tenant regardless, so tenant bindings only matter for operator/viewer.
      const tenantIds = adminUserRole === 'admin' ? [] : adminUserTenantIds;
      const response = await apiFetch('/api/v1/admin/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: adminUserEmail, password: adminUserPassword, name: adminUserName, role: adminUserRole, tenant_ids: tenantIds }),
      });
      if (response.ok) {
        setAdminUserStatus({ status: 'success', message: 'Novo usuário cadastrado e e-mail enviado!' });
        setAdminUserEmail('');
        setAdminUserPassword('');
        setAdminUserName('');
        setAdminUserTenantIds([]);
        fetchAdminUsers();
      } else {
        const msg = await response.text();
        setAdminUserStatus({ status: 'error', message: msg || 'Falha ao cadastrar usuário.' });
      }
    } catch (err) {
      setAdminUserStatus({ status: 'error', message: 'Erro ao conectar ao backend.' });
    }
  };

  const handleValidateIntegration = async (type: string) => {
    if (!token || !selectedTenant) return;
    setIsValidating(true);
    setValidationResult(null);
    try {
      const response = await apiFetch(`/api/v1/integrations/status?tenant_id=${selectedTenant.id}&type=${type}`);
      if (response.ok) {
        setValidationResult(await response.json());
      } else {
        setValidationResult({ status: 'error', last_error: 'Falha ao buscar status de conectividade.' });
      }
    } catch (err) {
      console.error(err);
      setValidationResult({ status: 'error', last_error: 'Erro de rede ao conectar à API de validação.' });
    } finally {
      setIsValidating(false);
    }
  };

  const fetchAllConnectorStatuses = async (tenantId: string) => {
    if (!token || !tenantId) return;
    const tools = ['uptimekuma', 'zabbix', 'prometheus', 'wazuh', 'grafana', 'sentinel', 'loki'];
    const results: Record<string, any> = {};
    await Promise.all(
      tools.map(async (tool) => {
        try {
          const res = await apiFetch(`/api/v1/integrations/status?tenant_id=${tenantId}&type=${tool}`);
          results[tool] = res.ok ? await res.json() : { status: 'error', last_error: 'Falha ao conectar à API de status.' };
        } catch (err) {
          console.error(`Error fetching status for ${tool}:`, err);
          results[tool] = { status: 'error', last_error: 'Erro de rede.' };
        }
      })
    );
    setConnectorStatuses(results);
  };

  const fetchPlaybooksAdmin = async () => {
    if (!token || !selectedTenant) return;
    setIsLoadingSettingsPlaybooks(true);
    try {
      const qTenant = playbooksFilter === 'all' ? 'all' : selectedTenant.id;
      const res = await apiFetch(`/api/v1/runbooks?tenant_id=${qTenant}`);
      if (res.ok) {
        setSettingsPlaybooks((await res.json()) || []);
      }
    } catch (err) {
      console.error('Failed to fetch settings playbooks:', err);
    } finally {
      setIsLoadingSettingsPlaybooks(false);
    }
  };

  const handleCreatePlaybook = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token || !selectedTenant) return;
    setPlaybookStatus({ status: 'saving' });
    try {
      const targetUrlTenant = playbookTargetTenantId === 'all' ? selectedTenant.id : playbookTargetTenantId;
      const res = await apiFetch(`/api/v1/runbooks?tenant_id=${targetUrlTenant}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: playbookName,
          trigger_rule: playbookTrigger,
          script: playbookScript,
          vault_key_host: playbookVaultKey,
          is_global: playbookTargetTenantId === 'all',
        }),
      });
      if (res.ok) {
        setPlaybookStatus({ status: 'success', message: 'Playbook criado com sucesso!' });
        setPlaybookName('');
        setPlaybookTrigger('');
        setPlaybookScript('');
        setPlaybookTargetTenantId('all');
        fetchPlaybooksAdmin();
      } else {
        const txt = await res.text();
        setPlaybookStatus({ status: 'error', message: txt || 'Falha ao criar playbook.' });
      }
    } catch (err) {
      setPlaybookStatus({ status: 'error', message: 'Erro ao conectar ao backend.' });
    }
  };

  const handleDeletePlaybook = async (id: string) => {
    if (!token || !selectedTenant) return;
    if (!confirm('Deseja realmente excluir este playbook de auto-cura?')) return;
    try {
      const res = await apiFetch(`/api/v1/runbooks?tenant_id=${selectedTenant.id}&id=${id}`, { method: 'DELETE' });
      if (res.ok) {
        fetchPlaybooksAdmin();
      } else {
        alert('Falha ao excluir playbook: ' + (await res.text()));
      }
    } catch (err) {
      alert('Erro ao excluir playbook: ' + err);
    }
  };

  const handleSaveVaultSecret = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!vaultValue || !selectedTenant) return;
    setSaveStatus({ status: 'saving' });
    try {
      const response = await apiFetch(`/api/v1/vault/secret?tenant_id=${selectedTenant.id}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key: vaultKey, value: vaultValue }),
      });
      if (response.ok) {
        setSaveStatus({ status: 'success', message: 'Credencial salva e encriptada com sucesso!' });
        setVaultValue('');
        setTimeout(() => setSaveStatus({ status: 'idle' }), 3000);
      } else {
        setSaveStatus({ status: 'error', message: 'Erro ao persistir a credencial no Vault.' });
      }
    } catch (err) {
      setSaveStatus({ status: 'error', message: 'Erro de conectividade com o backend Go.' });
    }
  };

  const handleCopyWebhookUrl = (url: string) => {
    navigator.clipboard.writeText(url);
    setCopiedText(true);
    setTimeout(() => setCopiedText(false), 2000);
  };

  useEffect(() => {
    setValidationResult(null);
  }, [selectedIntegrationTool, selectedTenant]);

  useEffect(() => {
    if (selectedIntegrationTool === 'users_admin') {
      fetchAdminUsers();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedIntegrationTool, token]);

  useEffect(() => {
    if (token) {
      fetchIntegrations();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, selectedTenant]);

  useEffect(() => {
    if (token && selectedTenant?.id && selectedIntegrationTool === 'integrations_admin') {
      fetchAllConnectorStatuses(selectedTenant.id);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, selectedTenant?.id, selectedIntegrationTool]);

  useEffect(() => {
    if (!token || !selectedTenant || selectedIntegrationTool !== 'sla_report') return;
    const fetchSlaData = async () => {
      setIsLoadingSla(true);
      try {
        const res = await apiFetch(`/api/v1/reports/sla/stats?tenant_id=${selectedTenant.id}`);
        if (res.ok) {
          setSlaData(await res.json());
        }
      } catch (err) {
        console.error('Failed to fetch SLA data:', err);
      } finally {
        setIsLoadingSla(false);
      }
    };
    fetchSlaData();
  }, [selectedTenant, selectedIntegrationTool, token]);

  useEffect(() => {
    if (!token || !selectedTenant || selectedIntegrationTool !== 'vault_admin') return;
    const fetchVaultSecrets = async () => {
      setIsLoadingVaultSecrets(true);
      try {
        const res = await apiFetch(`/api/v1/vault/list?tenant_id=${selectedTenant.id}`);
        if (res.ok) {
          setVaultSecrets((await res.json()) || []);
        }
      } catch (err) {
        console.error('Failed to fetch vault secrets:', err);
      } finally {
        setIsLoadingVaultSecrets(false);
      }
    };
    fetchVaultSecrets();
  }, [selectedTenant, selectedIntegrationTool, token]);

  useEffect(() => {
    if (!token || !selectedTenant || selectedIntegrationTool !== 'audit_admin') return;
    const fetchRunbookAudits = async () => {
      setIsLoadingRunbookAudits(true);
      try {
        const res = await apiFetch(`/api/v1/runbooks/audit?tenant_id=${selectedTenant.id}`);
        if (res.ok) {
          setRunbookAudits((await res.json()) || []);
        }
      } catch (err) {
        console.error('Failed to fetch runbook audits:', err);
      } finally {
        setIsLoadingRunbookAudits(false);
      }
    };
    fetchRunbookAudits();
  }, [selectedTenant, selectedIntegrationTool, token]);

  useEffect(() => {
    if (!token || !selectedTenant || selectedIntegrationTool !== 'playbooks_admin') return;
    fetchPlaybooksAdmin();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedTenant, selectedIntegrationTool, token, playbooksFilter]);

  return (
    <>
      {cockpitTab === 'topology' && (
            <TopologyView
              tenantId={selectedTenant?.id}
              onSearchTermChange={onSearchTermChange}
              onConfigureDiscovery={() => {
                setSelectedIntegrationTool('network_discovery');
                onNavigateSettings?.();
              }}
            />
      )}
      {cockpitTab === 'settings' && (
            // Unified Settings & Administration Split-Pane
            <div className="glass-card rounded-xl overflow-hidden flex flex-row border border-white/5 bg-surface/30 h-[700px] w-full">
              {/* Settings Sidebar */}
              <div className="w-[240px] bg-[#070b13]/80 border-r border-white/5 overflow-y-auto flex flex-col p-4 gap-1 select-none shrink-0">
                <span className="text-[9px] font-bold text-slate-500 uppercase tracking-widest px-2.5 py-2">Integrações & Conectores</span>
                
                <button
                  onClick={() => {
                    setSelectedIntegrationTool('integrations_admin');
                    setActiveSubTool(null);
                    setValidationResult(null);
                  }}
                  className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                    selectedIntegrationTool === 'integrations_admin' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                  }`}
                >
                  <Activity className="w-3.5 h-3.5 text-cyan-400" />
                  <span>Central de Conectores</span>
                </button>

                <span className="text-[9px] font-bold text-slate-500 uppercase tracking-widest px-2.5 py-2 mt-4">Personalização</span>
                <button
                  onClick={() => {
                    setSelectedIntegrationTool('theme_config');
                    setValidationResult(null);
                  }}
                  className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                    selectedIntegrationTool === 'theme_config' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                  }`}
                >
                  <Palette className="w-3.5 h-3.5 text-purple-400" />
                  <span>Identidade & White-Label</span>
                </button>

                <span className="text-[9px] font-bold text-slate-500 uppercase tracking-widest px-2.5 py-2 mt-4">Desempenho</span>
                <button
                  onClick={() => {
                    setSelectedIntegrationTool('sla_report');
                    setValidationResult(null);
                  }}
                  className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                    selectedIntegrationTool === 'sla_report' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                  }`}
                >
                  <TrendingUp className="w-3.5 h-3.5 text-emerald-400" />
                  <span>Métricas & SLA</span>
                </button>
                <button
                  onClick={() => {
                    setSelectedIntegrationTool('operational_kpis');
                    setValidationResult(null);
                  }}
                  className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                    selectedIntegrationTool === 'operational_kpis' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                  }`}
                >
                  <Activity className="w-3.5 h-3.5 text-cyan-400" />
                  <span>KPIs Operacionais</span>
                </button>

                <span className="text-[9px] font-bold text-slate-500 uppercase tracking-widest px-2.5 py-2 mt-4">Auto-Healing</span>
                <button
                  onClick={() => {
                    setSelectedIntegrationTool('playbooks_admin');
                    setValidationResult(null);
                  }}
                  className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                    selectedIntegrationTool === 'playbooks_admin' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                  }`}
                >
                  <Cpu className="w-3.5 h-3.5 text-cyan-400" />
                  <span>Playbooks de Auto-Cura</span>
                </button>
                <button
                  onClick={() => {
                    setSelectedIntegrationTool('approvals_admin');
                    setValidationResult(null);
                  }}
                  className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                    selectedIntegrationTool === 'approvals_admin' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                  }`}
                >
                  <ShieldAlert className="w-3.5 h-3.5 text-amber-400" />
                  <span>Aprovações Pendentes</span>
                </button>
                <button
                  onClick={() => {
                    setSelectedIntegrationTool('response_actions');
                    setValidationResult(null);
                  }}
                  className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                    selectedIntegrationTool === 'response_actions' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                  }`}
                >
                  <Ban className="w-3.5 h-3.5 text-rose-400" />
                  <span>Fila de Contenção</span>
                </button>
                <button
                  onClick={() => {
                    setSelectedIntegrationTool('network_discovery');
                    setValidationResult(null);
                  }}
                  className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                    selectedIntegrationTool === 'network_discovery' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                  }`}
                >
                  <Radar className="w-3.5 h-3.5 text-cyan-400" />
                  <span>Descoberta de Rede</span>
                </button>
                <button
                  onClick={() => {
                    setSelectedIntegrationTool('assets');
                    setValidationResult(null);
                  }}
                  className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                    selectedIntegrationTool === 'assets' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                  }`}
                >
                  <Boxes className="w-3.5 h-3.5 text-cyan-400" />
                  <span>CMDB &amp; Ativos</span>
                </button>

                {user?.role === 'admin' && (
                  <>
                    <span className="text-[9px] font-bold text-slate-500 uppercase tracking-widest px-2.5 py-2 mt-4">Administração NOC</span>
                    <button
                      onClick={() => {
                        setSelectedIntegrationTool('users_admin');
                        setAdminUserStatus({ status: 'idle' });
                        setValidationResult(null);
                      }}
                      className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                        selectedIntegrationTool === 'users_admin' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                      }`}
                    >
                      <User className="w-3.5 h-3.5 text-violet-400" />
                      <span>Usuários (RBAC)</span>
                    </button>
                    <button
                      onClick={() => {
                        setSelectedIntegrationTool('suppression');
                        setValidationResult(null);
                      }}
                      className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                        selectedIntegrationTool === 'suppression' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                      }`}
                    >
                      <BellOff className="w-3.5 h-3.5 text-amber-400" />
                      <span>Regras de Supressão</span>
                    </button>
                    <button
                      onClick={() => {
                        setSelectedIntegrationTool('access_admin');
                        setValidationResult(null);
                      }}
                      className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                        selectedIntegrationTool === 'access_admin' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                      }`}
                    >
                      <ShieldCheck className="w-3.5 h-3.5 text-emerald-400" />
                      <span>Segurança de Acessos</span>
                    </button>
                    <button
                      onClick={() => {
                        setSelectedIntegrationTool('tenants_admin');
                        setTenantCreateStatus({ status: 'idle' });
                        setValidationResult(null);
                      }}
                      className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                        selectedIntegrationTool === 'tenants_admin' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                      }`}
                    >
                      <Layers className="w-3.5 h-3.5 text-blue-400" />
                      <span>Clientes & Tenants</span>
                    </button>
                    <button
                      onClick={() => {
                        setSelectedIntegrationTool('vault_admin');
                        setValidationResult(null);
                      }}
                      className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                        selectedIntegrationTool === 'vault_admin' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                      }`}
                    >
                      <Lock className="w-3.5 h-3.5 text-amber-400" />
                      <span>Auditoria do Vault</span>
                    </button>
                    <button
                      onClick={() => {
                        setSelectedIntegrationTool('audit_admin');
                        setValidationResult(null);
                      }}
                      className={`w-full px-3 py-2 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                        selectedIntegrationTool === 'audit_admin' ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                      }`}
                    >
                      <Terminal className="w-3.5 h-3.5 text-slate-400" />
                      <span>Logs de Comandos SSH</span>
                    </button>
                  </>
                )}
              </div>

              {/* Settings Right Panel Content */}
              <div className="flex-1 p-6 overflow-y-auto flex flex-col gap-6 bg-[#080d16]">
                {selectedIntegrationTool === 'theme_config' ? (
                  // White-label Configuration Panel
                  <div className="flex flex-col gap-4">
                    <div className="flex flex-col gap-1 border-b border-white/5 pb-4 mb-2">
                      <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider">Painel de Configuração de White-Label & Temas</h4>
                      <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">Customize a identidade visual do cockpit para seu inquilino (Parceria IT Fácil MSP)</p>
                    </div>

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-8 text-xs text-slate-300">
                      <div className="flex flex-col gap-4">
                        <div className="flex flex-col gap-2">
                          <label className="font-bold text-slate-400 uppercase tracking-wider text-[9px]">URL do Logotipo customizado (SVG/PNG)</label>
                          <input
                            type="text"
                            className="bg-slate-950 border border-white/10 rounded-lg p-3 text-xs text-white focus:outline-none focus:border-violet-500 font-mono"
                            value={selectedTenant?.logo_url || ''}
                            onChange={(e) => {
                              if (selectedTenant) {
                                const updated = [...tenants];
                                const t = updated.find(x => x.id === selectedTenant.id);
                                if (t) t.logo_url = e.target.value;
                                setTenants(updated);
                              }
                            }}
                            placeholder="https://exemplo.com/logo.png"
                          />
                        </div>

                        <div className="flex flex-col gap-2">
                          <label className="font-bold text-slate-400 uppercase tracking-wider text-[9px]">Cor Primária do Tema (Hexadecimal)</label>
                          <div className="flex gap-3 items-center">
                            <input
                              type="color"
                              className="bg-transparent border-0 w-10 h-10 cursor-pointer"
                              value={selectedTenant?.primary_color || '#8b5cf6'}
                              onChange={(e) => {
                                if (selectedTenant) {
                                  const updated = [...tenants];
                                  const t = updated.find(x => x.id === selectedTenant.id);
                                  if (t) {
                                    t.primary_color = e.target.value;
                                    document.documentElement.style.setProperty('--primary-color', e.target.value);
                                  }
                                  setTenants(updated);
                                }
                              }}
                            />
                            <input
                              type="text"
                              className="bg-slate-950 border border-white/10 rounded-lg p-3 text-xs text-white focus:outline-none focus:border-violet-500 font-mono w-28 text-center"
                              value={selectedTenant?.primary_color || '#8b5cf6'}
                              onChange={(e) => {
                                if (selectedTenant) {
                                  const updated = [...tenants];
                                  const t = updated.find(x => x.id === selectedTenant.id);
                                  if (t) {
                                    t.primary_color = e.target.value;
                                    document.documentElement.style.setProperty('--primary-color', e.target.value);
                                  }
                                  setTenants(updated);
                                }
                              }}
                            />
                          </div>
                        </div>

                        <button
                          onClick={async () => {
                            if (!selectedTenant || !token) return;
                            try {
                              const res = await fetch(`${API_BASE_URL}/api/v1/tenants/update_style`, {
                                method: 'POST',
                                headers: {
                                  'Content-Type': 'application/json',
                                  'Authorization': `Bearer ${token}`
                                },
                                body: JSON.stringify({
                                  tenant_id: selectedTenant.id,
                                  logo_url: selectedTenant.logo_url,
                                  primary_color: selectedTenant.primary_color
                                })
                              });
                              if (res.ok) {
                                alert("Identidade visual White-Label atualizada com sucesso!");
                              } else {
                                const txt = await res.text();
                                alert("Falha ao salvar: " + txt);
                              }
                            } catch (err) {
                              alert("Erro ao conectar à API: " + err);
                            }
                          }}
                          className="py-3 px-6 rounded-xl bg-violet-600 hover:bg-violet-500 text-slate-950 font-extrabold uppercase tracking-wider text-[10px] transition-all cursor-pointer w-fit"
                        >
                          Salvar Identidade Visual
                        </button>
                      </div>

                      <div className="p-4 rounded-xl bg-slate-950/40 border border-white/5 flex flex-col gap-3 justify-center">
                        <h5 className="font-extrabold uppercase text-[10px] text-violet-400">💡 Demonstração White-Label</h5>
                        <p className="text-slate-400 leading-relaxed">
                          Nossa plataforma permite a customização de cores, marcas e logos de forma isolada por domínio. Ao alterar o logotipo e cor acima, os estilos são gravados no banco de dados e aplicados em tempo de execução ao cabeçalho e menus sempre que este cliente estiver selecionado.
                        </p>
                      </div>
                    </div>
                  </div>
                ) : selectedIntegrationTool === 'integrations_admin' ? (
                  // Connectors Grid Panel
                    <div className="flex flex-col gap-5">
                      <div className="flex items-center justify-between border-b border-white/5 pb-4">
                        <div className="flex flex-col gap-0.5">
                          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider">Central de Conectores & Integrações</h4>
                          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">Monitoramento de batimento (heartbeat) de telemetria em tempo real para o cliente: <strong className="text-violet-400">{selectedTenant?.name}</strong></p>
                        </div>
                        <button
                          onClick={() => selectedTenant && fetchAllConnectorStatuses(selectedTenant.id)}
                          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 text-xs text-slate-300 font-bold transition-all cursor-pointer"
                        >
                          <RefreshCw className="w-3.5 h-3.5" />
                          <span>Atualizar Status</span>
                        </button>
                      </div>

                      {/* Client selector dropdown for admin */}
                      {user?.role === 'admin' && (
                        <div className="p-4 rounded-xl bg-white/[0.02] border border-white/5 flex flex-col md:flex-row items-center gap-4 justify-between">
                          <div className="flex flex-col gap-1">
                            <span className="text-xs font-bold text-slate-300">Alterar Cliente para Configuração:</span>
                            <span className="text-[10px] text-slate-500">As chaves e webhooks abaixo serão relativos a este cliente.</span>
                          </div>
                          <select
                            value={selectedTenant?.id || ''}
                            onChange={(e) => {
                              const t = tenants.find(x => x.id === e.target.value);
                              if (t) {
                                setSelectedTenantIds([t.id]);
                              }
                            }}
                            className="bg-[#0b0f19] border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-violet-500 w-64"
                          >
                            {tenants.map(t => (
                              <option key={t.id} value={t.id}>{t.name}</option>
                            ))}
                          </select>
                        </div>
                      )}

                      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-5">
                        {[
                          { id: 'zabbix', name: 'Zabbix Monitor', method: 'Webhook (Push)', desc: 'Recepção de incidentes de infraestrutura, hosts e serviços.', color: 'border-rose-500/20' },
                          { id: 'prometheus', name: 'Prometheus Alertmanager', method: 'Webhook (Push)', desc: 'Alertas de Kubernetes, contêineres e aplicações cloud-native.', color: 'border-purple-500/20' },
                          { id: 'uptimekuma', name: 'Uptime Kuma', method: 'Webhook (Push)', desc: 'Notificações de queda de sites, portas TCP e ping ICMP.', color: 'border-emerald-500/20' },
                          { id: 'wazuh', name: 'Wazuh SIEM', method: 'Webhook (Push)', desc: 'Eventos de segurança, auditoria e ameaças de endpoints.', color: 'border-blue-500/20' },
                          { id: 'grafana', name: 'Grafana Webhook', method: 'Webhook (Push)', desc: 'Ingestão de triggers visuais baseados em painéis de métricas.', color: 'border-violet-500/20' },
                          { id: 'sentinel', name: 'Microsoft Sentinel', method: 'API Polling (Pull)', desc: 'Varredura ativa de incidentes de segurança na nuvem do Azure.', color: 'border-cyan-500/20' },
                          { id: 'loki', name: 'Grafana Loki', method: 'API Polling (Pull)', desc: 'Leitura de logs distribuídos com inteligência AIOps em tempo real.', color: 'border-orange-500/20' },
                          { id: 'slack', name: 'Slack', method: 'Escalonamento (Outbound)', desc: 'Notifica um canal via webhook quando um alerta crítico/fatal dispara.', color: 'border-fuchsia-500/20' },
                          { id: 'teams', name: 'Microsoft Teams', method: 'Escalonamento (Outbound)', desc: 'Notifica um canal via webhook quando um alerta crítico/fatal dispara.', color: 'border-indigo-600/20' },
                          { id: 'email', name: 'E-mail (SMTP)', method: 'Escalonamento (Outbound)', desc: 'Envia um e-mail de alerta crítico/fatal via SMTP da plataforma.', color: 'border-slate-400/20' },
                        ].map(tool => {
                          const statusData = connectorStatuses[tool.id] || { status: 'inactive' };
                          const isPush = tool.method.includes('Push');
                          const activeConns = isPush 
                            ? (integrations || []).filter(i => i.type === tool.id).length 
                            : (connectorStatuses[tool.id]?.status === 'active' ? 1 : 0);

                          return (
                            <div 
                              key={tool.id} 
                              className={`glass-card p-5 rounded-xl border flex flex-col gap-4 hover:scale-[1.02] hover:border-white/10 active:scale-[0.98] transition-all bg-[#0d1220]/45 ${tool.color}`}
                            >
                              <div className="flex justify-between items-start">
                                <div className="flex flex-col gap-0.5">
                                  <span className="text-xs font-bold text-slate-100 uppercase tracking-wide">{tool.name}</span>
                                  <span className="text-[9px] font-semibold text-slate-500 uppercase tracking-wider">{tool.method}</span>
                                </div>

                                {/* Status badge */}
                                {statusData.status === 'active' ? (
                                  <span className="px-1.5 py-0.5 rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-bold uppercase text-[8px] tracking-wider">Ativo</span>
                                ) : statusData.status === 'offline' ? (
                                  <span className="px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-400 border border-amber-500/20 font-bold uppercase text-[8px] tracking-wider">Aviso</span>
                                ) : statusData.status === 'error' ? (
                                  <span className="px-1.5 py-0.5 rounded bg-rose-500/10 text-rose-400 border border-rose-500/20 font-bold uppercase text-[8px] tracking-wider">Falha</span>
                                ) : (
                                  <span className="px-1.5 py-0.5 rounded bg-slate-500/10 text-slate-505 border border-slate-500/20 font-bold uppercase text-[8px] tracking-wider">Inativo</span>
                                )}
                              </div>

                              <p className="text-[10px] text-slate-400 leading-relaxed font-sans min-h-[30px]">{tool.desc}</p>

                              <div className="flex items-center justify-between border-t border-white/5 pt-3.5 text-[9px] font-bold text-slate-500 uppercase tracking-wider">
                                <span>Conexões: <strong className="text-slate-300">{activeConns}</strong></span>
                                {statusData.last_seen > 0 && (
                                  <span>Visto: <strong className="text-slate-300 font-mono">{new Date(statusData.last_seen * 1000).toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'})}</strong></span>
                                )}
                              </div>

                              <button
                                onClick={() => {
                                  setSelectedIntegrationTool(tool.id);
                                  setActiveSubTool(tool.id);
                                  setSaveStatus({ status: 'idle' });
                                  if (tool.id === 'sentinel') setVaultKey('sentinel_client_secret');
                                  else if (tool.id === 'loki') setVaultKey('loki_password');
                                  else if (tool.id === 'ssh') setVaultKey('ssh_private_key');
                                  else if (tool.id === 'slack') setVaultKey('slack_webhook_url');
                                  else if (tool.id === 'teams') setVaultKey('teams_webhook_url');
                                  else if (tool.id === 'email') setVaultKey('email_recipient');
                                  handleValidateIntegration(tool.id);
                                }}
                                className="w-full py-2 bg-white/5 hover:bg-cyan-500/15 hover:text-cyan-400 text-slate-300 font-extrabold uppercase tracking-widest text-[9px] rounded-lg transition-all border border-white/5 hover:border-cyan-500/30 cursor-pointer text-center"
                              >
                                Configurar Conector
                              </button>
                            </div>
                          );
                        })}
                      </div>
                    </div>
                ) : ['uptimekuma', 'zabbix', 'prometheus', 'wazuh', 'grafana', 'sentinel', 'loki', 'slack', 'teams', 'email'].includes(selectedIntegrationTool) ? (
                    // Detail panel for a conector
                    <div className="flex flex-col gap-4">
                      {/* Back button */}
                      <button
                        onClick={() => {
                          setSelectedIntegrationTool('integrations_admin');
                          setValidationResult(null);
                        }}
                        className="flex items-center gap-1 text-[10px] font-bold uppercase tracking-wider text-slate-400 hover:text-slate-200 transition-all cursor-pointer w-fit mb-2"
                      >
                        ← Voltar para a Central de Conectores
                      </button>

                      <div className="flex items-center gap-3 border-b border-white/5 pb-4 mb-2">
                        <div className="w-10 h-10 rounded-lg bg-cyan-950/20 border border-cyan-500/20 flex items-center justify-center text-cyan-400">
                          <HelpCircle className="w-6 h-6" />
                        </div>
                        <div>
                          <h4 className="text-sm font-bold uppercase text-white tracking-wide">
                            {selectedIntegrationTool === 'uptimekuma' && 'Configuração Uptime Kuma'}
                            {selectedIntegrationTool === 'zabbix' && 'Configuração Zabbix Monitor'}
                            {selectedIntegrationTool === 'prometheus' && 'Configuração Prometheus Alertmanager'}
                            {selectedIntegrationTool === 'wazuh' && 'Configuração Wazuh SIEM'}
                            {selectedIntegrationTool === 'grafana' && 'Configuração Grafana Alerts'}
                            {selectedIntegrationTool === 'sentinel' && 'Configuração Microsoft Sentinel'}
                            {selectedIntegrationTool === 'loki' && 'Configuração Grafana Loki'}
                            {selectedIntegrationTool === 'slack' && 'Configuração Slack'}
                            {selectedIntegrationTool === 'teams' && 'Configuração Microsoft Teams'}
                            {selectedIntegrationTool === 'email' && 'Configuração E-mail (SMTP)'}
                          </h4>
                          <p className="text-[10px] text-slate-500 uppercase tracking-widest font-bold">
                            {['uptimekuma', 'zabbix', 'prometheus', 'wazuh', 'grafana'].includes(selectedIntegrationTool)
                              ? 'Método: Webhook (Push / Envio de Alertas)'
                              : ['slack', 'teams', 'email'].includes(selectedIntegrationTool)
                                ? 'Método: Escalonamento (Outbound / Somente Envio)'
                                : 'Método: API Polling (Pull / Busca Ativa de Chaves)'}
                          </p>
                        </div>
                      </div>

                      {/* Push webhook forms */}
                      {['uptimekuma', 'zabbix', 'prometheus', 'wazuh', 'grafana'].includes(selectedIntegrationTool) ? (
                        <div className="flex flex-col gap-4">
                          {/* Active Integrations list */}
                          <div className="flex flex-col gap-2.5">
                            <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">
                              Integrações Ativas ({selectedTenant?.name})
                            </label>
                            
                            {(integrations || []).filter(i => i.type === selectedIntegrationTool).length > 0 ? (
                              <div className="flex flex-col gap-2 max-h-[150px] overflow-y-auto pr-1">
                                {(integrations || []).filter(i => i.type === selectedIntegrationTool).map(item => (
                                  <div key={item.id} className="p-3 rounded-lg bg-[#040811] border border-white/5 flex items-center justify-between font-sans text-xs">
                                    <div className="flex flex-col gap-0.5">
                                      <span className="font-bold text-slate-200">{item.name}</span>
                                      <span className="text-[9px] font-mono text-cyan-400 select-all leading-none mt-1">
                                        {`${API_BASE_URL}/api/v1/webhook/${selectedIntegrationTool}/${selectedTenant?.id}`}
                                      </span>
                                    </div>
                                    <div className="flex items-center gap-2 shrink-0 ml-4">
                                      <button
                                        onClick={() => selectedTenant && handleCopyWebhookUrl(`${API_BASE_URL}/api/v1/webhook/${selectedIntegrationTool}/${selectedTenant.id}`)}
                                        className="p-1.5 rounded bg-white/5 hover:bg-white/10 text-slate-400 hover:text-white transition-all"
                                        title="Copiar URL de Ingestão"
                                      >
                                        {copiedText ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
                                      </button>
                                      {user?.role === 'admin' && (
                                        <button
                                          onClick={() => handleDeleteIntegrationSetting(item.id)}
                                          className="p-1.5 rounded bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 transition-all font-bold text-[10px]"
                                          title="Desativar Integração"
                                        >
                                          Remover
                                        </button>
                                      )}
                                    </div>
                                  </div>
                                ))}
                              </div>
                            ) : (
                              <div className="p-3 rounded-lg bg-amber-950/10 border border-amber-500/10 text-amber-400 text-xs font-sans">
                                Nenhuma integração deste tipo está ativa para o Tenant atual. Ative abaixo para liberar a recepção de alertas.
                              </div>
                            )}
                          </div>

                          {/* Admin Integration Activation Form */}
                          {user?.role === 'admin' && (
                            <form onSubmit={handleCreateIntegrationSetting} className="p-4 rounded-xl bg-white/[0.02] border border-white/5 flex flex-col gap-3">
                              <h5 className="text-[10px] font-bold uppercase tracking-wider text-slate-200">Ativar Nova Integração</h5>
                              <div className="flex gap-2">
                                <input
                                  type="text"
                                  required
                                  value={integrationName}
                                  onChange={(e) => setIntegrationName(e.target.value)}
                                  placeholder="Nome identificador (Ex: Zabbix Produção)"
                                  className="flex-1 bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500 transition-all placeholder:text-slate-600"
                                />
                                <button
                                  type="submit"
                                  disabled={integrationStatus.status === 'saving'}
                                  className="bg-cyan-600 hover:bg-cyan-500 text-slate-950 font-bold uppercase tracking-wider text-[10px] px-5 rounded-lg transition-all flex items-center gap-1.5 cursor-pointer shrink-0"
                                >
                                  {integrationStatus.status === 'saving' && <RefreshCw className="w-3.5 h-3.5 animate-spin" />}
                                  Ativar Ingestão
                                </button>
                              </div>
                              {integrationStatus.status === 'success' && (
                                <span className="text-[10px] text-emerald-400">{integrationStatus.message}</span>
                              )}
                              {integrationStatus.status === 'error' && (
                                <span className="text-[10px] text-rose-400">{integrationStatus.message}</span>
                              )}
                            </form>
                          )}

                          {/* Unified Validation Box */}
                          <div className="p-3 rounded-lg bg-surface/30 border border-white/5 flex flex-col gap-2.5 mt-2">
                            <div className="flex items-center justify-between">
                              <span className="text-[10px] uppercase font-bold tracking-wider text-slate-400">
                                Validação de Comunicação / Logs de Entrada
                              </span>
                              <button
                                type="button"
                                onClick={() => handleValidateIntegration(selectedIntegrationTool)}
                                disabled={isValidating}
                                className="px-2.5 py-1 rounded bg-rose-500/10 hover:bg-rose-500/20 disabled:bg-white/5 text-rose-400 disabled:text-slate-500 border border-rose-500/25 disabled:border-transparent transition-all text-[10px] font-bold cursor-pointer"
                              >
                                {isValidating ? 'Validando...' : 'Testar Conexão / Logs'}
                              </button>
                            </div>

                            {validationResult && (
                              <div className="flex flex-col gap-2 font-sans text-xs">
                                <div className="flex items-center gap-1.5">
                                  <span className="text-slate-400">Status do Conector:</span>
                                  {validationResult.status === 'active' ? (
                                    <span className="px-1.5 py-0.5 rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-bold uppercase text-[9px]">Ativo (Online)</span>
                                  ) : validationResult.status === 'offline' ? (
                                    <span className="px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-400 border border-amber-500/20 font-bold uppercase text-[9px]">Offline (Sem Telemetria)</span>
                                  ) : (
                                    <span className="px-1.5 py-0.5 rounded bg-slate-500/10 text-slate-400 border border-slate-500/20 font-bold uppercase text-[9px]">Inativo (Sem Sinal)</span>
                                  )}
                                </div>
                                {validationResult.last_seen > 0 && (
                                  <div className="text-[10px] text-slate-500 leading-none">
                                    Último sinal recebido: {new Date(validationResult.last_seen * 1000).toLocaleString('pt-BR')}
                                  </div>
                                )}
                                <div className="flex flex-col gap-1 mt-1">
                                  <span className="text-slate-400 font-semibold">Verbose / Logs de Erro do Webhook:</span>
                                  {validationResult.last_error ? (
                                    <pre className="p-2.5 rounded bg-red-950/15 border border-red-500/20 text-[10px] text-red-400 font-mono overflow-x-auto max-h-[100px] whitespace-pre-wrap leading-tight">
                                      {validationResult.last_error}
                                    </pre>
                                  ) : (
                                    <p className="text-[10px] text-emerald-400 font-semibold bg-emerald-500/5 p-2 rounded border border-emerald-500/15">
                                      ✓ Nenhuma falha pendente. Integração operando de forma limpa.
                                    </p>
                                  )}
                                </div>
                              </div>
                            )}
                          </div>
                        </div>
                      ) : ['sentinel', 'loki', 'ssh', 'slack', 'teams', 'email'].includes(selectedIntegrationTool) ? (
                        // Secure Vault Pull Connectors Form
                        <form onSubmit={handleSaveVaultSecret} className="flex flex-col gap-4">
                          <div className="flex flex-col gap-3 p-4 rounded-xl bg-cyan-950/10 border border-cyan-500/20 text-xs text-slate-300 leading-relaxed font-sans mb-2">
                            <div className="flex items-center gap-1.5 text-cyan-400 font-extrabold uppercase text-[10px]">
                              <Lock className="w-3.5 h-3.5" /> Cofre Criptográfico RLS Seguro
                            </div>
                            <p>Estas credenciais são salvas e encriptadas usando algoritmos robustos de **AES-256-GCM** na tabela `tenant_vault`. Graças à segurança estrita por nível de linha (RLS) no PostgreSQL, estes segredos são 100% invisíveis a qualquer outro tenant.</p>
                          </div>

                          <div className="grid grid-cols-2 gap-4">
                            <div className="flex flex-col gap-2">
                              <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Identificador da Credencial (Key)</label>
                              <select
                                value={vaultKey}
                                onChange={(e) => setVaultKey(e.target.value)}
                                className="bg-surface border border-white/5 rounded-lg p-2.5 text-xs text-slate-200 focus:outline-none focus:border-cyan-500/50"
                              >
                                {selectedIntegrationTool === 'sentinel' && (
                                  <>
                                    <option value="sentinel_client_secret">Client Secret (Azure API)</option>
                                    <option value="sentinel_client_id">Client ID (App Registration)</option>
                                    <option value="sentinel_tenant_id">Tenant ID (Azure Directory)</option>
                                    <option value="sentinel_subscription_id">Subscription ID</option>
                                  </>
                                )}
                                {selectedIntegrationTool === 'loki' && (
                                  <>
                                    <option value="loki_url">Loki Server URL</option>
                                    <option value="loki_username">Loki Username</option>
                                    <option value="loki_password">Loki Password</option>
                                  </>
                                )}
                                {selectedIntegrationTool === 'ssh' && (
                                  <>
                                    <option value="ssh_private_key">SSH Private Key (PEM format)</option>
                                    <option value="ssh_username">SSH Username</option>
                                    <option value="ssh_password">SSH Password (Fallback)</option>
                                  </>
                                )}
                                {selectedIntegrationTool === 'slack' && (
                                  <option value="slack_webhook_url">Incoming Webhook URL</option>
                                )}
                                {selectedIntegrationTool === 'teams' && (
                                  <option value="teams_webhook_url">Incoming Webhook URL</option>
                                )}
                                {selectedIntegrationTool === 'email' && (
                                  <option value="email_recipient">E-mail(s) do Destinatário</option>
                                )}
                              </select>
                            </div>

                            <div className="flex flex-col gap-2">
                              <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">
                                {selectedIntegrationTool === 'email' ? 'Valor (não confidencial)' : 'Valor da Credencial (Secret Value)'}
                              </label>
                              <input
                                type={selectedIntegrationTool === 'email' ? 'text' : 'password'}
                                required
                                value={vaultValue}
                                placeholder={selectedIntegrationTool === 'email' ? 'destinatario@empresa.com' : 'Digite ou cole o valor confidencial aqui...'}
                                onChange={(e) => setVaultValue(e.target.value)}
                                className="bg-surface border border-white/5 rounded-lg p-2.5 text-xs text-slate-200 focus:outline-none focus:border-cyan-500/50 placeholder:text-slate-600"
                              />
                            </div>
                          </div>

                          <button
                            type="submit"
                            disabled={saveStatus.status === 'saving'}
                            className="bg-cyan-600 hover:bg-cyan-500 disabled:bg-cyan-800 disabled:opacity-50 text-slate-950 font-bold uppercase tracking-wider text-xs py-2.5 rounded-lg flex items-center justify-center gap-2 transition-all mt-2 cursor-pointer"
                          >
                            {saveStatus.status === 'saving' ? (
                              <>
                                <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                                <span>Criptografando e Salvando...</span>
                              </>
                            ) : (
                              <>
                                <Lock className="w-3.5 h-3.5" />
                                <span>Salvar Segredo no Cofre do Banco</span>
                              </>
                            )}
                          </button>

                          {saveStatus.status === 'success' && (
                            <div className="p-3 rounded-lg bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 text-xs font-semibold text-center">
                              {saveStatus.message}
                            </div>
                          )}
                          {saveStatus.status === 'error' && (
                            <div className="p-3 rounded-lg bg-rose-500/10 border border-rose-500/20 text-rose-400 text-xs font-semibold text-center">
                              {saveStatus.message}
                            </div>
                          )}

                          <div className="flex flex-col gap-3 p-4 rounded-xl bg-slate-900/40 border border-white/5 text-xs text-slate-300 leading-relaxed font-sans mt-3">
                            <h5 className="font-bold text-slate-200 uppercase tracking-wider text-[10px] border-b border-white/5 pb-2">Instruções de Configuração e Uso:</h5>
                            {selectedIntegrationTool === 'sentinel' && (
                              <div className="flex flex-col gap-2">
                                <p>O conector do <b>Microsoft Sentinel</b> atua via busca ativa (Polling API) consultando logs e incidentes de segurança no Azure Log Analytics:</p>
                                <div className="flex flex-col gap-1.5 pl-3 border-l-2 border-cyan-500/50">
                                  <span>1. Registre um aplicativo (App Registration) no seu Azure Active Directory (Microsoft Entra ID).</span>
                                  <span>2. Atribua a função de **Log Analytics Reader** ou similar a este aplicativo.</span>
                                  <span>3. Salve as chaves obtidas (Client ID, Client Secret, Tenant ID e Subscription ID) separadamente neste cofre.</span>
                                  <span>4. O coletor rodará a cada 5 minutos buscando incidentes e normalizando as ameaças na fila do SOC da IT Fácil.</span>
                                </div>
                              </div>
                            )}

                            {selectedIntegrationTool === 'loki' && (
                              <div className="flex flex-col gap-2">
                                <p>A integração com o <b>Grafana Loki</b> permite coletar logs brutos em tempo real e processar inteligência AIOps:</p>
                                <div className="flex flex-col gap-1.5 pl-3 border-l-2 border-orange-500/50">
                                  <span>1. Insira a URL base de acesso à API do seu servidor Loki (ex: <code>https://loki.empresa.com.br</code>).</span>
                                  <span>2. Forneça o Usuário e Senha de autenticação básica (Basic Auth) se configurado.</span>
                                  <span>3. A IT Fácil buscará ativamente exceções de logs e normalizará strings de erro em eventos unificados.</span>
                                </div>
                              </div>
                            )}

                            {selectedIntegrationTool === 'slack' && (
                              <div className="flex flex-col gap-2">
                                <p>Envia uma notificação para um canal do <b>Slack</b> sempre que um alerta crítico ou fatal disparar:</p>
                                <div className="flex flex-col gap-1.5 pl-3 border-l-2 border-fuchsia-500/50">
                                  <span>1. No Slack, vá em Apps → Incoming Webhooks e crie um novo webhook para o canal desejado.</span>
                                  <span>2. Cole a URL gerada (formato <code>https://hooks.slack.com/services/...</code>) no campo ao lado.</span>
                                  <span>3. Alertas critical/fatal passam a ser enviados automaticamente para esse canal.</span>
                                </div>
                              </div>
                            )}

                            {selectedIntegrationTool === 'teams' && (
                              <div className="flex flex-col gap-2">
                                <p>Envia uma notificação para um canal do <b>Microsoft Teams</b> sempre que um alerta crítico ou fatal disparar:</p>
                                <div className="flex flex-col gap-1.5 pl-3 border-l-2 border-indigo-500/50">
                                  <span>1. No canal do Teams, adicione o conector "Incoming Webhook" e gere a URL.</span>
                                  <span>2. Cole a URL gerada no campo ao lado.</span>
                                  <span>3. Alertas critical/fatal passam a ser enviados automaticamente para esse canal.</span>
                                </div>
                              </div>
                            )}

                            {selectedIntegrationTool === 'email' && (
                              <div className="flex flex-col gap-2">
                                <p>Envia um e-mail sempre que um alerta crítico ou fatal disparar, usando o SMTP já configurado na plataforma:</p>
                                <div className="flex flex-col gap-1.5 pl-3 border-l-2 border-slate-400/50">
                                  <span>1. Informe o(s) e-mail(s) que devem receber os alertas deste cliente.</span>
                                  <span>2. Não é necessário configurar credenciais SMTP aqui — elas são globais da plataforma.</span>
                                  <span>3. Se o SMTP da plataforma não estiver configurado, o envio é apenas registrado em log (sem erro).</span>
                                </div>
                              </div>
                            )}
                          </div>

                          {/* Unified Validation Box */}
                          {['sentinel', 'loki'].includes(selectedIntegrationTool) && (
                            <div className="p-3 rounded-lg bg-surface/30 border border-white/5 flex flex-col gap-2.5 mt-2">
                              <div className="flex items-center justify-between">
                                <span className="text-[10px] uppercase font-bold tracking-wider text-slate-400">
                                  Validação de Comunicação / Teste de API
                                </span>
                                <button
                                  type="button"
                                  onClick={() => handleValidateIntegration(selectedIntegrationTool)}
                                  disabled={isValidating}
                                  className="px-2.5 py-1 rounded bg-rose-500/10 hover:bg-rose-500/20 disabled:bg-white/5 text-rose-400 disabled:text-slate-500 border border-rose-500/25 disabled:border-transparent transition-all text-[10px] font-bold cursor-pointer"
                                >
                                  {isValidating ? 'Validando...' : 'Testar Conexão / Logs'}
                                </button>
                              </div>

                              {validationResult && (
                                <div className="flex flex-col gap-2 font-sans text-xs font-sans">
                                  <div className="flex items-center gap-1.5">
                                    <span className="text-slate-400">Status do Conector:</span>
                                    {validationResult.status === 'active' ? (
                                      <span className="px-1.5 py-0.5 rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-bold uppercase text-[9px]">Ativo (Online)</span>
                                    ) : validationResult.status === 'offline' ? (
                                      <span className="px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-400 border border-amber-500/20 font-bold uppercase text-[9px]">Offline (Sem Telemetria)</span>
                                    ) : (
                                      <span className="px-1.5 py-0.5 rounded bg-slate-500/10 text-slate-400 border border-slate-500/20 font-bold uppercase text-[9px]">Inativo (Sem Sinal)</span>
                                    )}
                                  </div>
                                  {validationResult.last_seen > 0 && (
                                    <div className="text-[10px] text-slate-500 leading-none">
                                      Último sinal recebido: {new Date(validationResult.last_seen * 1000).toLocaleString('pt-BR')}
                                    </div>
                                  )}
                                  <div className="flex flex-col gap-1 mt-1 font-sans">
                                    <span className="text-slate-400 font-semibold">Verbose / Logs de Erro:</span>
                                    {validationResult.last_error ? (
                                      <pre className="p-2.5 rounded bg-red-950/15 border border-red-500/20 text-[10px] text-red-400 font-mono overflow-x-auto max-h-[100px] whitespace-pre-wrap leading-tight">
                                        {validationResult.last_error}
                                      </pre>
                                    ) : (
                                      <p className="text-[10px] text-emerald-400 font-semibold bg-emerald-500/5 p-2 rounded border border-emerald-500/15">
                                        ✓ Conexão bem-sucedida. Integração operando de forma limpa.
                                      </p>
                                    )}
                                  </div>
                                </div>
                              )}
                            </div>
                          )}
                        </form>
                      ) : null}
                    </div>
                ) : selectedIntegrationTool === 'playbooks_admin' ? (
                  <div className="flex flex-col gap-4 font-sans animate-fadeIn">
                    <div className="flex flex-col gap-3 p-4 rounded-xl bg-cyan-950/10 border border-cyan-500/20 text-xs text-slate-300 leading-relaxed font-sans mb-2">
                      <div className="flex items-center gap-1.5 text-cyan-400 font-extrabold uppercase text-[10px]">
                        <Cpu className="w-3.5 h-3.5" /> Automação SOAR & Auto-Healing
                      </div>
                      <p>Adicione e gerencie scripts SSH para remediação automatizada de incidentes. Quando um novo alerta chega e bate com o padrão da <b>Regra de Trigger</b>, o playbook associado pode ser executado para auto-cura.</p>
                    </div>

                    <div className="grid grid-cols-1 xl:grid-cols-2 gap-8">
                      {/* Left side: Create Playbook */}
                      {user?.role === 'admin' ? (
                        <form onSubmit={handleCreatePlaybook} className="flex flex-col gap-4 bg-white/[0.01] p-5 rounded-xl border border-white/5 animate-fadeIn">
                          <h5 className="text-xs font-bold uppercase tracking-wider text-slate-200 border-b border-white/5 pb-2">Cadastrar Novo Playbook</h5>
                          
                          <div className="flex flex-col gap-2.5">
                            <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Carregar Template Padrão</label>
                            <select
                              onChange={(e) => {
                                const val = e.target.value;
                                if (val === 'dos') {
                                  setPlaybookName('Auto-Remediação DoS (iptables)');
                                  setPlaybookTrigger('(?i)DoS|ddos|brute force|port scan|flood');
                                  setPlaybookScript('# Bloqueio de IP atacante via iptables\nif [ -n "$ALERT_SOURCE_IP" ]; then\n  echo "Bloqueando IP hostil: $ALERT_SOURCE_IP"\n  sudo iptables -A INPUT -s $ALERT_SOURCE_IP -j DROP\n  sudo iptables-save\nelse\n  echo "Nenhum IP de origem detectado no alerta."\n  exit 1\nfi');
                                } else if (val === 'service') {
                                  setPlaybookName('Auto-Healing Serviço Down');
                                  setPlaybookTrigger('(?i)service down|failed|inactive|http 502|http 500');
                                  setPlaybookScript('# Reinicia serviço inativo no host remoto\nTARGET_SERVICE="nginx"\necho "Verificando status de $TARGET_SERVICE..."\nif ! systemctl is-active --quiet $TARGET_SERVICE; then\n  echo "Serviço inativo. Tentando reiniciar..."\n  sudo systemctl restart $TARGET_SERVICE\n  sleep 3\n  if systemctl is-active --quiet $TARGET_SERVICE; then\n    echo "Auto-healing realizado com sucesso. Serviço Online!"\n  else\n    echo "Falha ao recuperar serviço. Verificando logs..."\n    sudo journalctl -u $TARGET_SERVICE -n 20\n    exit 1\n  fi\nelse\n  echo "Serviço já está ativo."\nfi');
                                } else if (val === 'cleanup') {
                                  setPlaybookName('Log Rotation & Limpeza de Disco');
                                  setPlaybookTrigger('(?i)disk space|disk full|disk usage > 90%');
                                  setPlaybookScript('# Rotaciona logs do systemd e limpa arquivos temporários\necho "=== USO DE DISCO ANTES ==="\ndf -h /\necho "Limpando logs antigos do systemd journal..."\nsudo journalctl --vacuum-time=1d\necho "Limpando cache do gerenciador de pacotes..."\nif command -v apt-get &> /dev/null; then\n  sudo apt-get clean\nelif command -v yum &> /dev/null; then\n  sudo yum clean all\nfi\necho "Removendo imagens docker órfãs/não usadas..."\nif command -v docker &> /dev/null; then\n  sudo docker system prune -af --volumes\nfi\necho "=== USO DE DISCO DEPOIS ==="\ndf -h /');
                                } else if (val === 'diagnose') {
                                  setPlaybookName('Coleta de Diagnósticos de Performance');
                                  setPlaybookTrigger('(?i)high load|high memory|high CPU|memory leakage');
                                  setPlaybookScript('echo "=== DIAGNÓSTICO DE CARGA DO SISTEMA ==="\nuptime\necho "=== TOP 10 PROCESSOS POR CONSUMO DE CPU ==="\nps -eo pid,ppid,user,%cpu,%mem,cmd --sort=-%cpu | head -n 11\necho "=== TOP 10 PROCESSOS POR CONSUMO DE MEMÓRIA ==="\nps -eo pid,ppid,user,%cpu,%mem,cmd --sort=-%mem | head -n 11\necho "=== ESTATÍSTICA DE REDE E CONEXÕES ==="\nnetstat -tulpen || ss -tulpen');
                                } else if (val === 'rotate') {
                                  setPlaybookName('Rotação de Chaves de Acesso');
                                  setPlaybookTrigger('(?i)security advisory|credential leak|rotate keys');
                                  setPlaybookScript('# Gera novo par de chaves e rotaciona chaves SSH autorizadas\necho "Iniciando rotação programada de credenciais..."\nSSH_DIR="$HOME/.ssh"\nmkdir -p "$SSH_DIR"\nchmod 700 "$SSH_DIR"\n# Rotacionar logs e sessões SSH inativas\nsudo killall -HUP sshd\necho "Configurações de segurança recarregadas."');
                                }
                              }}
                              className="bg-[#0b0f19] border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-violet-500"
                            >
                              <option value="">Selecione um template para preencher...</option>
                              <option value="dos">🛡️ Remediação DoS (Bloqueio de IP)</option>
                              <option value="service">🔄 Restart de Serviço Systemd</option>
                              <option value="cleanup">🧹 Limpeza de Disco & Logs</option>
                              <option value="diagnose">📋 Diagnóstico de Carga & CPU</option>
                              <option value="rotate">🔒 Rotação de Credenciais & SSH Sessions</option>
                            </select>
                          </div>

                          <div className="flex flex-col gap-1.5">
                            <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Nome do Playbook</label>
                            <input
                              type="text"
                              required
                              value={playbookName}
                              onChange={(e) => setPlaybookName(e.target.value)}
                              placeholder="Ex: Restart Nginx"
                              className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500"
                            />
                          </div>

                          <div className="flex flex-col gap-1.5">
                            <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Regra de Trigger (Expressão Regular)</label>
                            <input
                              type="text"
                              required
                              value={playbookTrigger}
                              onChange={(e) => setPlaybookTrigger(e.target.value)}
                              placeholder="Regex para acionar (Ex: (?i)nginx|down)"
                              className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500 font-mono"
                            />
                          </div>

                          <div className="flex flex-col gap-1.5">
                            <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Associar ao Tenant / Cliente</label>
                            <select
                              value={playbookTargetTenantId}
                              onChange={(e) => setPlaybookTargetTenantId(e.target.value)}
                              className="bg-[#0b0f19] border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-violet-500"
                            >
                              <option value="all">🌐 Todos os Tenants (Playbook Global)</option>
                              {tenants.map((t) => (
                                <option key={t.id} value={t.id}>
                                  🏢 {t.name}
                                </option>
                              ))}
                            </select>
                          </div>

                          <div className="flex flex-col gap-1.5">
                            <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Chave Credencial do Host (Cofre)</label>
                            <input
                              type="text"
                              required
                              value={playbookVaultKey}
                              onChange={(e) => setPlaybookVaultKey(e.target.value)}
                              placeholder="Ex: ssh"
                              className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500 font-mono"
                            />
                          </div>

                          <div className="flex flex-col gap-1.5">
                            <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Script / Comandos SSH</label>
                            <textarea
                              required
                              rows={5}
                              value={playbookScript}
                              onChange={(e) => setPlaybookScript(e.target.value)}
                              placeholder="Digite os comandos Bash a serem disparados no servidor..."
                              className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500 font-mono"
                            />
                          </div>

                          <button
                            type="submit"
                            disabled={playbookStatus.status === 'saving'}
                            className="bg-[#8b5cf6] hover:bg-violet-500 text-slate-950 font-bold text-xs py-3 px-4 rounded-lg transition-all shadow-md shadow-violet-950/30 flex items-center justify-center gap-2 cursor-pointer w-full"
                          >
                            {playbookStatus.status === 'saving' && <RefreshCw className="w-3.5 h-3.5 animate-spin" />}
                            Salvar Playbook no Banco
                          </button>

                          {playbookStatus.status === 'success' && (
                            <div className="p-2.5 bg-emerald-950/20 border border-emerald-500/20 text-emerald-400 text-xs rounded-lg">
                              {playbookStatus.message}
                            </div>
                          )}
                          {playbookStatus.status === 'error' && (
                            <div className="p-2.5 bg-rose-950/20 border border-rose-500/20 text-rose-400 text-xs rounded-lg">
                              {playbookStatus.message}
                            </div>
                          )}
                        </form>
                      ) : (
                        <div className="p-5 rounded-xl border border-white/5 bg-white/[0.01] text-xs text-slate-400 flex items-center justify-center text-center">
                          Apenas usuários administradores (role admin) podem criar e alterar playbooks de auto-cura SSH.
                        </div>
                      )}

                      {/* Right side: List Playbooks */}
                      <div className="flex flex-col gap-4 animate-fadeIn">
                        <div className="flex flex-col gap-2 border-b border-white/5 pb-3">
                          <div className="flex items-center justify-between">
                            <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400 block font-sans">
                              Playbooks Ativos
                            </label>
                            <button
                              onClick={fetchPlaybooksAdmin}
                              disabled={isLoadingSettingsPlaybooks}
                              className="flex items-center gap-1.5 px-2 py-0.5 rounded bg-white/5 hover:bg-white/10 border border-white/10 text-[9px] text-slate-300 font-medium transition-all cursor-pointer"
                            >
                              <RefreshCw className={`w-2.5 h-2.5 ${isLoadingSettingsPlaybooks ? 'animate-spin' : ''}`} />
                              <span>Atualizar</span>
                            </button>
                          </div>

                          {/* Filter Segmented Control */}
                          <div className="flex bg-[#0a0f1d] border border-white/5 p-0.5 rounded-lg w-full">
                            <button
                              type="button"
                              onClick={() => setPlaybooksFilter('all')}
                              className={`flex-1 py-1 text-[10px] font-bold rounded-md transition-all cursor-pointer text-center ${
                                playbooksFilter === 'all'
                                  ? 'bg-cyan-500/10 text-cyan-400 border border-cyan-500/20'
                                  : 'text-slate-500 hover:text-slate-300 border border-transparent'
                              }`}
                            >
                              🌐 Todos os Tenants
                            </button>
                            <button
                              type="button"
                              onClick={() => setPlaybooksFilter('tenant')}
                              className={`flex-1 py-1 text-[10px] font-bold rounded-md transition-all cursor-pointer text-center ${
                                playbooksFilter === 'tenant'
                                  ? 'bg-purple-500/10 text-purple-400 border border-purple-500/20'
                                  : 'text-slate-500 hover:text-slate-300 border border-transparent'
                              }`}
                            >
                              🏢 Apenas {selectedTenant?.name || 'Cliente Ativo'}
                            </button>
                          </div>
                        </div>

                        {isLoadingSettingsPlaybooks ? (
                          <div className="flex flex-col items-center justify-center py-16 gap-2 text-slate-400 text-xs">
                            <RefreshCw className="w-6 h-6 animate-spin text-cyan-400" />
                            <span>Carregando playbooks...</span>
                          </div>
                        ) : settingsPlaybooks.length === 0 ? (
                          <div className="text-xs text-slate-500 italic text-center py-10">
                            Nenhum playbook de auto-cura cadastrado para este tenant.
                          </div>
                        ) : (
                          <div className="flex flex-col gap-3 max-h-[500px] overflow-y-auto pr-1">
                            {settingsPlaybooks.map(p => (
                              <div
                                key={p.id}
                                className="p-4 rounded-xl border bg-black/40 border-white/5 text-slate-300 flex flex-col gap-3 hover:border-white/10 transition-all font-sans"
                              >
                                <div className="flex justify-between items-start">
                                  <div className="flex flex-col gap-1 min-w-0 mr-3">
                                    <div className="flex items-center gap-2">
                                      <span className="text-xs font-bold text-slate-200">{p.name}</span>
                                      {p.is_global ? (
                                        <span className="px-1.5 py-0.5 rounded text-[8px] font-bold bg-blue-500/10 border border-blue-500/30 text-blue-400 uppercase flex items-center gap-0.5">
                                          🌐 Global
                                        </span>
                                      ) : (
                                        <span className="px-1.5 py-0.5 rounded text-[8px] font-bold bg-purple-500/10 border border-purple-500/30 text-purple-400 uppercase flex items-center gap-0.5">
                                          🏢 {p.tenant_name || 'Tenant'}
                                        </span>
                                      )}
                                    </div>
                                    <span className="text-[9px] font-mono text-cyan-400 truncate">Trigger: {p.trigger_rule}</span>
                                  </div>
                                  {user?.role === 'admin' && (
                                    <button
                                      onClick={() => handleDeletePlaybook(p.id)}
                                      className="text-[9px] text-rose-400 hover:text-rose-300 bg-rose-500/10 hover:bg-rose-500/20 border border-rose-500/10 hover:border-rose-500/20 px-2 py-1 rounded transition-all font-bold cursor-pointer shrink-0"
                                    >
                                      Excluir
                                    </button>
                                  )}
                                </div>

                                <div className="flex flex-col gap-1 font-mono text-[10px]">
                                  <span className="text-slate-500 font-sans font-bold">Script / Comandos:</span>
                                  <pre className="p-2 rounded bg-black/60 text-slate-300 overflow-x-auto whitespace-pre-wrap">{p.script}</pre>
                                </div>

                                <div className="flex items-center justify-between text-[9px] text-slate-500 font-bold uppercase tracking-wider border-t border-white/5 pt-2">
                                  <span>Cofre: <strong className="text-slate-300 font-mono">{p.vault_key_host}</strong></span>
                                  <span>Criado: <strong className="text-slate-300">{new Date(p.created_at).toLocaleDateString()}</strong></span>
                                </div>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                ) : selectedIntegrationTool === 'operational_kpis' ? (
                  <OperationalKpisPanel tenantId={selectedTenant?.id} />
                ) : selectedIntegrationTool === 'approvals_admin' ? (
                  <RunbookApprovalsPanel />
                ) : selectedIntegrationTool === 'response_actions' ? (
                  <ResponseActionsPanel />
                ) : selectedIntegrationTool === 'network_discovery' ? (
                  <NetworkDiscoveryPanel tenantId={selectedTenant?.id} />
                ) : selectedIntegrationTool === 'assets' ? (
                  <AssetsPanel tenantId={selectedTenant?.id} />
                ) : selectedIntegrationTool === 'suppression' ? (
                  <SuppressionRulesPanel tenantId={selectedTenant?.id} />
                ) : selectedIntegrationTool === 'access_admin' ? (
                  <AccessControlPanel />
                ) : selectedIntegrationTool === 'users_admin' ? (
                  // 4. Admin Users Form
                  <div className="flex flex-col gap-4">
                    <div className="flex flex-col gap-3 p-4 rounded-xl bg-violet-950/10 border border-violet-500/20 text-xs text-slate-300 leading-relaxed font-sans mb-2">
                      <div className="flex items-center gap-1.5 text-violet-400 font-extrabold uppercase text-[10px]">
                        <User className="w-3.5 h-3.5" /> Painel de Controle de Usuários (RBAC)
                      </div>
                      <p>Como administrador do NOC, você pode cadastrar e gerenciar perfis de novos colaboradores. Escolha se o nível de privilégio será **Admin** (acesso irrestrito), **Operator** (gerenciamento e SLA) ou **Viewer** (somente visualização).</p>
                    </div>

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                      {/* Left: Create Form */}
                      <form onSubmit={handleAdminCreateUser} className="flex flex-col gap-4 animate-fadeIn">
                        <div className="flex flex-col gap-1.5">
                          <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Nome Completo</label>
                          <input
                            type="text"
                            required
                            value={adminUserName}
                            onChange={(e) => setAdminUserName(e.target.value)}
                            placeholder="Ex: Carlos Silva"
                            className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500 transition-all placeholder:text-slate-600"
                          />
                        </div>

                        <div className="flex flex-col gap-1.5">
                          <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Endereço de E-mail</label>
                          <input
                            type="email"
                            required
                            value={adminUserEmail}
                            onChange={(e) => setAdminUserEmail(e.target.value)}
                            placeholder="usuario@empresa.com"
                            className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500 transition-all placeholder:text-slate-600"
                          />
                        </div>

                        <div className="flex flex-col gap-1.5">
                          <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Senha Provisória</label>
                          <div className="relative flex items-center">
                            <input
                              type={showAdminUserPassword ? 'text' : 'password'}
                              required
                              value={adminUserPassword}
                              onChange={(e) => setAdminUserPassword(e.target.value)}
                              placeholder="Mínimo de 6 caracteres"
                              className="w-full bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 pr-10 text-xs text-white focus:outline-none focus:border-violet-500 transition-all placeholder:text-slate-600"
                            />
                            <button
                              type="button"
                              onClick={() => setShowAdminUserPassword(!showAdminUserPassword)}
                              className="absolute right-3 text-slate-400 hover:text-white transition-all cursor-pointer"
                            >
                              {showAdminUserPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                            </button>
                          </div>
                        </div>

                        <div className="flex flex-col gap-1.5">
                          <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Nível de Permissão (Role)</label>
                          <select
                            value={adminUserRole}
                            onChange={(e) => setAdminUserRole(e.target.value)}
                            className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500 transition-all"
                          >
                            <optgroup label="Papéis granulares (SOC/NOC)">
                              <option value="read_only">Read-Only (Somente leitura de painéis)</option>
                              <option value="analyst_l1">Analyst L1 (Triagem)</option>
                              <option value="analyst_l2">Analyst L2 (Investigação/Ação)</option>
                              <option value="analyst_l3">Analyst L3 (Resposta avançada)</option>
                              <option value="tenant_admin">Tenant Admin (Admin do cliente)</option>
                            </optgroup>
                            <optgroup label="Legado (compatível)">
                              <option value="operator">Operator (≈ Analyst L2)</option>
                              <option value="admin">Admin (Acesso completo + plataforma)</option>
                              <option value="viewer">Viewer (≈ Read-Only)</option>
                            </optgroup>
                          </select>
                        </div>

                        {adminUserRole === 'admin' ? (
                          <div className="p-3 rounded-lg bg-blue-950/20 border border-blue-500/15 text-blue-300 text-[11px] flex items-center gap-2">
                            <ShieldCheck className="w-3.5 h-3.5 shrink-0" />
                            Admin de plataforma acessa todos os tenants — não é preciso selecionar.
                          </div>
                        ) : (
                          <div className="flex flex-col gap-1.5">
                            <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">
                              Tenants Autorizados ({adminUserTenantIds.length} selecionado{adminUserTenantIds.length === 1 ? '' : 's'})
                            </label>
                            <p className="text-[10px] text-slate-500">
                              Selecione um a um os clientes que este usuário poderá acessar. Sem nenhum, ele não conseguirá logar.
                            </p>
                            <div className="flex flex-col gap-1.5 max-h-[180px] overflow-y-auto pr-1 mt-1 rounded-lg border border-white/5 bg-black/30 p-2">
                              {tenants.length === 0 ? (
                                <span className="text-[10px] text-amber-500 font-medium px-1 py-2">Nenhum tenant cadastrado.</span>
                              ) : (
                                tenants.map((t) => {
                                  const checked = adminUserTenantIds.includes(t.id);
                                  return (
                                    <label
                                      key={t.id}
                                      className="flex items-center gap-2 px-2 py-1.5 rounded-md hover:bg-white/[0.03] cursor-pointer transition-all"
                                    >
                                      <input
                                        type="checkbox"
                                        checked={checked}
                                        onChange={(e) =>
                                          setAdminUserTenantIds((prev) =>
                                            e.target.checked ? [...prev, t.id] : prev.filter((id) => id !== t.id)
                                          )
                                        }
                                        className="accent-violet-500 w-3.5 h-3.5 cursor-pointer"
                                      />
                                      <span className="text-xs text-slate-200 truncate">{t.name}</span>
                                    </label>
                                  );
                                })
                              )}
                            </div>
                          </div>
                        )}

                        <button
                          type="submit"
                          disabled={adminUserStatus.status === 'saving'}
                          className="bg-[#8b5cf6] hover:bg-violet-500 text-slate-950 font-bold text-xs py-3 px-4 rounded-lg transition-all shadow-md shadow-violet-950/30 flex items-center justify-center gap-2 cursor-pointer"
                        >
                          {adminUserStatus.status === 'saving' && <RefreshCw className="w-3.5 h-3.5 animate-spin" />}
                          Cadastrar Novo Usuário
                        </button>

                        {adminUserStatus.status === 'success' && (
                          <div className="p-3 bg-emerald-950/20 border border-emerald-500/20 text-emerald-400 text-xs rounded-lg font-sans">
                            {adminUserStatus.message}
                          </div>
                        )}
                        {adminUserStatus.status === 'error' && (
                          <div className="p-3 bg-rose-950/20 border border-rose-500/20 text-rose-400 text-xs rounded-lg font-sans">
                            {adminUserStatus.message}
                          </div>
                        )}
                      </form>

                      {/* Right: Active Users List */}
                      <div className="flex flex-col gap-4 border-l border-white/5 pl-6 animate-fadeIn">
                        <div className="flex items-center justify-between">
                          <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400 block">
                            Usuários Ativos no Sistema (RBAC)
                          </label>
                          <button
                            onClick={fetchAdminUsers}
                            disabled={isLoadingAdminUsers}
                            className="flex items-center gap-1.5 px-2 py-0.5 rounded bg-white/5 hover:bg-white/10 border border-white/10 text-[9px] text-slate-300 font-medium transition-all cursor-pointer"
                          >
                            <RefreshCw className={`w-2.5 h-2.5 ${isLoadingAdminUsers ? 'animate-spin' : ''}`} />
                            <span>Atualizar</span>
                          </button>
                        </div>
                        
                        {isLoadingAdminUsers ? (
                          <div className="flex flex-col items-center justify-center py-12 gap-2 text-slate-400 text-xs font-sans">
                            <RefreshCw className="w-6 h-6 animate-spin text-violet-400" />
                            <span>Carregando usuários...</span>
                          </div>
                        ) : adminUsers.length === 0 ? (
                          <span className="text-[10px] text-amber-500 font-medium">Nenhum usuário cadastrado.</span>
                        ) : (
                          <div className="flex flex-col gap-2 max-h-[300px] overflow-y-auto pr-1 font-sans">
                            {adminUsers.map(u => {
                              const isSelf = u.email === user?.email;
                              return (
                                <div key={u.id} className="p-3 rounded-lg bg-black/40 border border-white/5 flex items-center justify-between text-xs hover:border-white/10 transition-all font-sans">
                                  <div className="flex flex-col gap-0.5 min-w-0 mr-2">
                                    <div className="flex items-center gap-1.5 flex-wrap">
                                      <span className="font-bold text-slate-200 truncate">{u.name}</span>
                                      <span className={`px-1 rounded text-[8px] font-extrabold uppercase tracking-wider leading-normal ${
                                        u.global_role === 'admin' 
                                          ? 'bg-violet-500/20 text-violet-400 border border-violet-500/10' 
                                          : u.global_role === 'operator' 
                                            ? 'bg-blue-500/20 text-blue-400 border border-blue-500/10'
                                            : 'bg-slate-500/20 text-slate-400 border border-slate-500/10'
                                      }`}>
                                        {u.global_role}
                                      </span>
                                    </div>
                                    <span className="text-[10px] text-slate-400 font-mono select-all truncate">{u.email}</span>
                                  </div>
                                  <button
                                    onClick={() => handleDeleteUser(u.id)}
                                    disabled={isSelf}
                                    className={`text-[9px] px-2.5 py-1 rounded transition-all font-bold cursor-pointer shrink-0 ${
                                      isSelf 
                                        ? 'text-slate-600 bg-white/5 cursor-not-allowed border border-white/5' 
                                        : 'text-rose-400 hover:text-rose-300 bg-rose-500/10 hover:bg-rose-500/20 border border-rose-500/10 hover:border-rose-500/20'
                                    }`}
                                  >
                                    Excluir
                                  </button>
                                </div>
                              );
                            })}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                ) : selectedIntegrationTool === 'tenants_admin' ? (
                  // 5. Admin Tenants Form (Clean unified version)
                  <div className="flex flex-col gap-4 font-sans animate-fadeIn">
                    <div className="flex flex-col gap-3 p-4 rounded-xl bg-violet-950/10 border border-violet-500/20 text-xs text-slate-300 leading-relaxed font-sans mb-2">
                      <div className="flex items-center gap-1.5 text-violet-400 font-extrabold uppercase text-[10px]">
                        <Layers className="w-3.5 h-3.5" /> Painel de Controle de Tenants (Multi-tenancy)
                      </div>
                      <p>Adicione novos Tenants para segmentação física de alertas. Ao cadastrar um novo tenant, ele passará a contar com isolamento completo de banco de dados e regras de segurança baseadas em RLS.</p>
                    </div>

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-8">
                      <form onSubmit={handleCreateTenant} className="flex flex-col gap-4 animate-fadeIn">
                        <div className="flex flex-col gap-1.5">
                          <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Nome da Empresa / Tenant</label>
                          <input
                            type="text"
                            required
                            value={newTenantName}
                            onChange={(e) => setNewTenantName(e.target.value)}
                            placeholder="Ex: Banco Alfa S.A."
                            className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500 transition-all placeholder:text-slate-600"
                          />
                        </div>

                        <button
                          type="submit"
                          disabled={tenantCreateStatus.status === 'saving'}
                          className="bg-[#8b5cf6] hover:bg-violet-500 text-slate-950 font-bold text-xs py-3 px-4 rounded-lg transition-all shadow-md shadow-violet-950/30 flex items-center justify-center gap-2 cursor-pointer w-full"
                        >
                          {tenantCreateStatus.status === 'saving' && <RefreshCw className="w-3.5 h-3.5 animate-spin" />}
                          Cadastrar Novo Tenant
                        </button>

                        {tenantCreateStatus.status === 'success' && (
                          <div className="p-2.5 bg-emerald-950/20 border border-emerald-500/20 text-emerald-400 text-xs rounded-lg font-sans">
                            {tenantCreateStatus.message}
                          </div>
                        )}
                        {tenantCreateStatus.status === 'error' && (
                          <div className="p-2.5 bg-rose-950/20 border border-rose-500/20 text-rose-400 text-xs rounded-lg font-sans">
                            {tenantCreateStatus.message}
                          </div>
                        )}
                      </form>

                      <div className="flex flex-col gap-3 animate-fadeIn">
                        <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400 block font-sans">Tenants Ativos no Banco de Dados</label>
                        <div className="flex flex-col gap-2.5 max-h-[350px] overflow-y-auto pr-1">
                          {tenants.map(t => (
                            <div
                              key={t.id}
                              className="p-3.5 rounded-lg border bg-black/40 border-white/5 text-slate-300 flex items-center justify-between transition-all"
                            >
                              <div className="flex flex-col gap-1 min-w-0 mr-3">
                                <span className="text-xs font-bold text-slate-200 truncate">{t.name}</span>
                                <span className="text-[9px] font-mono select-all text-slate-500 truncate">{t.id}</span>
                              </div>
                              <button
                                onClick={() => handleDeleteTenant(t.id)}
                                className="text-[9px] text-rose-400 hover:text-rose-300 bg-rose-500/10 hover:bg-rose-500/20 border border-rose-500/10 hover:border-rose-500/20 px-2.5 py-1.5 rounded transition-all font-bold cursor-pointer shrink-0"
                              >
                                Excluir
                              </button>
                            </div>
                          ))}
                        </div>
                      </div>
                    </div>
                  </div>
                ) : selectedIntegrationTool === 'vault_admin' ? (
                  // Vault keys expiration & status inspector
                  <div className="flex flex-col gap-4 font-sans animate-fadeIn">
                    <div className="flex flex-col gap-3 p-4 rounded-xl bg-violet-950/10 border border-violet-500/20 text-xs text-slate-300 leading-relaxed font-sans mb-2">
                      <div className="flex items-center gap-1.5 text-violet-400 font-extrabold uppercase text-[10px]">
                        <Lock className="w-3.5 h-3.5" /> Auditoria Gerencial do Cofre (Vault)
                      </div>
                      <p>Lista de chaves criptográficas de API e SSH cadastradas para este tenant. Por motivos de segurança, os valores descriptografados originais não são enviados para o navegador.</p>
                    </div>

                    {isLoadingVaultSecrets ? (
                      <div className="flex items-center justify-center py-12 gap-3 text-slate-400 text-xs font-sans">
                        <RefreshCw className="w-5 h-5 animate-spin text-violet-400" />
                        <span>Carregando auditoria de segredos...</span>
                      </div>
                    ) : vaultSecrets.length > 0 ? (
                      <div className="flex flex-col gap-2.5">
                        {vaultSecrets.map((s, idx) => (
                          <div key={idx} className="p-3.5 rounded-xl bg-black/40 border border-white/5 flex items-center justify-between text-xs hover:border-white/10 transition-all font-sans">
                            <div className="flex flex-col gap-1 font-sans">
                              <span className="font-bold text-slate-200 font-mono text-xs">{s.secret_key}</span>
                              <div className="flex items-center gap-2 text-[10px] text-slate-500">
                                <span>Criado: {new Date(s.created_at).toLocaleString()}</span>
                                <span>•</span>
                                <span className="text-emerald-400 font-semibold">GCM-256 Encriptado</span>
                              </div>
                            </div>
                            <span className="px-2 py-0.5 rounded bg-violet-500/10 text-violet-400 border border-violet-500/25 font-bold uppercase text-[9px]">Protegido</span>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <div className="p-6 rounded-xl bg-white/[0.01] border border-dashed border-white/5 text-center text-xs text-slate-500 py-10 font-sans">
                        Nenhum segredo criptografado armazenado para o tenant atual.
                      </div>
                    )}
                  </div>
                ) : selectedIntegrationTool === 'audit_admin' ? (
                  // SSH Commands Auditor
                  <div className="flex flex-col gap-4 font-sans animate-fadeIn">
                    <div className="flex flex-col gap-3 p-4 rounded-xl bg-violet-950/10 border border-violet-500/20 text-xs text-slate-300 leading-relaxed font-sans mb-2">
                      <div className="flex items-center gap-1.5 text-violet-400 font-extrabold uppercase text-[10px]">
                        <Terminal className="w-3.5 h-3.5" /> Auditoria de Scripts & Remediações SSH
                      </div>
                      <p>Visualização e auditoria completa de comandos remotos disparados via SOAR/Runbooks para resolução automática de incidentes (Auto-healing) ou contenção de ataques.</p>
                    </div>

                    {isLoadingRunbookAudits ? (
                      <div className="flex items-center justify-center py-12 gap-3 text-slate-400 text-xs font-sans">
                        <RefreshCw className="w-5 h-5 animate-spin text-violet-400" />
                        <span>Carregando logs de auditoria...</span>
                      </div>
                    ) : runbookAudits.length > 0 ? (
                      <div className="flex flex-col gap-4 max-h-[400px] overflow-y-auto pr-1">
                        {runbookAudits.map((a, idx) => (
                          <div key={idx} className="p-4 rounded-xl bg-black/40 border border-white/5 flex flex-col gap-3 hover:border-white/10 transition-all font-sans text-xs">
                            <div className="flex justify-between items-center border-b border-white/5 pb-2 font-sans">
                              <div className="flex flex-col gap-0.5">
                                <span className="font-bold text-slate-200">Ação disparada por: <strong className="text-violet-400">{a.triggered_by}</strong></span>
                                <span className="text-[10px] text-slate-500">{new Date(a.created_at).toLocaleString()}</span>
                              </div>
                              <span className="px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-bold uppercase text-[9px]">Sucesso</span>
                            </div>
                            
                            <div className="flex flex-col gap-1 font-mono text-[10px]">
                              <span className="text-slate-500 font-sans font-bold">Script Executado:</span>
                              <pre className="p-2.5 rounded bg-black/60 text-slate-300 overflow-x-auto whitespace-pre-wrap">{a.script}</pre>
                            </div>
                            
                            <div className="flex flex-col gap-1 font-mono text-[10px]">
                              <span className="text-slate-500 font-sans font-bold">Console Output:</span>
                              <pre className="p-2.5 rounded bg-black/80 text-emerald-400 overflow-x-auto max-h-36 overflow-y-auto whitespace-pre-wrap">{a.output}</pre>
                            </div>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <div className="text-xs text-slate-500 italic text-center py-10">
                        Nenhuma execução de remediação remota registrada para este cliente.
                      </div>
                    )}
                  </div>
                ) : selectedIntegrationTool === 'sla_report' ? (
                  // Relatório Dual-Mode (NOC/SOC Compliance)
                  <div className="flex flex-col gap-4 font-sans animate-fadeIn">
                    <div className="flex flex-col gap-3 p-4 rounded-xl bg-emerald-950/10 border border-emerald-500/20 text-xs text-slate-300 leading-relaxed font-sans mb-2">
                      <div className="flex items-center gap-1.5 text-emerald-400 font-extrabold uppercase text-[10px]">
                        <TrendingUp className="w-3.5 h-3.5" /> Relatório Dual-Mode (NOC/SOC Compliance)
                      </div>
                      <p>Mude o modo de visualização entre a perspectiva de governança de negócios (C-Level) ou detalhamento de infraestrutura e cibersegurança (Analistas).</p>
                      
                      {/* Mode switcher */}
                      <div className="flex bg-black/40 rounded-lg p-0.5 mt-1 border border-white/5 w-fit">
                        <button
                          onClick={() => setReportMode('executive')}
                          className={`px-3 py-1 text-[10px] uppercase font-bold tracking-wide rounded-md transition-all cursor-pointer ${
                            reportMode === 'executive'
                              ? 'bg-emerald-500 text-slate-950'
                              : 'text-slate-400 hover:text-slate-200'
                          }`}
                        >
                          Modo Executivo (Business)
                        </button>
                        <button
                          onClick={() => setReportMode('technical')}
                          className={`px-3 py-1 text-[10px] uppercase font-bold tracking-wide rounded-md transition-all cursor-pointer ${
                            reportMode === 'technical'
                              ? 'bg-emerald-500 text-slate-950'
                              : 'text-slate-400 hover:text-slate-200'
                          }`}
                        >
                          Modo Técnico (SOC)
                        </button>
                      </div>
                    </div>

                    {isLoadingSla ? (
                      <div className="flex flex-col items-center justify-center py-20 gap-3 text-slate-400 text-xs font-sans">
                        <RefreshCw className="w-8 h-8 text-emerald-400 animate-spin" />
                        <span>Gerando métricas de governança...</span>
                      </div>
                    ) : slaData ? (
                      <div className="flex flex-col gap-5 text-slate-300 font-sans animate-fadeIn">
                        {reportMode === 'executive' ? (
                          <>
                            {/* Executive view */}
                            <div className="grid grid-cols-4 gap-4">
                              <div className="p-4 rounded-xl bg-white/5 border border-white/5 flex flex-col gap-1 animate-fadeIn">
                                <span className="text-[9px] uppercase font-bold text-slate-400">Total de Incidentes</span>
                                <span className="text-2xl font-bold text-slate-100">{slaData.total_incidents}</span>
                              </div>

                              <div className="p-4 rounded-xl bg-white/5 border border-white/5 flex flex-col gap-1 animate-fadeIn">
                                <span className="text-[9px] uppercase font-bold text-slate-400">Tempo Médio Reconhecimento (MTTA)</span>
                                <span className="text-2xl font-bold text-slate-100">{slaData.average_tta.toFixed(1)} min</span>
                              </div>

                              <div className="p-4 rounded-xl bg-white/5 border border-white/5 flex flex-col gap-1 animate-fadeIn">
                                <span className="text-[9px] uppercase font-bold text-slate-400">Tempo Médio Resposta (MTTR)</span>
                                <span className="text-2xl font-bold text-slate-100">{slaData.average_ttr.toFixed(1)} min</span>
                              </div>

                              <div className="p-4 rounded-xl bg-white/5 border border-white/5 flex flex-col gap-1 animate-fadeIn">
                                <span className="text-[9px] uppercase font-bold text-slate-400">Nível Geral de SLA (Compliance)</span>
                                <span className="text-2xl font-bold text-emerald-400">{slaData.sla_compliance.toFixed(1)}%</span>
                              </div>
                            </div>

                            <div className="p-5 rounded-xl bg-white/5 border border-white/5 flex flex-col gap-3 animate-fadeIn">
                              <h5 className="text-xs font-bold text-slate-200 uppercase tracking-wide">Status Geral de Compliance</h5>
                              <div className="w-full bg-slate-950 rounded-full h-3.5 border border-white/5 overflow-hidden">
                                <div
                                  className="bg-emerald-500 h-full rounded-full transition-all"
                                  style={{ width: `${slaData.sla_compliance}%` }}
                                ></div>
                              </div>
                              <p className="text-[10px] text-slate-400 leading-relaxed">
                                O SLA (Service Level Agreement) é calculado com base na meta de tempo de resolução por severidade: até 15 minutos para alertas fatais, 30 minutos para críticos, 2 horas para avisos e 8 horas para informativos. Compliance considera apenas incidentes já resolvidos.
                              </p>
                            </div>

                            {slaData.by_severity && slaData.by_severity.length > 0 && (
                              <div className="p-5 rounded-xl bg-white/5 border border-white/5 flex flex-col gap-3 animate-fadeIn">
                                <h5 className="text-xs font-bold text-slate-200 uppercase tracking-wide">MTTR Médio vs. Meta de SLA, por Severidade</h5>
                                <ResponsiveContainer width="100%" height={260}>
                                  <BarChart data={slaData.by_severity} margin={{ top: 8, right: 8, left: 0, bottom: 8 }}>
                                    <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" />
                                    <XAxis dataKey="severity" tickFormatter={(s: string) => s.toUpperCase()} stroke="#64748b" fontSize={10} />
                                    <YAxis stroke="#64748b" fontSize={10} label={{ value: 'minutos', angle: -90, position: 'insideLeft', fill: '#64748b', fontSize: 10 }} />
                                    <Tooltip contentStyle={{ background: '#0b0f19', border: '1px solid rgba(255,255,255,0.1)', fontSize: 11 }} labelFormatter={(s: string) => s.toUpperCase()} />
                                    <Legend wrapperStyle={{ fontSize: 10 }} />
                                    <Bar dataKey="average_ttr" name="MTTR médio" fill="#10b981" radius={[4, 4, 0, 0]} />
                                    <Bar dataKey="target_minutes" name="Meta SLA" fill="#64748b" fillOpacity={0.35} radius={[4, 4, 0, 0]} />
                                  </BarChart>
                                </ResponsiveContainer>
                              </div>
                            )}

                            {/* Export/Download SLA PDF */}
                            <div className="p-5 rounded-xl bg-[#0e1626] border border-cyan-500/10 flex items-center justify-between mt-2 animate-fadeIn">
                              <div className="flex flex-col gap-0.5">
                                <h5 className="text-xs font-bold text-white">Relatório Executivo Mensal</h5>
                                <p className="text-[10px] text-slate-400">Gere e baixe a via em PDF oficial com assinaturas e log de incidentes.</p>
                              </div>
                              <button
                                onClick={() => {
                                  if (!token || !selectedTenant) return;
                                  window.open(`${API_BASE_URL}/api/v1/reports/sla?token=${token}&tenant_name=${encodeURIComponent(selectedTenant.name)}`);
                                }}
                                className="bg-emerald-600 hover:bg-emerald-500 text-slate-950 font-bold uppercase tracking-wider text-[10px] px-4 py-2.5 rounded-lg flex items-center gap-1.5 transition-all shadow-lg cursor-pointer"
                              >
                                <FileText className="w-3.5 h-3.5" />
                                Baixar Relatório PDF
                              </button>
                            </div>
                          </>
                        ) : (
                          <>
                            {/* Technical SOC view */}
                            {/* MITRE ATT&CK Matrix simulation */}
                            <div className="p-4 rounded-xl bg-white/5 border border-white/5 flex flex-col gap-3 animate-fadeIn">
                              <div className="flex justify-between items-center animate-fadeIn">
                                <h5 className="text-xs font-bold text-slate-200 uppercase tracking-wider">Mapeamento Tático MITRE ATT&CK</h5>
                                <span className="text-[9px] font-mono text-slate-500">v13 Enterprise Matrix</span>
                              </div>
                              
                              <div className="grid grid-cols-3 gap-3 text-[10px]">
                                <div className="p-2.5 rounded bg-slate-900 border border-white/5 flex flex-col gap-1.5">
                                  <span className="font-bold text-slate-400 border-b border-white/5 pb-1">1. Initial Access</span>
                                  <div className="flex flex-col gap-1">
                                    <span className="p-1 rounded bg-rose-500/10 text-rose-400 border border-rose-500/20 font-medium">T1078 Valid Accounts (VPN)</span>
                                    <span className="p-1 rounded bg-white/5 text-slate-400 font-medium">T1190 Exploit Public-Facing App</span>
                                  </div>
                                </div>
                                
                                <div className="p-2.5 rounded bg-slate-900 border border-white/5 flex flex-col gap-1.5 animate-fadeIn">
                                  <span className="font-bold text-slate-400 border-b border-white/5 pb-1">2. Credential Access</span>
                                  <div className="flex flex-col gap-1">
                                    <span className="p-1 rounded bg-rose-500/10 text-rose-400 border border-rose-500/20 font-medium">T1110 Brute Force (SSH)</span>
                                    <span className="p-1 rounded bg-white/5 text-slate-400 font-medium">T1555 Credentials from Store</span>
                                  </div>
                                </div>

                                <div className="p-2.5 rounded bg-slate-900 border border-white/5 flex flex-col gap-1.5 animate-fadeIn">
                                  <span className="font-bold text-slate-400 border-b border-white/5 pb-1">3. Impact</span>
                                  <div className="flex flex-col gap-1">
                                    <span className="p-1 rounded bg-rose-500/10 text-rose-400 border border-rose-500/20 font-medium">T1498 Network DoS (Loki)</span>
                                    <span className="p-1 rounded bg-white/5 text-slate-400 font-medium">T1489 Service Stop</span>
                                  </div>
                                </div>
                              </div>
                            </div>

                            {/* Threat Intelligence Feed simulator */}
                            <div className="p-4 rounded-xl bg-[#030712] border border-white/5 flex flex-col gap-2.5 animate-fadeIn">
                              <h5 className="text-xs font-bold text-slate-200 uppercase tracking-wider font-sans">Feed Integrado de Threat Intelligence</h5>
                              <div className="flex flex-col gap-2 max-h-[140px] overflow-y-auto pr-1 animate-fadeIn">
                                <div className="p-2 rounded bg-white/5 flex items-center justify-between text-xs animate-fadeIn">
                                  <div className="flex flex-col gap-0.5">
                                    <span className="font-extrabold text-rose-400 font-mono">[CVE-2026-9912] Threat Advisory</span>
                                    <span className="text-[10px] text-slate-400">Atividade suspeita vinda do IP malicioso catalogado: 198.51.100.42</span>
                                  </div>
                                  <span className="text-[8px] font-bold bg-rose-500/15 text-rose-400 px-2 py-0.5 rounded border border-rose-500/30 uppercase">Bloqueado SOAR</span>
                                </div>
                                <div className="p-2 rounded bg-white/5 flex items-center justify-between text-xs">
                                  <div className="flex flex-col gap-0.5">
                                    <span className="font-extrabold text-amber-400 font-mono">[STIX/TAXII feed] IP Reputation</span>
                                    <span className="text-[10px] text-slate-400">Scanner de porta de entrada detectado em múltiplos firewalls periféricos.</span>
                                  </div>
                                  <span className="text-[8px] font-bold bg-amber-500/15 text-amber-400 px-2 py-0.5 rounded border border-amber-500/30 uppercase">Monitorando</span>
                                </div>
                              </div>
                            </div>
                          </>
                        )}
                      </div>
                    ) : (
                      <div className="text-xs text-slate-505 italic text-center py-10 font-sans">
                        Nenhum dado operacional registrado para calcular métricas de SLA.
                      </div>
                    )}
                  </div>
                ) : null}
              </div>
            </div>
      )}
    </>
  );
}
