'use client';

import React, { useState, useEffect, useRef } from 'react';
import { 
  Activity, 
  Wifi, 
  WifiOff, 
  AlertOctagon, 
  AlertTriangle, 
  Info, 
  Terminal, 
  Layers, 
  CheckCircle2, 
  User, 
  Cpu, 
  RefreshCw,
  Search,
  LayoutDashboard,
  Brain,
  FileText,
  Lock,
  Link as LinkIcon,
  HelpCircle,
  Copy,
  Check,
  ChevronDown,
  ChevronLeft,
  Palette,
  Target,
  Zap,
  Clock,
  Shield,
  TrendingUp,
  Network,
  Settings,
  Users,
  Eye,
  EyeOff,
  Trash2
} from 'lucide-react';

interface Alert {
  id: string;
  tenant_id: string;
  device_id?: string;
  event_type: string;
  severity: 'info' | 'warning' | 'critical' | 'fatal';
  status: 'triggered' | 'acknowledged' | 'resolved' | 'suppressed';
  summary: string;
  payload: Record<string, any>;
  ai_analysis?: Record<string, any>;
  created_at: string;
  updated_at: string;
  resolved_at?: string;
  acknowledged_at?: string;
  ai_diagnostic?: string;
}

const SlaCountdown = ({ alert }: { alert: Alert }) => {
  const [timeLeft, setTimeLeft] = useState<string>('');
  const [isOverSla, setIsOverSla] = useState(false);

  useEffect(() => {
    if (alert.status === 'resolved' || alert.status === 'suppressed') {
      setTimeLeft('SLA OK');
      setIsOverSla(false);
      return;
    }

    const calculateTime = () => {
      const created = new Date(alert.created_at).getTime();
      let limitMs = 480 * 60 * 1000; // default info: 8 hours
      if (alert.severity === 'fatal') limitMs = 15 * 60 * 1000;
      else if (alert.severity === 'critical') limitMs = 30 * 60 * 1000;
      else if (alert.severity === 'warning') limitMs = 120 * 60 * 1000;

      const now = new Date().getTime();
      const diff = (created + limitMs) - now;

      if (diff <= 0) {
        setTimeLeft('SLA ESTOURADO');
        setIsOverSla(true);
      } else {
        const mins = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60));
        const secs = Math.floor((diff % (1000 * 60)) / 1000);
        
        let hrsText = "";
        const hrs = Math.floor(diff / (1000 * 60 * 60));
        if (hrs > 0) {
          hrsText = `${hrs}h `;
        }
        
        setTimeLeft(`${hrsText}${mins}m ${secs}s`);
        setIsOverSla(false);
      }
    };

    calculateTime();
    const interval = setInterval(calculateTime, 1000);
    return () => clearInterval(interval);
  }, [alert.created_at, alert.severity, alert.status]);

  if (alert.status === 'resolved' || alert.status === 'suppressed') {
    return <span className="text-[10px] text-emerald-400 font-extrabold bg-emerald-500/10 px-2 py-0.5 rounded border border-emerald-500/20">RESOLVIDO</span>;
  }

  return (
    <span className={`text-[10px] font-mono font-bold px-2 py-0.5 rounded border flex items-center gap-1 shrink-0 ${
      isOverSla 
        ? 'text-rose-400 bg-rose-500/10 border-rose-500/30 animate-pulse' 
        : 'text-amber-400 bg-amber-500/10 border-amber-500/30'
    }`}>
      <Clock className="w-3 h-3" />
      {timeLeft}
    </span>
  );
};
// SRE Global Resilient Fetch Wrapper (Monkey-patching window.fetch with retries and backoff)
if (typeof window !== 'undefined' && !(window as any).__fetchPatched) {
  (window as any).__fetchPatched = true;
  const originalFetch = window.fetch;
  window.fetch = async function (input, init) {
    let retries = 2;
    let delay = 500;
    
    async function attemptFetch(currentRetry: number): Promise<Response> {
      try {
        const response = await originalFetch(input, init);
        // Automatically retry on transient server errors (502, 503, 504)
        if (!response.ok && [502, 503, 504].includes(response.status) && currentRetry > 0) {
          console.warn(`[SRE FETCH] Received status ${response.status} from ${String(input)}. Retrying in ${delay}ms... (${currentRetry} attempts left)`);
          await new Promise(resolve => setTimeout(resolve, delay));
          delay *= 2;
          return attemptFetch(currentRetry - 1);
        }
        return response;
      } catch (err) {
        // Automatically retry on network failures (like ERR_CONNECTION_REFUSED or CORS glitches)
        if (currentRetry > 0) {
          console.warn(`[SRE FETCH] Network error on ${String(input)}. Retrying in ${delay}ms... (${currentRetry} attempts left):`, err);
          await new Promise(resolve => setTimeout(resolve, delay));
          delay *= 2;
          return attemptFetch(currentRetry - 1);
        }
        console.error(`[SRE FETCH] Fatal network error on ${String(input)}:`, err);
        throw err;
      }
    }
    
    return attemptFetch(retries);
  };
}


let API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

// Dynamic production fallback resolution in case of missing build-time environment variables
if (typeof window !== 'undefined') {
  const hostname = window.location.hostname;
  const isLocalhost = hostname === 'localhost' || hostname === '127.0.0.1' || hostname.startsWith('192.168.') || hostname.startsWith('10.');
  if (!isLocalhost && API_BASE_URL.includes('localhost')) {
    API_BASE_URL = 'https://noc-soc-saas-production.up.railway.app';
  }
}

const getWSUrl = (token: string, tenantIds: string[]) => {
  let base = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
  if (typeof window !== 'undefined') {
    const hostname = window.location.hostname;
    const isLocalhost = hostname === 'localhost' || hostname === '127.0.0.1' || hostname.startsWith('192.168.') || hostname.startsWith('10.');
    if (!isLocalhost && base.includes('localhost')) {
      base = 'https://noc-soc-saas-production.up.railway.app';
    }
  }
  const host = base.replace(/^https?:\/\//, '');
  
  // Force secure WebSocket (wss) if API base is https OR if the frontend page itself is loaded over https
  let wsProtocol = 'ws';
  if (base.startsWith('https') || (typeof window !== 'undefined' && window.location.protocol === 'https:')) {
    wsProtocol = 'wss';
  }
  return `${wsProtocol}://${host}/api/v1/ws?token=${encodeURIComponent(token)}&tenants=${tenantIds.join(',')}`;
};

export default function CockpitPage() {
  // Authentication States
  const [token, setToken] = useState<string | null>(null);
  const [user, setUser] = useState<{ id: string, email: string, name: string, role: string } | null>(null);
  const [authView, setAuthView] = useState<'login' | 'register'>('login');
  const [authEmail, setAuthEmail] = useState('');
  const [authPassword, setAuthPassword] = useState('');
  const [authName, setAuthName] = useState('');
  const [authTenant, setAuthTenant] = useState('e1b7c123-1234-4321-abcd-123456789abc');
  const [publicTenants, setPublicTenants] = useState<any[]>([]);
  const [authStatus, setAuthStatus] = useState<{ status: 'idle' | 'loading' | 'success' | 'error', message?: string }>({ status: 'idle' });
  const [apiHealth, setApiHealth] = useState<'checking' | 'online' | 'offline'>('checking');

  // Password Visibility & Confirmation States
  const [showLoginPassword, setShowLoginPassword] = useState(false);
  const [showSignupPassword, setShowSignupPassword] = useState(false);
  const [showSignupConfirmPassword, setShowSignupConfirmPassword] = useState(false);
  const [showAdminUserPassword, setShowAdminUserPassword] = useState(false);
  const [authConfirmPassword, setAuthConfirmPassword] = useState('');
  const [signupEmailError, setSignupEmailError] = useState('');
  const [signupPasswordError, setSignupPasswordError] = useState('');

  // Admin User Creation States
  const [adminUserEmail, setAdminUserEmail] = useState('');
  const [adminUserPassword, setAdminUserPassword] = useState('');
  const [adminUserName, setAdminUserName] = useState('');
  const [adminUserRole, setAdminUserRole] = useState('operator');
  const [adminUserTenantId, setAdminUserTenantId] = useState('e1b7c123-1234-4321-abcd-123456789abc');
  const [adminUserStatus, setAdminUserStatus] = useState<{ status: 'idle' | 'saving' | 'success' | 'error', message?: string }>({ status: 'idle' });

  const [tenants, setTenants] = useState<{ id: string, name: string, slug: string, logo_url?: string, primary_color?: string }[]>([]);
  const [selectedTenant, setSelectedTenant] = useState<any>({
    id: '',
    name: '',
    slug: ''
  });
  const [selectedTenantIds, setSelectedTenantIds] = useState<string[]>([]);
  const [isTenantDropdownOpen, setIsTenantDropdownOpen] = useState(false);
  const [newTenantName, setNewTenantName] = useState('');
  const [tenantCreateStatus, setTenantCreateStatus] = useState<{ status: 'idle' | 'saving' | 'success' | 'error', message?: string }>({ status: 'idle' });
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [selectedAlert, setSelectedAlert] = useState<Alert | null>(null);
  const [runbooks, setRunbooks] = useState<any[]>([]);
  const [runbookLogs, setRunbookLogs] = useState<string>('');
  const [isExecutingRunbook, setIsExecutingRunbook] = useState<boolean>(false);
  const [slaData, setSlaData] = useState<any | null>(null);
  const [cockpitTab, setCockpitTab] = useState<'alerts' | 'topology' | 'settings'>('alerts');
  const [isLoadingSla, setIsLoadingSla] = useState<boolean>(false);
  const [isWallboardMode, setIsWallboardMode] = useState<boolean>(false);
  const [connStatus, setConnStatus] = useState<'connecting' | 'connected' | 'disconnected'>('disconnected');
  const [searchTerm, setSearchTerm] = useState('');
  const [severityFilter, setSeverityFilter] = useState<string>('all');
  const [activeTab, setActiveTab] = useState<'general' | 'logs' | 'grafana' | 'raw' | 'timeline' | 'chat'>('general');
  const [comments, setComments] = useState<any[]>([]);
  const [chatPrompt, setChatPrompt] = useState('');
  const [isSendingChat, setIsSendingChat] = useState(false);
  const [isLoadingComments, setIsLoadingComments] = useState(false);
  const [reportMode, setReportMode] = useState<'executive' | 'technical'>('executive');
  const [vaultSecrets, setVaultSecrets] = useState<any[]>([]);
  const [isLoadingVaultSecrets, setIsLoadingVaultSecrets] = useState(false);
  const [runbookAudits, setRunbookAudits] = useState<any[]>([]);
  const [isLoadingRunbookAudits, setIsLoadingRunbookAudits] = useState(false);
  const [settingsPlaybooks, setSettingsPlaybooks] = useState<any[]>([]);
  const [isLoadingSettingsPlaybooks, setIsLoadingSettingsPlaybooks] = useState(false);
  const [playbookName, setPlaybookName] = useState('');
  const [playbookTrigger, setPlaybookTrigger] = useState('');
  const [playbookScript, setPlaybookScript] = useState('');
  const [playbookVaultKey, setPlaybookVaultKey] = useState('ssh');
  const [playbookStatus, setPlaybookStatus] = useState<{ status: 'idle' | 'saving' | 'success' | 'error', message?: string }>({ status: 'idle' });
  const [playbookIsGlobal, setPlaybookIsGlobal] = useState(false);
  const [simulatorNotification, setSimulatorNotification] = useState<string | null>(null);
  const [activeSummaryModal, setActiveSummaryModal] = useState<'total' | 'fatal' | 'critical' | 'warning' | 'info' | null>(null);
  
  // Shift Handover States
  const [activeHandover, setActiveHandover] = useState<any | null>(null);
  const [showHandoverModal, setShowHandoverModal] = useState(false);
  const [handoverSummary, setHandoverSummary] = useState('');
  const [handoverPendingAlerts, setHandoverPendingAlerts] = useState(0);
  const [isSubmittingHandover, setIsSubmittingHandover] = useState(false);
  
  // Integrations Modal States
  const [showIntegrationsModal, setShowIntegrationsModal] = useState(false);
  const [showActiveUsersModal, setShowActiveUsersModal] = useState(false);
  const [adminUsers, setAdminUsers] = useState<any[]>([]);
  const [isLoadingAdminUsers, setIsLoadingAdminUsers] = useState(false);
  const [activeUsers, setActiveUsers] = useState<any[]>([]);
  const [isLoadingActiveUsers, setIsLoadingActiveUsers] = useState(false);
  const [selectedIntegrationTool, setSelectedIntegrationTool] = useState<string>('integrations_admin');
  const [copiedText, setCopiedText] = useState(false);
  
  // Vault secret storage states
  const [vaultKey, setVaultKey] = useState('ssh_private_key');
  const [vaultValue, setVaultValue] = useState('');
  const [saveStatus, setSaveStatus] = useState<{ status: 'idle' | 'saving' | 'success' | 'error', message?: string }>({ status: 'idle' });

  // Integrations states
  const [integrations, setIntegrations] = useState<any[]>([]);
  const [integrationName, setIntegrationName] = useState('');
  const [integrationStatus, setIntegrationStatus] = useState<{ status: 'idle' | 'saving' | 'success' | 'error', message?: string }>({ status: 'idle' });

  // Admin Tenant integrations management states
  const [selectedAdminTenant, setSelectedAdminTenant] = useState<any | null>(null);
  const [adminIntegrationTool, setAdminIntegrationTool] = useState('zabbix');
  const [adminIntegrationName, setAdminIntegrationName] = useState('');
  const [adminIntegrations, setAdminIntegrations] = useState<any[]>([]);
  const [adminIntegrationStatus, setAdminIntegrationStatus] = useState<{ status: 'idle' | 'saving' | 'success' | 'error', message?: string }>({ status: 'idle' });
  const [validationResult, setValidationResult] = useState<any>(null);
  const [isValidating, setIsValidating] = useState(false);

  // Unified Connectors Health Center States
  const [connectorStatuses, setConnectorStatuses] = useState<Record<string, any>>({});
  const [activeSubTool, setActiveSubTool] = useState<string | null>(null);

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null);

  // Stats computation
  const stats = {
    total: alerts.filter(a => selectedTenantIds.includes(a.tenant_id) && a.status !== 'resolved' && a.status !== 'suppressed').length,
    fatal: alerts.filter(a => selectedTenantIds.includes(a.tenant_id) && a.severity === 'fatal' && a.status !== 'resolved' && a.status !== 'suppressed').length,
    critical: alerts.filter(a => selectedTenantIds.includes(a.tenant_id) && a.severity === 'critical' && a.status !== 'resolved' && a.status !== 'suppressed').length,
    warning: alerts.filter(a => selectedTenantIds.includes(a.tenant_id) && a.severity === 'warning' && a.status !== 'resolved' && a.status !== 'suppressed').length,
    info: alerts.filter(a => selectedTenantIds.includes(a.tenant_id) && a.severity === 'info' && a.status !== 'resolved' && a.status !== 'suppressed').length,
  };

  // API Connection Health Check Loop
  useEffect(() => {
    let active = true;
    const checkApiHealth = async () => {
      const controller = new AbortController();
      const id = setTimeout(() => controller.abort(), 4000);
      try {
        const response = await fetch(`${API_BASE_URL}/`, { method: 'GET', signal: controller.signal });
        clearTimeout(id);
        if (response.ok && active) {
          setApiHealth('online');
        } else if (active) {
          setApiHealth('offline');
        }
      } catch (err) {
        clearTimeout(id);
        if (active) {
          setApiHealth('offline');
        }
      }
    };

    checkApiHealth();
    const interval = setInterval(checkApiHealth, 20000); // Check every 20 seconds

    return () => {
      active = false;
      clearInterval(interval);
    };
  }, []);

  // Mount effect to load session cache
  useEffect(() => {
    const cachedToken = localStorage.getItem('noc_token');
    const cachedUser = localStorage.getItem('noc_user');
    const cachedTenant = localStorage.getItem('noc_tenant');
    if (cachedToken && cachedUser) {
      setToken(cachedToken);
      setUser(JSON.parse(cachedUser));
      if (cachedTenant) {
        setSelectedTenant(JSON.parse(cachedTenant));
      }
    } else {
      setToken(null);
      setUser(null);
    }
  }, []);

  const fetchTenants = async () => {
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/tenants`, {
        headers: {
          'Authorization': `Bearer ${token || 'bypass-token'}`
        }
      });
      if (response.ok) {
        const data = await response.json();
        if (Array.isArray(data) && data.length > 0) {
          setTenants(data);
          
          // Atualiza o tenant selecionado se ele não estiver no novo array
          const currentExists = data.some((t: any) => t.id === selectedTenant.id);
          if (!currentExists) {
            setSelectedTenant(data[0]);
          }

          // Define adminUserTenantId dinamicamente
          setAdminUserTenantId(data[0].id);

          // Inicializa selectedTenantIds para conter todos os tenants no primeiro carregamento
          if (selectedTenantIds.length === 0) {
            setSelectedTenantIds(data.map((t: any) => t.id));
          } else {
            // Filtra IDs antigos que possam ter sido removidos do banco
            const validIds = selectedTenantIds.filter(id => data.some((t: any) => t.id === id));
            if (validIds.length > 0) {
              setSelectedTenantIds(validIds);
            } else {
              setSelectedTenantIds([data[0].id]);
            }
          }
        }
      }
    } catch (err) {
      console.error("Falha ao buscar tenants:", err);
    }
  };

  const fetchIntegrations = async () => {
    if (!token || token === 'bypass-token') return;
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/integrations`, {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (response.ok) {
        const data = await response.json();
        setIntegrations(data);
      }
    } catch (err) {
      console.error("Falha ao buscar integrações:", err);
    }
  };

  const fetchActiveUsers = async () => {
    if (!token || user?.role !== 'admin') return;
    setIsLoadingActiveUsers(true);
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/ws/active_users`, {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (response.ok) {
        const data = await response.json();
        setActiveUsers(data);
      }
    } catch (err) {
      console.error("Falha ao buscar usuários ativos:", err);
    } finally {
      setIsLoadingActiveUsers(false);
    }
  };

  useEffect(() => {
    if (showActiveUsersModal) {
      fetchActiveUsers();
      const interval = setInterval(fetchActiveUsers, 10000);
      return () => clearInterval(interval);
    }
  }, [showActiveUsersModal, token]);

  const fetchAdminUsers = async () => {
    if (!token || user?.role !== 'admin') return;
    setIsLoadingAdminUsers(true);
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/admin/users`, {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (response.ok) {
        const data = await response.json();
        setAdminUsers(data);
      }
    } catch (err) {
      console.error("Falha ao buscar usuários:", err);
    } finally {
      setIsLoadingAdminUsers(false);
    }
  };

  const handleDeleteUser = async (id: string) => {
    if (!token) return;
    if (!confirm('Deseja excluir este usuário do NOC permanentemente?')) return;
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/admin/users?id=${id}`, {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (response.ok) {
        fetchAdminUsers();
      } else {
        const msg = await response.text();
        alert(msg || 'Falha ao excluir usuário.');
      }
    } catch (err) {
      console.error(err);
    }
  };

  const handleDeleteTenant = async (id: string) => {
    if (!token) return;
    if (!confirm('ATENÇÃO: A exclusão do tenant removerá todos os alertas, regras e conectores associados permanentemente! Deseja continuar?')) return;
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/tenants?id=${id}`, {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (response.ok) {
        await fetchTenants();
        setSelectedAdminTenant(null);
      } else {
        alert('Falha ao excluir tenant.');
      }
    } catch (err) {
      console.error(err);
    }
  };

  useEffect(() => {
    setValidationResult(null);
  }, [selectedIntegrationTool, selectedTenant, selectedAdminTenant, adminIntegrationTool]);

  useEffect(() => {
    if (selectedIntegrationTool === 'users_admin') {
      fetchAdminUsers();
    }
  }, [selectedIntegrationTool, token]);

  const handleCreateIntegrationSetting = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setIntegrationStatus({ status: 'saving' });
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/integrations`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({
          name: integrationName,
          type: selectedIntegrationTool,
          status: 'active'
        })
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
      const response = await fetch(`${API_BASE_URL}/api/v1/integrations?id=${id}`, {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (response.ok) {
        fetchIntegrations();
      } else {
        alert('Falha ao desativar integração.');
      }
    } catch (err) {
      console.error(err);
    }
  };

  useEffect(() => {
    if (token) {
      fetchTenants();
      fetchIntegrations();
    }
  }, [token, selectedTenant]);

  useEffect(() => {
    if (token && selectedTenant?.id && selectedIntegrationTool === 'integrations_admin') {
      fetchAllConnectorStatuses(selectedTenant.id);
    }
  }, [token, selectedTenant?.id, selectedIntegrationTool]);

  const fetchAdminTenantIntegrations = async (tenantId: string) => {
    if (!token) return;
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/integrations?tenant_id=${tenantId}`, {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (response.ok) {
        const data = await response.json();
        setAdminIntegrations(data);
      }
    } catch (err) {
      console.error("Falha ao buscar integrações do tenant admin:", err);
    }
  };

  const handleAdminCreateIntegration = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token || !selectedAdminTenant) return;
    setAdminIntegrationStatus({ status: 'saving' });
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/integrations?tenant_id=${selectedAdminTenant.id}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({
          name: adminIntegrationName,
          type: adminIntegrationTool,
          status: 'active'
        })
      });
      if (response.ok) {
        setAdminIntegrationStatus({ status: 'success', message: 'Integração ativada com sucesso!' });
        setAdminIntegrationName('');
        fetchAdminTenantIntegrations(selectedAdminTenant.id);
        fetchIntegrations(); // Refresh global integrations too
        setTimeout(() => setAdminIntegrationStatus({ status: 'idle' }), 3000);
      } else {
        const msg = await response.text();
        setAdminIntegrationStatus({ status: 'error', message: msg || 'Falha ao ativar integração.' });
      }
    } catch (err) {
      setAdminIntegrationStatus({ status: 'error', message: 'Erro de conexão com a API.' });
    }
  };

  const handleAdminDeleteIntegration = async (id: string) => {
    if (!token || !selectedAdminTenant) return;
    if (!confirm('Deseja desativar esta integração para o tenant selecionado?')) return;
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/integrations?id=${id}&tenant_id=${selectedAdminTenant.id}`, {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (response.ok) {
        fetchAdminTenantIntegrations(selectedAdminTenant.id);
        fetchIntegrations(); // Refresh global integrations too
      } else {
        alert('Falha ao desativar integração.');
      }
    } catch (err) {
      console.error(err);
    }
  };

  useEffect(() => {
    if (selectedAdminTenant) {
      fetchAdminTenantIntegrations(selectedAdminTenant.id);
    }
  }, [selectedAdminTenant]);

  const fetchPublicTenants = async () => {
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/public/tenants`);
      if (response.ok) {
        const data = await response.json();
        const validList = data || [];
        setPublicTenants(validList);
        if (validList.length > 0) {
          setAuthTenant(validList[0].id);
        }
      }
    } catch (err) {
      console.error("Falha ao buscar tenants públicos:", err);
    }
  };

  useEffect(() => {
    fetchPublicTenants();
  }, [authView]);

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setAuthStatus({ status: 'loading' });
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: authEmail, password: authPassword })
      });
      if (response.ok) {
        const data = await response.json();
        localStorage.setItem('noc_token', data.token);
        localStorage.setItem('noc_user', JSON.stringify(data.user));
        localStorage.setItem('noc_tenant', JSON.stringify(data.tenant));
        setToken(data.token);
        setUser(data.user);
        setSelectedTenant(data.tenant);
        setAuthStatus({ status: 'success' });
      } else {
        const contentType = response.headers.get('content-type');
        let msg = '';
        if (contentType && contentType.includes('text/html')) {
          msg = 'Erro de conexão: O servidor retornou uma página HTML. Verifique se a variável de ambiente NEXT_PUBLIC_API_URL do frontend está configurada corretamente com a URL da API Go.';
        } else {
          msg = await response.text();
        }
        setAuthStatus({ status: 'error', message: msg || 'Credenciais inválidas.' });
      }
    } catch (err) {
      setAuthStatus({ status: 'error', message: 'Falha ao se conectar com o servidor.' });
    }
  };

  const handleRegister = async (e: React.FormEvent) => {
    e.preventDefault();

    // 1. Validar e-mail corporativo format
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    if (!emailRegex.test(authEmail)) {
      setAuthStatus({ status: 'error', message: 'Por favor, informe um endereço de e-mail válido.' });
      return;
    }

    // 2. Validar tamanho da senha
    if (authPassword.length < 6) {
      setAuthStatus({ status: 'error', message: 'A senha deve ter pelo menos 6 caracteres.' });
      return;
    }

    // 3. Validar confirmação de senha
    if (authPassword !== authConfirmPassword) {
      setAuthStatus({ status: 'error', message: 'As senhas informadas não coincidem.' });
      return;
    }

    setAuthStatus({ status: 'loading' });
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/auth/register`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: authEmail, password: authPassword, name: authName })
      });
      if (response.ok) {
        const data = await response.json();
        if (data.auto_verified) {
          setAuthStatus({ status: 'success', message: 'Conta criada e ativada automaticamente com sucesso! Redirecionando para login...' });
          setTimeout(() => {
            setAuthView('login');
            setAuthStatus({ status: 'idle' });
          }, 3000);
        } else {
          setAuthStatus({ status: 'success', message: data.message || 'Conta criada! Por favor, verifique seu e-mail para ativar.' });
        }
        setAuthEmail('');
        setAuthPassword('');
        setAuthConfirmPassword('');
        setAuthName('');
      } else {
        const contentType = response.headers.get('content-type');
        let msg = '';
        if (contentType && contentType.includes('text/html')) {
          msg = 'Erro de conexão: O servidor retornou uma página HTML. Verifique se a variável de ambiente NEXT_PUBLIC_API_URL do frontend está configurada corretamente com a URL da API Go.';
        } else {
          msg = await response.text();
        }
        setAuthStatus({ status: 'error', message: msg || 'Falha ao registrar.' });
      }
    } catch (err) {
      setAuthStatus({ status: 'error', message: 'Falha ao se conectar com o servidor.' });
    }
  };

  const handleLogout = () => {
    localStorage.removeItem('noc_token');
    localStorage.removeItem('noc_user');
    localStorage.removeItem('noc_tenant');
    setToken(null);
    setUser(null);
    setAlerts([]);
  };

  const handleAdminCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    setAdminUserStatus({ status: 'saving' });
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/admin/users`, {
        method: 'POST',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({
          email: adminUserEmail,
          password: adminUserPassword,
          name: adminUserName,
          role: adminUserRole
        })
      });
      if (response.ok) {
        setAdminUserStatus({ status: 'success', message: 'Novo usuário cadastrado e e-mail enviado!' });
        setAdminUserEmail('');
        setAdminUserPassword('');
        setAdminUserName('');
        fetchAdminUsers();
      } else {
        const msg = await response.text();
        setAdminUserStatus({ status: 'error', message: msg || 'Falha ao cadastrar usuário.' });
      }
    } catch (err) {
      setAdminUserStatus({ status: 'error', message: 'Erro ao conectar ao backend.' });
    }
  };

  const handleCreateTenant = async (e: React.FormEvent) => {
    e.preventDefault();
    setTenantCreateStatus({ status: 'saving' });
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/tenants`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token || 'bypass-token'}`
        },
        body: JSON.stringify({ name: newTenantName })
      });
      if (response.ok) {
        const data = await response.json();
        setTenantCreateStatus({ status: 'success', message: `Tenant '${data.name}' criado com sucesso!` });
        setNewTenantName('');
        // Atualiza a lista de tenants dinamicamente
        await fetchTenants();
      } else {
        const msg = await response.text();
        setTenantCreateStatus({ status: 'error', message: msg || 'Falha ao criar tenant.' });
      }
    } catch (err) {
      setTenantCreateStatus({ status: 'error', message: 'Erro ao conectar ao backend.' });
    }
  };

  // Reset tab to general when selected incident changes
  useEffect(() => {
    if (selectedAlert) {
      setActiveTab('general');
    }
  }, [selectedAlert?.id]);

  // Synthesize custom SRE/SOC notification sounds using Web Audio API
  const playAlertSound = (severity: string) => {
    try {
      const audioCtx = new (window.AudioContext || (window as any).webkitAudioContext)();
      const osc1 = audioCtx.createOscillator();
      const osc2 = audioCtx.createOscillator();
      const gainNode = audioCtx.createGain();

      osc1.connect(gainNode);
      osc2.connect(gainNode);
      gainNode.connect(audioCtx.destination);

      if (severity === 'fatal') {
        osc1.frequency.setValueAtTime(880, audioCtx.currentTime); // A5
        osc2.frequency.setValueAtTime(1200, audioCtx.currentTime);
        gainNode.gain.setValueAtTime(0.15, audioCtx.currentTime);
        gainNode.gain.exponentialRampToValueAtTime(0.01, audioCtx.currentTime + 0.55);
        osc1.start();
        osc2.start();
        osc1.stop(audioCtx.currentTime + 0.55);
        osc2.stop(audioCtx.currentTime + 0.55);
      } else if (severity === 'critical') {
        osc1.frequency.setValueAtTime(587.33, audioCtx.currentTime); // D5
        osc2.frequency.setValueAtTime(698.46, audioCtx.currentTime); // F5
        gainNode.gain.setValueAtTime(0.1, audioCtx.currentTime);
        gainNode.gain.exponentialRampToValueAtTime(0.01, audioCtx.currentTime + 0.4);
        osc1.start();
        osc2.start();
        osc1.stop(audioCtx.currentTime + 0.4);
        osc2.stop(audioCtx.currentTime + 0.4);
      } else {
        osc1.frequency.setValueAtTime(523.25, audioCtx.currentTime); // C5
        gainNode.gain.setValueAtTime(0.05, audioCtx.currentTime);
        gainNode.gain.exponentialRampToValueAtTime(0.01, audioCtx.currentTime + 0.15);
        osc1.start();
        osc1.stop(audioCtx.currentTime + 0.15);
      }
    } catch (e) {
      console.warn("AudioContext audio blocker active:", e);
    }
  };

  // Load alerts history via HTTP (fail-safe fallback)
  const loadAlertsHistory = async () => {
    if (!token || selectedTenantIds.length === 0) return;
    try {
      const allFetchedAlerts: Alert[] = [];
      for (const tenantId of selectedTenantIds) {
        const response = await fetch(`${API_BASE_URL}/api/v1/alerts?tenant_id=${tenantId}`, {
          headers: {
            'Authorization': `Bearer ${token}`
          }
        });
        if (response.ok) {
          const data = await response.json();
          if (Array.isArray(data)) {
            allFetchedAlerts.push(...data);
          }
        }
      }
      
      const uniqueAlertsMap = new Map<string, Alert>();
      allFetchedAlerts.forEach(a => uniqueAlertsMap.set(a.id, a));
      const sortedAlerts = Array.from(uniqueAlertsMap.values()).sort(
        (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      );
      
      setAlerts(sortedAlerts);
    } catch (err) {
      console.error("Falha ao carregar histórico de alertas:", err);
    }
  };

  const handleCleanupMockAlerts = async () => {
    if (!token) return;
    if (!confirm("Tem certeza que deseja deletar todos os alertas mock/teste do sistema?")) return;
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/alerts/cleanup?tenant_id=${selectedTenant.id}`, {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (response.ok) {
        const data = await response.json();
        alert(`Limpeza concluída com sucesso! Alertas mock removidos: ${data.rows_affected}`);
        loadAlertsHistory();
      } else {
        alert("Falha ao limpar alertas mock.");
      }
    } catch (err) {
      console.error(err);
      alert("Erro na requisição de limpeza.");
    }
  };

  const handleValidateIntegration = async (type: string, isMsp: boolean = false) => {
    if (!token) return;
    const targetTenantId = isMsp ? selectedAdminTenant?.id : selectedTenant.id;
    if (!targetTenantId) return;
    setIsValidating(true);
    setValidationResult(null);
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/integrations/status?tenant_id=${targetTenantId}&type=${type}`, {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (response.ok) {
        const data = await response.json();
        setValidationResult(data);
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
    await Promise.all(tools.map(async (tool) => {
      try {
        const res = await fetch(`${API_BASE_URL}/api/v1/integrations/status?tenant_id=${tenantId}&type=${tool}`, {
          headers: { 'Authorization': `Bearer ${token}` }
        });
        if (res.ok) {
          results[tool] = await res.json();
        } else {
          results[tool] = { status: 'error', last_error: 'Falha ao conectar à API de status.' };
        }
      } catch (err) {
        console.error(`Error fetching status for ${tool}:`, err);
        results[tool] = { status: 'error', last_error: 'Erro de rede.' };
      }
    }));
    setConnectorStatuses(results);
  };

  // Connect to Go WebSocket Server
  const connectWebSocket = () => {
    if (!token) return;

    if (wsRef.current) {
      wsRef.current.close();
    }

    setConnStatus('connecting');
    const wsUrl = getWSUrl(token, selectedTenantIds);
    
    const socket = new WebSocket(wsUrl);
    wsRef.current = socket;

    socket.onopen = () => {
      setConnStatus('connected');
      console.log(`WebSocket connected to tenants: ${selectedTenantIds.join(', ')}`);
      loadAlertsHistory();
    };

    socket.onmessage = (event) => {
      try {
        const receivedAlert: Alert = JSON.parse(event.data);
        console.log("WebSocket event received:", receivedAlert);

        setAlerts(prevAlerts => {
          const index = prevAlerts.findIndex(a => a.id === receivedAlert.id);
          if (index !== -1) {
            // Update existing alert (deduplication/debounce update)
            const updated = [...prevAlerts];
            updated[index] = receivedAlert;
            
            // Sync selected alert detail if it's currently opened
            if (selectedAlert && selectedAlert.id === receivedAlert.id) {
              setSelectedAlert(receivedAlert);
            }
            return updated;
          } else {
            // Append new alert on top
            playAlertSound(receivedAlert.severity);
            return [receivedAlert, ...prevAlerts];
          }
        });
      } catch (err) {
        console.error("Failed to parse WebSocket message:", err);
      }
    };

    socket.onclose = () => {
      setConnStatus('disconnected');
      // Schedule automatic reconnection
      reconnectTimeoutRef.current = setTimeout(() => {
        console.log("Reconnecting WebSocket...");
        connectWebSocket();
      }, 3000);
    };

    socket.onerror = (err) => {
      console.error("WebSocket error:", err);
      socket.close();
    };
  };

  // Triggers reconnection when tenant changes or when token is acquired
  useEffect(() => {
    if (!token) return;
    setAlerts([]); // Clear previous tenant alerts on switch
    setSelectedAlert(null);
    loadAlertsHistory();
    connectWebSocket();

    return () => {
      if (wsRef.current) {
        wsRef.current.close();
      }
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
    };
  }, [selectedTenantIds, token]);

  // Fetch SLA stats when selected tenant or integration view changes to SLA
  useEffect(() => {
    if (!token || !selectedTenant || selectedIntegrationTool !== 'sla_report') return;
    const fetchSlaData = async () => {
      setIsLoadingSla(true);
      try {
        const res = await fetch(`${API_BASE_URL}/api/v1/reports/sla/stats?tenant_id=${selectedTenant.id}`, {
          headers: {
            'Authorization': `Bearer ${token}`
          }
        });
        if (res.ok) {
          const data = await res.json();
          setSlaData(data);
        }
      } catch (err) {
        console.error("Failed to fetch SLA data:", err);
      } finally {
        setIsLoadingSla(false);
      }
    };
    fetchSlaData();
  }, [selectedTenant, selectedIntegrationTool, token]);

  // Fetch incident timeline / comments
  useEffect(() => {
    if (!token || !selectedAlert) return;
    const fetchComments = async () => {
      setIsLoadingComments(true);
      try {
        const res = await fetch(`${API_BASE_URL}/api/v1/incidents/comments?incident_id=${selectedAlert.id}&tenant_id=${selectedAlert.tenant_id}`, {
          headers: {
            'Authorization': `Bearer ${token}`
          }
        });
        if (res.ok) {
          const data = await res.json();
          setComments(data || []);
        }
      } catch (err) {
        console.error("Failed to fetch comments:", err);
      } finally {
        setIsLoadingComments(false);
      }
    };
    fetchComments();
  }, [selectedAlert?.id, activeTab, token]);

  const handleSendChat = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedAlert || !chatPrompt || !token) return;
    setIsSendingChat(true);
    try {
      const res = await fetch(`${API_BASE_URL}/api/v1/incidents/chat?tenant_id=${selectedAlert.tenant_id}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({
          incident_id: selectedAlert.id,
          created_at: selectedAlert.created_at,
          prompt: chatPrompt
        })
      });
      if (res.ok) {
        setChatPrompt('');
        // Reload comments timeline
        const resComments = await fetch(`${API_BASE_URL}/api/v1/incidents/comments?incident_id=${selectedAlert.id}&tenant_id=${selectedAlert.tenant_id}`, {
          headers: {
            'Authorization': `Bearer ${token}`
          }
        });
        if (resComments.ok) {
          const data = await resComments.json();
          setComments(data || []);
        }
      }
    } catch (err) {
      console.error("Failed to send chat message:", err);
    } finally {
      setIsSendingChat(false);
    }
  };

  // Dynamic White-Label Theme logic
  useEffect(() => {
    if (selectedTenantIds.length === 1 && selectedTenant) {
      const activeT = tenants.find(t => t.id === selectedTenantIds[0]);
      if (activeT && activeT.primary_color) {
        document.documentElement.style.setProperty('--primary-color', activeT.primary_color);
        return;
      }
    }
    // Default theme color (violet-500)
    document.documentElement.style.setProperty('--primary-color', '#8b5cf6');
  }, [selectedTenant, selectedTenantIds, tenants]);

  // Shift Handover Handlers & Effect
  useEffect(() => {
    if (!token || !selectedTenant) return;
    const checkActiveHandover = async () => {
      try {
        const res = await fetch(`${API_BASE_URL}/api/v1/shift/handover/current?tenant_id=${selectedTenant.id}`, {
          headers: {
            'Authorization': `Bearer ${token}`
          }
        });
        if (res.ok) {
          const data = await res.json();
          if (data && data.status === 'pending') {
            setActiveHandover(data);
          } else {
            setActiveHandover(null);
          }
        }
      } catch (err) {
        console.error("Failed to check shift handover:", err);
      }
    };
    checkActiveHandover();
    // Poll every 60 seconds
    const interval = setInterval(checkActiveHandover, 60000);
    return () => clearInterval(interval);
  }, [token, selectedTenant]);

  const handleSubmitHandover = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token || !selectedTenant || !handoverSummary) return;
    setIsSubmittingHandover(true);
    try {
      const res = await fetch(`${API_BASE_URL}/api/v1/shift/handover?tenant_id=${selectedTenant.id}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({
          shift_summary: handoverSummary,
          pending_alerts_count: Number(handoverPendingAlerts)
        })
      });
      if (res.ok) {
        setHandoverSummary('');
        setHandoverPendingAlerts(0);
        setShowHandoverModal(false);
        alert("Passagem de bastão registrada com sucesso! O próximo operador verá o modal ao logar.");
      } else {
        const errText = await res.text();
        alert(`Erro ao salvar handover: ${errText}`);
      }
    } catch (err) {
      console.error("Failed to submit handover:", err);
    } finally {
      setIsSubmittingHandover(false);
    }
  };

  const handleAckHandover = async () => {
    if (!token || !selectedTenant || !activeHandover) return;
    try {
      const res = await fetch(`${API_BASE_URL}/api/v1/shift/handover/ack?tenant_id=${selectedTenant.id}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({
          handover_id: activeHandover.id
        })
      });
      if (res.ok) {
        setActiveHandover(null);
      } else {
        const errText = await res.text();
        alert(`Erro ao aceitar handover: ${errText}`);
      }
    } catch (err) {
      console.error("Failed to ack handover:", err);
    }
  };

  // Fetch Vault secrets metadata for admin view
  useEffect(() => {
    if (!token || !selectedTenant || selectedIntegrationTool !== 'vault_admin') return;
    const fetchVaultSecrets = async () => {
      setIsLoadingVaultSecrets(true);
      try {
        const res = await fetch(`${API_BASE_URL}/api/v1/vault/list?tenant_id=${selectedTenant.id}`, {
          headers: {
            'Authorization': `Bearer ${token}`
          }
        });
        if (res.ok) {
          const data = await res.json();
          setVaultSecrets(data || []);
        }
      } catch (err) {
        console.error("Failed to fetch vault secrets:", err);
      } finally {
        setIsLoadingVaultSecrets(false);
      }
    };
    fetchVaultSecrets();
  }, [selectedTenant, selectedIntegrationTool, token]);

  // Fetch Runbook execution audits for admin view
  useEffect(() => {
    if (!token || !selectedTenant || selectedIntegrationTool !== 'audit_admin') return;
    const fetchRunbookAudits = async () => {
      setIsLoadingRunbookAudits(true);
      try {
        const res = await fetch(`${API_BASE_URL}/api/v1/runbooks/audit?tenant_id=${selectedTenant.id}`, {
          headers: {
            'Authorization': `Bearer ${token}`
          }
        });
        if (res.ok) {
          const data = await res.json();
          setRunbookAudits(data || []);
        }
      } catch (err) {
        console.error("Failed to fetch runbook audits:", err);
      } finally {
        setIsLoadingRunbookAudits(false);
      }
    };
    fetchRunbookAudits();
  }, [selectedTenant, selectedIntegrationTool, token]);

  // Fetch playbooks for SRE Settings administration
  const fetchPlaybooksAdmin = async () => {
    if (!token || !selectedTenant) return;
    setIsLoadingSettingsPlaybooks(true);
    try {
      const res = await fetch(`${API_BASE_URL}/api/v1/runbooks?tenant_id=${selectedTenant.id}`, {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (res.ok) {
        const data = await res.json();
        setSettingsPlaybooks(data || []);
      }
    } catch (err) {
      console.error("Failed to fetch settings playbooks:", err);
    } finally {
      setIsLoadingSettingsPlaybooks(false);
    }
  };

  useEffect(() => {
    if (!token || !selectedTenant || selectedIntegrationTool !== 'playbooks_admin') return;
    fetchPlaybooksAdmin();
  }, [selectedTenant, selectedIntegrationTool, token]);

  const handleCreatePlaybook = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token || !selectedTenant) return;
    setPlaybookStatus({ status: 'saving' });
    try {
      const res = await fetch(`${API_BASE_URL}/api/v1/runbooks?tenant_id=${selectedTenant.id}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({
          name: playbookName,
          trigger_rule: playbookTrigger,
          script: playbookScript,
          vault_key_host: playbookVaultKey,
          is_global: playbookIsGlobal
        })
      });
      if (res.ok) {
        setPlaybookStatus({ status: 'success', message: 'Playbook criado com sucesso!' });
        setPlaybookName('');
        setPlaybookTrigger('');
        setPlaybookScript('');
        setPlaybookIsGlobal(false);
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
      const res = await fetch(`${API_BASE_URL}/api/v1/runbooks?tenant_id=${selectedTenant.id}&id=${id}`, {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (res.ok) {
        fetchPlaybooksAdmin();
      } else {
        const txt = await res.text();
        alert('Falha ao excluir playbook: ' + txt);
      }
    } catch (err) {
      alert('Erro ao excluir playbook: ' + err);
    }
  };

  // Fetch runbooks when selected alert changes
  useEffect(() => {
    if (!selectedAlert || !token) return;
    const fetchRunbooks = async () => {
      try {
        const res = await fetch(`${API_BASE_URL}/api/v1/runbooks?tenant_id=${selectedAlert.tenant_id}`, {
          headers: {
            'Authorization': `Bearer ${token}`
          }
        });
        if (res.ok) {
          const data = await res.json();
          setRunbooks(data || []);
        }
      } catch (err) {
        console.error("Failed to fetch runbooks:", err);
      }
    };
    fetchRunbooks();
    setRunbookLogs(''); // Reset logs when changing selected alert
  }, [selectedAlert, token]);

  const handleExecuteRunbook = async (runbookId: string) => {
    if (!selectedAlert || !token) return;
    setIsExecutingRunbook(true);
    setRunbookLogs('Iniciando conexão remota via túnel seguro SSH...\nExecutando playbook de auto-cura...\n');

    try {
      const res = await fetch(`${API_BASE_URL}/api/v1/runbooks/execute?tenant_id=${selectedAlert.tenant_id}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({
          runbook_id: runbookId,
          incident_id: selectedAlert.id
        })
      });

      const data = await res.json();
      if (res.ok) {
        setRunbookLogs(prev => prev + `[Sucesso] Executado com sucesso.\n\nRetorno SSH:\n${data.output}`);
      } else {
        setRunbookLogs(prev => prev + `[Falha] Erro na execução:\n${data.message || data.output}`);
      }
    } catch (err) {
      setRunbookLogs(prev => prev + `[Erro de Rede] Não foi possível conectar ao backend.`);
    } finally {
      setIsExecutingRunbook(false);
    }
  };

  // Handle action triggers (sync status with backend REST API)
  const handleUpdateStatus = async (alertId: string, newStatus: Alert['status']) => {
    // 1. Update local state immediately for a responsive UI
    setAlerts(prevAlerts => {
      return prevAlerts.map(a => {
        if (a.id === alertId) {
          return {
            ...a,
            status: newStatus,
            resolved_at: newStatus === 'resolved' ? new Date().toISOString() : undefined,
            acknowledged_at: newStatus === 'acknowledged' ? new Date().toISOString() : a.acknowledged_at,
            updated_at: new Date().toISOString()
          };
        }
        return a;
      });
    });

    if (selectedAlert && selectedAlert.id === alertId) {
      setSelectedAlert(prev => prev ? {
        ...prev,
        status: newStatus,
        resolved_at: newStatus === 'resolved' ? new Date().toISOString() : undefined,
        acknowledged_at: newStatus === 'acknowledged' ? new Date().toISOString() : prev.acknowledged_at,
        updated_at: new Date().toISOString()
      } : null);
    }

    // 2. Fetch API to sync DB
    const endpoint = newStatus === 'acknowledged' ? '/api/v1/incidents/acknowledge' : '/api/v1/incidents/resolve';
    const alertItem = alerts.find(a => a.id === alertId);
    if (!alertItem) return;

    try {
      const res = await fetch(`${API_BASE_URL}${endpoint}?tenant_id=${alertItem.tenant_id}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({
          id: alertId,
          created_at: alertItem.created_at
        })
      });
      if (!res.ok) {
        console.error("Failed to update status on server:", await res.text());
      }
    } catch (err) {
      console.error("Network error updating incident status:", err);
    }
  };

  // Custom markdown formatter for AI reports
  const formatMarkdown = (text: string) => {
    if (!text) return null;
    return text.split('\n').map((line, idx) => {
      const cleanLine = line.trim();
      if (cleanLine.startsWith('### ')) {
        return <h4 key={idx} className="text-xs font-bold text-slate-200 mt-3 mb-1">{cleanLine.replace('### ', '')}</h4>;
      }
      if (cleanLine.startsWith('## ')) {
        return <h3 key={idx} className="text-xs font-extrabold text-violet-300 mt-4 mb-1.5">{cleanLine.replace('## ', '')}</h3>;
      }
      if (cleanLine.startsWith('# ')) {
        return <h2 key={idx} className="text-sm font-black text-white mt-5 mb-2">{cleanLine.replace('# ', '')}</h2>;
      }
      if (cleanLine.startsWith('- ') || cleanLine.startsWith('* ')) {
        return <li key={idx} className="text-xs text-slate-300 ml-4 list-disc space-y-1">{cleanLine.substring(2)}</li>;
      }
      if (cleanLine.startsWith('1. ') || cleanLine.startsWith('2. ') || cleanLine.startsWith('3. ')) {
        return <li key={idx} className="text-xs text-slate-300 ml-4 list-decimal space-y-1">{cleanLine.substring(cleanLine.indexOf('.') + 1).trim()}</li>;
      }
      if (!cleanLine) return <div key={idx} className="h-1.5" />;
      return <p key={idx} className="text-xs text-slate-300 mb-1 leading-relaxed">{cleanLine}</p>;
    });
  };

  // Filter alerts based on search and buttons
  const filteredAlerts = alerts.filter(a => {
    const matchesSearch = a.summary.toLowerCase().includes(searchTerm.toLowerCase()) || 
                          a.event_type.toLowerCase().includes(searchTerm.toLowerCase()) ||
                          (a.ai_analysis?.source || '').toLowerCase().includes(searchTerm.toLowerCase());
    const matchesSeverity = severityFilter === 'all' || a.severity === severityFilter;
    const matchesTenant = selectedTenantIds.includes(a.tenant_id);
    const isActive = a.status !== 'resolved' && a.status !== 'suppressed';
    return matchesSearch && matchesSeverity && matchesTenant && isActive;
  });

  // Simulator helper function
  const simulateEvent = async (type: 'cpu' | 'memory' | 'wazuh') => {
    try {
      const targetTenantId = selectedTenantIds.length > 0 ? selectedTenantIds[0] : selectedTenant.id;
      let url = '';
      let payload: any = {};
      
      if (type === 'cpu') {
        url = `${API_BASE_URL}/api/v1/webhook/prometheus/${targetTenantId}`;
        payload = {
          receiver: "webhook",
          status: "firing",
          alerts: [{
            status: "firing",
            labels: { alertname: "HostHighCpuLoad", instance: "web-server-99", severity: "critical" },
            annotations: { summary: "High CPU load on web-server-99", description: "CPU utilization has reached 98%." },
            startsAt: new Date().toISOString(),
            fingerprint: "cpu-spike-" + Date.now()
          }]
        };
      } else if (type === 'memory') {
        url = `${API_BASE_URL}/api/v1/webhook/prometheus/${targetTenantId}`;
        payload = {
          receiver: "webhook",
          status: "firing",
          alerts: [{
            status: "firing",
            labels: { alertname: "OOMKillerTriggered", instance: "db-node-03", severity: "critical" },
            annotations: { summary: "Out of Memory Killer activated", description: "System ran out of memory, postgres process killed." },
            startsAt: new Date().toISOString(),
            fingerprint: "oom-spike-" + Date.now()
          }]
        };
      } else if (type === 'wazuh') {
        url = `${API_BASE_URL}/api/v1/webhook/wazuh/${targetTenantId}`;
        payload = {
          timestamp: new Date().toISOString(),
          rule: { level: 10, comment: "SSH brute force authentication failed", sid: 5716, id: "5716", groups: ["syslog", "sshd", "security_event"] },
          agent: { id: "005", name: "auth-gateway", ip: "10.0.0.5" },
          location: "/var/log/auth.log",
          full_log: "Failed password for root from 203.0.113.5 port 55667 ssh2"
        };
      }

      const response = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (response.ok) {
        setSimulatorNotification(`Simulação de tipo ${type.toUpperCase()} enviada ao pipeline!`);
        setTimeout(() => setSimulatorNotification(null), 4000);
      } else {
        setSimulatorNotification("Erro ao despachar evento simulado.");
        setTimeout(() => setSimulatorNotification(null), 4000);
      }
    } catch (err) {
      console.error("Simulation dispatch failed:", err);
      setSimulatorNotification("Falha de rede na simulação.");
      setTimeout(() => setSimulatorNotification(null), 4000);
    }
  };

  // Secure Vault credential saver
  const handleSaveVaultSecret = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!vaultValue) return;

    setSaveStatus({ status: 'saving' });
    try {
      const url = `${API_BASE_URL}/api/v1/vault/secret?token=${selectedTenant.id}`;
      const response = await fetch(url, {
        method: 'POST',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({ key: vaultKey, value: vaultValue })
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



  if (!token) {
    return (
      <div className="min-h-screen bg-[#070b13] text-slate-100 flex items-center justify-center font-sans p-4 relative overflow-hidden">
        <div className="absolute top-1/4 left-1/4 -translate-x-1/2 -translate-y-1/2 w-96 h-96 rounded-full bg-violet-600/10 blur-[100px] pointer-events-none"></div>
        <div className="absolute bottom-1/4 right-1/4 translate-x-1/2 translate-y-1/2 w-96 h-96 rounded-full bg-cyan-600/10 blur-[100px] pointer-events-none"></div>

        <div className="glass-card w-full max-w-md border border-white/10 rounded-2xl shadow-2xl p-8 relative z-10 bg-slate-900/60 backdrop-blur-md">
          <div className="flex flex-col items-center gap-2 mb-8 text-center">
            <div className="relative flex items-center justify-center h-16 w-52 overflow-hidden rounded-xl bg-white/5 p-2 border border-white/10 mb-2">
              <img 
                src="https://lirp.cdn-website.com/2cf4cfdc/dms3rep/multi/opt/IT+Facil+-+logo+-+alta-47c0885e-158w.png" 
                alt="ITFácil Logo" 
                className="h-full w-auto object-contain"
              />
            </div>
            <h1 className="text-xl font-bold uppercase tracking-wider text-white">ITFácil NOC</h1>
            <p className="text-xs text-slate-400">Painel SRE Multi-tenant de Gerenciamento & Auto-cura</p>
            {apiHealth === 'offline' && (
              <div className="mt-4 px-3 py-1.5 rounded-lg bg-rose-500/10 border border-rose-500/20 text-rose-400 text-[10px] flex items-center gap-1.5 animate-pulse">
                <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
                <span>API do NOC indisponível ou em manutenção.</span>
              </div>
            )}
          </div>

          {typeof window !== 'undefined' && window.location.search.includes('verified=true') && (
            <div className="mb-6 p-3 rounded-lg bg-emerald-950/20 border border-emerald-500/20 text-emerald-400 text-xs flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 shrink-0" />
              <span>E-mail verificado com sucesso! Você já pode realizar o login.</span>
            </div>
          )}

          <div className="flex border-b border-white/5 mb-6">
            <button
              onClick={() => { setAuthView('login'); setAuthStatus({ status: 'idle' }); }}
              className={`flex-1 pb-3 text-sm font-bold transition-all ${authView === 'login' ? 'text-violet-400 border-b-2 border-violet-500' : 'text-slate-400 hover:text-slate-200'}`}
            >
              Acessar Cockpit
            </button>
            <button
              onClick={() => { setAuthView('register'); setAuthStatus({ status: 'idle' }); }}
              className={`flex-1 pb-3 text-sm font-bold transition-all ${authView === 'register' ? 'text-violet-400 border-b-2 border-violet-500' : 'text-slate-400 hover:text-slate-200'}`}
            >
              Criar Conta
            </button>
          </div>

          <form onSubmit={authView === 'login' ? handleLogin : handleRegister} className="flex flex-col gap-4">
            {authView === 'register' && (
              <div className="flex flex-col gap-1.5">
                <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Nome Completo</label>
                <input
                  type="text"
                  required
                  value={authName}
                  onChange={(e) => setAuthName(e.target.value)}
                  placeholder="Seu nome"
                  className="bg-black/30 border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500 transition-all placeholder:text-slate-600"
                />
              </div>
            )}

            <div className="flex flex-col gap-1.5">
              <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">E-mail Corporativo</label>
              <input
                type="email"
                required
                value={authEmail}
                onChange={(e) => {
                  setAuthEmail(e.target.value);
                  if (authView === 'register') {
                    const regex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
                    if (e.target.value && !regex.test(e.target.value)) {
                      setSignupEmailError('Formato de e-mail inválido');
                    } else {
                      setSignupEmailError('');
                    }
                  }
                }}
                placeholder="seu-nome@empresa.com"
                className="bg-black/30 border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500 transition-all placeholder:text-slate-600"
              />
              {authView === 'register' && signupEmailError && (
                <span className="text-[10px] text-rose-400 font-medium px-1 mt-0.5">{signupEmailError}</span>
              )}
            </div>

            <div className="flex flex-col gap-1.5">
              <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Senha</label>
              <div className="relative flex items-center">
                <input
                  type={authView === 'login' ? (showLoginPassword ? 'text' : 'password') : (showSignupPassword ? 'text' : 'password')}
                  required
                  value={authPassword}
                  onChange={(e) => {
                    setAuthPassword(e.target.value);
                    if (authView === 'register') {
                      if (e.target.value && e.target.value.length < 6) {
                        setSignupPasswordError('A senha deve ter pelo menos 6 caracteres');
                      } else {
                        setSignupPasswordError('');
                      }
                    }
                  }}
                  placeholder={authView === 'login' ? 'Sua senha' : 'Mínimo de 6 caracteres'}
                  className="w-full bg-black/30 border border-white/10 rounded-lg p-2.5 pr-10 text-xs text-white focus:outline-none focus:border-violet-500 transition-all placeholder:text-slate-600"
                />
                <button
                  type="button"
                  onClick={() => {
                    if (authView === 'login') setShowLoginPassword(!showLoginPassword);
                    else setShowSignupPassword(!showSignupPassword);
                  }}
                  className="absolute right-3 text-slate-400 hover:text-white transition-all cursor-pointer"
                >
                  {authView === 'login' ? (
                    showLoginPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />
                  ) : (
                    showSignupPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />
                  )}
                </button>
              </div>
              {authView === 'register' && signupPasswordError && (
                <span className="text-[10px] text-rose-400 font-medium px-1 mt-0.5">{signupPasswordError}</span>
              )}
            </div>

            {authView === 'register' && (
              <div className="flex flex-col gap-1.5">
                <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Confirmar Senha</label>
                <div className="relative flex items-center">
                  <input
                    type={showSignupConfirmPassword ? 'text' : 'password'}
                    required
                    value={authConfirmPassword}
                    onChange={(e) => setAuthConfirmPassword(e.target.value)}
                    placeholder="Repita sua senha"
                    className="w-full bg-black/30 border border-white/10 rounded-lg p-2.5 pr-10 text-xs text-white focus:outline-none focus:border-violet-500 transition-all placeholder:text-slate-600"
                  />
                  <button
                    type="button"
                    onClick={() => setShowSignupConfirmPassword(!showSignupConfirmPassword)}
                    className="absolute right-3 text-slate-400 hover:text-white transition-all cursor-pointer"
                  >
                    {showSignupConfirmPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
                {authConfirmPassword && authPassword !== authConfirmPassword && (
                  <span className="text-[10px] text-rose-400 font-medium px-1 mt-0.5">As senhas não coincidem.</span>
                )}
              </div>
            )}

            <button
              type="submit"
              disabled={authStatus.status === 'loading'}
              className="w-full bg-gradient-to-r from-violet-600 to-indigo-600 hover:from-violet-500 hover:to-indigo-500 text-white font-bold text-xs py-3 rounded-lg mt-2 transition-all shadow-md shadow-violet-950/40 flex items-center justify-center gap-2 cursor-pointer"
            >
              {authStatus.status === 'loading' && <RefreshCw className="w-4 h-4 animate-spin" />}
              {authView === 'login' ? 'Entrar no Painel' : 'Registrar Minha Conta'}
            </button>

            {authStatus.status === 'success' && authStatus.message && (
              <div className="p-3 bg-emerald-950/20 border border-emerald-500/20 text-emerald-400 text-xs rounded-lg mt-2 font-sans">
                {authStatus.message}
              </div>
            )}

            {authStatus.status === 'error' && authStatus.message && (
              <div className="p-3 bg-rose-950/20 border border-rose-500/20 text-rose-400 text-xs rounded-lg mt-2 font-sans">
                {authStatus.message}
              </div>
            )}
          </form>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-background text-slate-100 flex flex-col font-sans select-none">
      {apiHealth === 'offline' && (
        <div className="bg-rose-500/10 border-b border-rose-500/20 text-rose-400 py-1.5 px-4 text-center text-xs flex items-center justify-center gap-2 relative z-[100] animate-pulse">
          <AlertTriangle className="w-4 h-4 shrink-0" />
          <span>Conectividade instável com o servidor central da ITFácil. Algumas ações podem falhar. Tentando restabelecer conexão...</span>
        </div>
      )}
      
      {/* 1. Header (Navbar Glass) */}
      {isWallboardMode ? (
        <div className="h-14 shrink-0 bg-[#0e1626] border-b border-white/5 flex items-center justify-between px-6 text-xs select-none">
          <div className="flex items-center gap-2.5 text-rose-400 font-extrabold uppercase tracking-widest animate-pulse text-[10px]">
            <Activity className="w-4 h-4 text-rose-500" /> MONITOR DE SOC CENTRAL & NOC (MODO TV ATIVO)
          </div>
          <div className="flex items-center gap-6">
            <span className="text-slate-400 font-bold uppercase text-[9px] tracking-wider">Acordo de Nível de Serviço (SLA): <strong className="text-emerald-400 ml-1">99.98%</strong></span>
            <span className="text-slate-400 font-bold uppercase text-[9px] tracking-wider">Alertas Fatais: <strong className="text-rose-500 ml-1">{stats.fatal}</strong></span>
            <button
              onClick={() => setIsWallboardMode(false)}
              className="bg-rose-600 hover:bg-rose-500 text-white font-bold px-3.5 py-1.5 rounded transition-all uppercase text-[9px] tracking-wider cursor-pointer"
            >
              Sair do Modo TV
            </button>
          </div>
        </div>
      ) : (
        <header className="h-16 shrink-0 flex items-center justify-between px-6 border-b border-white/5 bg-surface/50 backdrop-blur-md sticky top-0 z-50">
        <div className="flex items-center gap-3">
          <div className="relative flex items-center justify-center h-11 w-36 overflow-hidden rounded-lg bg-white/5 p-1 border border-white/10">
            <img 
              src={
                selectedTenantIds.length === 1 && tenants.find(t => t.id === selectedTenantIds[0])?.logo_url
                  ? tenants.find(t => t.id === selectedTenantIds[0])?.logo_url
                  : "https://lirp.cdn-website.com/2cf4cfdc/dms3rep/multi/opt/IT+Facil+-+logo+-+alta-47c0885e-158w.png"
              } 
              alt="Brand Logo" 
              className="h-full w-auto object-contain"
            />
          </div>
          <div>
            <h1 className="text-sm font-bold tracking-wider text-slate-100 flex items-center gap-2">
              ITFácil NOC <span className="text-xs px-2 py-0.5 rounded-full bg-violet-900/60 border border-violet-500/30 text-violet-300">2.0 ENGINE</span>
            </h1>
            <p className="text-[10px] text-slate-400 tracking-wide uppercase">Real-Time Cockpit</p>
          </div>
        </div>

        {/* Dynamic Tenant Selector (Multi-tenancy Visual Testbench) */}
        <div className="flex items-center gap-4">
          {user?.role === 'admin' ? (
            <div className="relative">
              <button
                onClick={() => setIsTenantDropdownOpen(!isTenantDropdownOpen)}
                className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-white/5 border border-white/5 text-xs text-slate-300 font-bold hover:bg-white/10 transition-all select-none cursor-pointer"
              >
                <User className="w-3.5 h-3.5 text-violet-400" />
                <span>Visual Domain:</span>
                <span className="text-violet-400 font-extrabold">
                  {selectedTenantIds.length === tenants.length
                    ? "Multi-Tenant (Todos)"
                    : selectedTenantIds.length === 1
                    ? tenants.find(t => t.id === selectedTenantIds[0])?.name || "1 Selecionado"
                    : `${selectedTenantIds.length} Empresas`}
                </span>
                <ChevronDown className="w-3 h-3 text-slate-400" />
              </button>

              {isTenantDropdownOpen && (
                <>
                  {/* Backdrop overlay to close when clicking outside */}
                  <div className="fixed inset-0 z-40" onClick={() => setIsTenantDropdownOpen(false)}></div>
                  <div className="absolute right-0 mt-2 w-64 rounded-xl border border-white/10 bg-slate-950 p-2 shadow-2xl z-50 flex flex-col gap-1 backdrop-blur-md">
                    <div className="px-3 py-1 text-[9px] font-bold text-slate-500 uppercase tracking-widest border-b border-white/5 mb-1 flex items-center justify-between">
                      <span>Selecionar Empresas</span>
                      <button
                        onClick={() => {
                          setSelectedTenantIds(tenants.map(t => t.id));
                          if (tenants.length > 0) {
                            setSelectedTenant(tenants[0]);
                          }
                        }}
                        className="text-[9px] text-cyan-400 hover:text-cyan-300 uppercase font-bold"
                      >
                        Marcar Todas
                      </button>
                    </div>
                    <div className="flex flex-col max-h-48 overflow-y-auto pr-1">
                      {tenants.map(t => {
                        const isChecked = selectedTenantIds.includes(t.id);
                        return (
                          <label
                            key={t.id}
                            className={`flex items-center gap-2.5 px-2.5 py-2 rounded-lg cursor-pointer transition-all hover:bg-white/[0.03] select-none text-xs font-medium ${
                              isChecked ? 'text-white' : 'text-slate-400'
                            }`}
                          >
                            <input
                              type="checkbox"
                              checked={isChecked}
                              onChange={() => {
                                let newIds = [...selectedTenantIds];
                                if (isChecked) {
                                  if (newIds.length > 1) {
                                    newIds = newIds.filter(id => id !== t.id);
                                  }
                                } else {
                                  newIds.push(t.id);
                                }
                                setSelectedTenantIds(newIds);
                                const firstTenant = tenants.find(x => x.id === newIds[0]);
                                if (firstTenant) {
                                  setSelectedTenant(firstTenant);
                                }
                              }}
                              className="rounded border-white/10 bg-black/40 text-violet-600 focus:ring-violet-500 w-3.5 h-3.5"
                            />
                            <span className="truncate">{t.name}</span>
                          </label>
                        );
                      })}
                    </div>
                  </div>
                </>
              )}
            </div>
          ) : (
            <div className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-white/5 border border-white/5">
              <User className="w-4 h-4 text-slate-400" />
              <span className="text-xs text-slate-300 font-medium">Tenant:</span>
              <span className="text-xs text-violet-400 font-bold">{selectedTenant.name}</span>
            </div>
          )}

          {/* Connections / Integrations Manager Trigger (Hidden for viewers) */}
          {user?.role !== 'viewer' && (
            <button
              onClick={() => {
                setCockpitTab('settings');
                setSelectedIntegrationTool('integrations_admin');
                setActiveSubTool(null);
              }}
              className="flex items-center gap-2 px-3 py-1 rounded-lg bg-cyan-600/20 hover:bg-cyan-600/30 border border-cyan-500/35 text-cyan-300 text-xs font-bold transition-all uppercase tracking-wider"
            >
              <LinkIcon className="w-3.5 h-3.5" />
              <span>Integrações</span>
            </button>
          )}

          {/* Active Users Modal Trigger (Admin Only) */}
          {user?.role === 'admin' && (
            <button
              onClick={() => setShowActiveUsersModal(true)}
              className="flex items-center gap-2 px-3 py-1 rounded-lg bg-emerald-600/20 hover:bg-emerald-600/30 border border-emerald-500/35 text-emerald-300 text-xs font-bold transition-all uppercase tracking-wider"
            >
              <Users className="w-3.5 h-3.5" />
              <span>Operadores Online</span>
            </button>
          )}

          {/* Shift Handover Pass button */}
          {user?.role !== 'viewer' && (
            <button
              onClick={() => setShowHandoverModal(true)}
              className="flex items-center gap-2 px-3 py-1 rounded-lg bg-rose-600/20 hover:bg-rose-600/30 border border-rose-500/35 text-rose-300 text-xs font-bold transition-all uppercase tracking-wider"
            >
              <Clock className="w-3.5 h-3.5" />
              <span>Passar Turno</span>
            </button>
          )}

          {/* SLA PDF Report Downloader (Hidden for viewers) */}
          {user?.role !== 'viewer' && (
            <button
              onClick={() => {
                window.open(`${API_BASE_URL}/api/v1/reports/sla?token=${token || selectedTenant.id}&tenant_name=${encodeURIComponent(selectedTenant.name)}`);
              }}
              className="flex items-center gap-2 px-3 py-1 rounded-lg bg-violet-600/20 hover:bg-violet-600/30 border border-violet-500/35 text-violet-300 text-xs font-bold transition-all uppercase tracking-wider"
            >
              <FileText className="w-3.5 h-3.5" />
              <span>SLA Report</span>
            </button>
          )}

          {/* TV Wallboard Toggle */}
          <button
            onClick={() => setIsWallboardMode(true)}
            className="flex items-center gap-2 px-3 py-1 rounded-lg bg-rose-600/20 hover:bg-rose-600/30 border border-rose-500/35 text-rose-300 text-xs font-bold transition-all uppercase tracking-wider cursor-pointer"
            title="Alternar Modo TV Wallboard"
          >
            <Activity className="w-3.5 h-3.5 animate-pulse" />
            <span>Modo TV</span>
          </button>

          {/* User Profile and Logout */}
          <div className="flex items-center gap-3 px-3 py-1.5 rounded-lg bg-white/5 border border-white/5 ml-2">
            <div className="flex flex-col text-right">
              <span className="text-[10px] text-white font-bold leading-none">{user?.name}</span>
              <div className="flex items-center gap-1.5 justify-end mt-0.5">
                <span className="text-[8px] text-slate-400 uppercase tracking-widest font-semibold">{user?.role}</span>
                <span className="text-[8px] text-slate-500">•</span>
                <span className={`text-[8px] font-bold uppercase tracking-wider flex items-center gap-1 ${
                  connStatus === 'connected' 
                    ? 'text-emerald-400' 
                    : connStatus === 'connecting'
                      ? 'text-amber-400'
                      : 'text-rose-400'
                }`}>
                  <span className={`w-1 h-1 rounded-full ${
                    connStatus === 'connected' 
                      ? 'bg-emerald-400 animate-pulse' 
                      : connStatus === 'connecting'
                        ? 'bg-amber-400 animate-pulse'
                        : 'bg-rose-400'
                  }`} />
                  {connStatus === 'connected' ? 'On' : connStatus === 'connecting' ? '...' : 'Off'}
                </span>
              </div>
            </div>
            <button
              onClick={handleLogout}
              className="text-[9px] text-rose-400 hover:text-rose-300 bg-rose-500/10 hover:bg-rose-500/20 px-2 py-1 rounded transition-all font-bold cursor-pointer"
            >
              Sair
            </button>
          </div>
        </div>
       </header>
      )}

      {/* 2. Main Dashboard Layout */}
      <main className="flex-1 flex overflow-hidden">
        
        {/* Left Section (Stats + Alerts Feed) */}
        <section className={`flex-1 flex flex-col p-6 overflow-y-auto gap-6 w-full ${isWallboardMode ? 'max-w-none' : 'max-w-7xl mx-auto'}`}>
          
          {/* Stat Cards */}
          <div className="grid grid-cols-5 gap-4">
            <div className="glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer hover:border-violet-500/35 transition-all" onClick={() => { setSeverityFilter('all'); setActiveSummaryModal('total'); }}>
              <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
                <Layers className="w-3.5 h-3.5 text-violet-400" /> Active Alerts
              </span>
              <span className="text-3xl font-extrabold tracking-tight text-white">{stats.total}</span>
              <div className="h-1 bg-violet-600/30 rounded mt-2 overflow-hidden">
                <div className="h-full bg-violet-500 rounded" style={{ width: '100%' }}></div>
              </div>
            </div>

            <div className={`glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer transition-all hover:border-rose-500/35 ${
              severityFilter === 'fatal' ? 'glass-card-active border-severity-fatal/50' : ''
            }`} onClick={() => { setSeverityFilter('fatal'); setActiveSummaryModal('fatal'); }}>
              <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
                <AlertOctagon className="w-3.5 h-3.5 text-severity-fatal" /> Fatal Issues
              </span>
              <span className={`text-3xl font-extrabold tracking-tight ${stats.fatal > 0 ? 'text-severity-fatal animate-pulse' : 'text-white'}`}>
                {stats.fatal}
              </span>
              <div className="h-1 bg-severity-fatal/20 rounded mt-2 overflow-hidden">
                <div className="h-full bg-severity-fatal rounded" style={{ width: stats.total > 0 ? `${(stats.fatal / stats.total) * 100}%` : '0%' }}></div>
              </div>
            </div>

            <div className={`glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer transition-all hover:border-orange-500/35 ${
              severityFilter === 'critical' ? 'glass-card-active border-severity-critical/50' : ''
            }`} onClick={() => { setSeverityFilter('critical'); setActiveSummaryModal('critical'); }}>
              <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
                <AlertOctagon className="w-3.5 h-3.5 text-severity-critical" /> Critical
              </span>
              <span className="text-3xl font-extrabold tracking-tight text-white">{stats.critical}</span>
              <div className="h-1 bg-severity-critical/20 rounded mt-2 overflow-hidden">
                <div className="h-full bg-severity-critical rounded" style={{ width: stats.total > 0 ? `${(stats.critical / stats.total) * 100}%` : '0%' }}></div>
              </div>
            </div>

            <div className={`glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer transition-all hover:border-amber-500/35 ${
              severityFilter === 'warning' ? 'glass-card-active border-severity-warning/50' : ''
            }`} onClick={() => { setSeverityFilter('warning'); setActiveSummaryModal('warning'); }}>
              <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
                <AlertTriangle className="w-3.5 h-3.5 text-severity-warning" /> Warnings
              </span>
              <span className="text-3xl font-extrabold tracking-tight text-white">{stats.warning}</span>
              <div className="h-1 bg-severity-warning/20 rounded mt-2 overflow-hidden">
                <div className="h-full bg-severity-warning rounded" style={{ width: stats.total > 0 ? `${(stats.warning / stats.total) * 100}%` : '0%' }}></div>
              </div>
            </div>

            <div className={`glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer transition-all hover:border-blue-500/35 ${
              severityFilter === 'info' ? 'glass-card-active border-severity-info/50' : ''
            }`} onClick={() => { setSeverityFilter('info'); setActiveSummaryModal('info'); }}>
              <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
                <Info className="w-3.5 h-3.5 text-severity-info" /> Informational
              </span>
              <span className="text-3xl font-extrabold tracking-tight text-white">{stats.info}</span>
              <div className="h-1 bg-severity-info/20 rounded mt-2 overflow-hidden">
                <div className="h-full bg-severity-info rounded" style={{ width: stats.total > 0 ? `${(stats.info / stats.total) * 100}%` : '0%' }}></div>
              </div>
            </div>
          </div>

          {/* AIOps Predictive Analytics Baseline Widget */}
          <div className="glass-card p-5 rounded-xl border border-white/5 bg-[#0a0f1d] flex flex-col gap-4">
            <div className="flex justify-between items-center">
              <div className="flex items-center gap-2">
                <Brain className="w-5 h-5 text-emerald-400 animate-pulse" />
                <div>
                  <h4 className="text-xs font-extrabold uppercase tracking-widest text-slate-200">AIOps Predictive Baseline Engine</h4>
                  <p className="text-[9px] text-slate-500 uppercase tracking-wider font-semibold">Análise de Tendência de Recursos e Previsão de Falhas</p>
                </div>
              </div>
              <span className="text-[9px] font-bold text-emerald-400 bg-emerald-500/10 border border-emerald-500/20 px-2 py-0.5 rounded uppercase">Baseline Ativa</span>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-6 items-center">
              <div className="flex flex-col gap-1 p-3 rounded-lg bg-white/[0.02] border border-white/5 text-xs">
                <span className="text-[9px] text-slate-500 font-bold uppercase tracking-wider">Tendência de Esgotamento de Recursos</span>
                <span className={`text-base font-extrabold ${alerts.some(a => a.status !== 'resolved' && a.event_type.includes('disk')) ? 'text-amber-400' : 'text-slate-300'}`}>
                  {alerts.some(a => a.status !== 'resolved' && a.event_type.includes('disk')) ? 'Risco de Esgotamento de Disco' : 'Estável (Sem esgotamento previsto)'}
                </span>
                <span className="text-[9px] text-slate-400">Banco de Dados & Armazenamento</span>
              </div>
              <div className="flex flex-col gap-1 p-3 rounded-lg bg-white/[0.02] border border-white/5 text-xs">
                <span className="text-[9px] text-slate-500 font-bold uppercase tracking-wider">Previsão de Pico de CPU</span>
                <span className={`text-base font-extrabold ${alerts.some(a => a.status !== 'resolved' && a.event_type.includes('cpu')) ? 'text-violet-400' : 'text-slate-300'}`}>
                  {alerts.some(a => a.status !== 'resolved' && a.event_type.includes('cpu')) ? 'Sobrecarga de CPU Detectada' : 'Normal (Sem picos previstos)'}
                </span>
                <span className="text-[9px] text-slate-400">Serviços de Aplicação</span>
              </div>
              <div className="flex flex-col gap-1 p-3 rounded-lg bg-white/[0.02] border border-white/5 text-xs">
                <span className="text-[9px] text-slate-500 font-bold uppercase tracking-wider">Previsão de Saúde do Host (AIOps)</span>
                <span className={`text-base font-extrabold ${alerts.filter(a => a.status !== 'resolved' && (a.severity === 'critical' || a.severity === 'fatal')).length > 0 ? 'text-rose-400' : 'text-emerald-400'}`}>
                  {alerts.filter(a => a.status !== 'resolved' && (a.severity === 'critical' || a.severity === 'fatal')).length > 0 ? 'Alerta Crítico Ativo' : 'Estável (Sem anomalias ativas)'}
                </span>
                <span className="text-[9px] text-slate-400">Varredura de telemetria geral</span>
              </div>
            </div>

            {/* Simulating baseline graph */}
            <div className="relative h-20 w-full bg-black/40 rounded-lg overflow-hidden border border-white/5 p-2 flex flex-col justify-end">
              <span className="absolute top-2 left-3 text-[8px] font-bold text-slate-500 uppercase tracking-widest">Linha de Base Histórica vs. Projeção Futura (AIOps Engine)</span>
              <svg className="w-full h-12 stroke-emerald-500 fill-emerald-500/5 stroke-1.5" viewBox="0 0 100 20" preserveAspectRatio="none">
                {/* Historical baseline */}
                <path d="M 0,15 L 10,14 L 20,13 L 30,14 L 40,15 L 50,13 L 60,11 L 70,8 L 80,6 L 90,4 L 100,2" />
                {/* Threshold line */}
                <line x1="0" y1="5" x2="100" y2="5" stroke="#f43f5e" strokeDasharray="2 1" />
              </svg>
              <div className="flex justify-between items-center text-[8px] text-slate-500 uppercase font-bold tracking-wider mt-1 px-1">
                <span>08:00 (Passado)</span>
                <span className={alerts.some(a => a.status !== 'resolved') ? "text-rose-400 animate-pulse" : "text-emerald-400"}>
                  {alerts.some(a => a.status !== 'resolved') ? "Alerta Detectado (Fora da Linha de Base)" : "Operação Nominal (Dentro do Threshold)"}
                </span>
                <span>20:00 (Previsão)</span>
              </div>
            </div>
          </div>

          {/* NOC/SOC Sandbox Simulator & Live Metrics Console */}
          {user?.role !== 'viewer' && (
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
              {/* Left Side: Sandbox Simulator */}
              <div className="glass-card p-5 rounded-xl border border-white/5 bg-surface/20 flex flex-col gap-3 justify-between">
                <div>
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <Brain className="w-4 h-4 text-violet-400" />
                      <h4 className="text-xs font-bold uppercase tracking-wider text-slate-200">
                        Simulador de Alertas (Didático)
                      </h4>
                    </div>
                    <span className="text-[9px] font-bold text-slate-500 bg-white/5 px-2 py-0.5 rounded font-mono">
                      PLAYGROUND
                    </span>
                  </div>
                  <p className="text-xs text-slate-400 leading-relaxed mt-2">
                    Injete alertas e anomalias simuladas na API de homologação do NOC/SOC para testar a triagem e o de-bounce de eventos em tempo real.
                  </p>
                </div>
                <div className="flex gap-2.5 flex-col mt-2">
                  <button
                    onClick={() => simulateEvent('cpu')}
                    className="w-full bg-violet-600/10 hover:bg-violet-600/20 border border-violet-500/30 text-violet-300 py-2 px-3 rounded-lg text-xs font-bold flex items-center gap-2 transition-all cursor-pointer"
                  >
                    <Cpu className="w-3.5 h-3.5" />
                    <span>Simular Sobrecarga CPU (NOC)</span>
                  </button>
                  <button
                    onClick={() => simulateEvent('memory')}
                    className="w-full bg-cyan-600/10 hover:bg-cyan-600/20 border border-cyan-500/30 text-cyan-300 py-2 px-3 rounded-lg text-xs font-bold flex items-center gap-2 transition-all cursor-pointer"
                  >
                    <Layers className="w-3.5 h-3.5" />
                    <span>Simular Falta Memória (NOC)</span>
                  </button>
                  <button
                    onClick={() => simulateEvent('wazuh')}
                    className="w-full bg-blue-600/10 hover:bg-blue-600/20 border border-blue-500/30 text-blue-300 py-2 px-3 rounded-lg text-xs font-bold flex items-center gap-2 transition-all cursor-pointer"
                  >
                    <Terminal className="w-3.5 h-3.5" />
                    <span>Simular Ataque SSH Bruteforce (SOC)</span>
                  </button>
                </div>
              </div>

              {/* Right Side: NOC & SOC Real-Time Metrics Console */}
              <div className="glass-card p-5 rounded-xl border border-white/5 bg-surface/20 flex flex-col gap-4">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Activity className="w-4 h-4 text-cyan-400" />
                    <h4 className="text-xs font-bold uppercase tracking-wider text-slate-200">
                      Painel de Indicadores Core (Tempo Real)
                    </h4>
                  </div>
                  <span className="flex h-2 w-2 relative">
                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                    <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
                  </span>
                </div>

                <div className="grid grid-cols-2 gap-4 text-[11px] font-sans">
                  {/* NOC Indicators */}
                  <div className="p-3 rounded-lg bg-white/[0.02] border border-white/5 flex flex-col gap-2">
                    <div className="font-extrabold text-[9px] text-slate-500 uppercase tracking-widest border-b border-white/5 pb-1">NOC (Performance)</div>
                    <div className="flex justify-between items-center">
                      <span className="text-slate-400">Assets Uptime</span>
                      <span className="text-emerald-400 font-bold font-mono">99.98%</span>
                    </div>
                    <div className="flex justify-between items-center">
                      <span className="text-slate-400">Latência / Perda</span>
                      <span className="text-slate-200 font-mono">12ms / 0%</span>
                    </div>
                    <div className="flex justify-between items-center">
                      <span className="text-slate-400">Recurso Crítico</span>
                      <span className={`font-bold ${alerts.some(a => a.event_type === 'HostHighCpuLoad' && a.status !== 'resolved') ? 'text-rose-400 animate-pulse' : 'text-slate-400'}`}>
                        {alerts.some(a => a.event_type === 'HostHighCpuLoad' && a.status !== 'resolved') ? 'CPU: web-server (98%)' : 'Normal'}
                      </span>
                    </div>
                  </div>

                  {/* SOC Indicators */}
                  <div className="p-3 rounded-lg bg-white/[0.02] border border-white/5 flex flex-col gap-2">
                    <div className="font-extrabold text-[9px] text-slate-500 uppercase tracking-widest border-b border-white/5 pb-1">SOC (Segurança)</div>
                    <div className="flex justify-between items-center">
                      <span className="text-slate-400">Ameaças Ativas</span>
                      <span className={`font-bold font-mono ${stats.fatal + stats.critical > 0 ? 'text-rose-400' : 'text-emerald-400'}`}>
                        {stats.fatal + stats.critical} Ativas
                      </span>
                    </div>
                    <div className="flex justify-between items-center">
                      <span className="text-slate-400">Anomalia Auth</span>
                      <span className={`font-bold ${alerts.some(a => a.event_type.includes('SSH') && a.status !== 'resolved') ? 'text-rose-400 animate-pulse' : 'text-slate-400'}`}>
                        {alerts.some(a => a.event_type.includes('SSH') && a.status !== 'resolved') ? 'Bruteforce Ativo!' : 'Normal'}
                      </span>
                    </div>
                    <div className="flex justify-between items-center">
                      <span className="text-slate-400">Perímetro IPS</span>
                      <span className="text-slate-200 font-mono">1.4K block/m</span>
                    </div>
                  </div>
                </div>

                {/* Operation Indicators */}
                <div className="p-2.5 rounded-lg bg-white/[0.02] border border-white/5 flex justify-between items-center text-[10px] text-slate-400">
                  <div className="flex items-center gap-1.5">
                    <span className="w-1.5 h-1.5 rounded-full bg-violet-500"></span>
                    <span>MTTA Órfãos: <strong className="text-slate-200 font-mono">{alerts.filter(a => a.status === 'triggered' && (Date.now() - new Date(a.created_at).getTime()) > 180000).length} aguardando</strong></span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span className="w-1.5 h-1.5 rounded-full bg-cyan-500"></span>
                    <span>Operador de Turno: <strong className="text-slate-200">{user ? `${user.name} (${user.role.toUpperCase()})` : 'Nenhum'}</strong></span>
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* Cockpit Switcher Tab Bar */}
          <div className="flex border-b border-white/5 gap-2 pb-1">
            <button
              onClick={() => setCockpitTab('alerts')}
              className={`pb-2 px-3 text-xs uppercase tracking-wider font-bold border-b-2 transition-all flex items-center gap-1.5 cursor-pointer ${
                cockpitTab === 'alerts'
                  ? 'border-violet-500 text-white'
                  : 'border-transparent text-slate-400 hover:text-slate-200'
              }`}
            >
              <Activity className="w-3.5 h-3.5" />
              Painel de Alertas
            </button>
            <button
              onClick={() => setCockpitTab('topology')}
              className={`pb-2 px-3 text-xs uppercase tracking-wider font-bold border-b-2 transition-all flex items-center gap-1.5 cursor-pointer ${
                cockpitTab === 'topology'
                  ? 'border-violet-500 text-white'
                  : 'border-transparent text-slate-400 hover:text-slate-200'
              }`}
            >
              <Network className="w-3.5 h-3.5" />
              Topologia CMDB & Ativos
            </button>
            {user?.role === 'admin' && (
              <button
                onClick={() => setCockpitTab('settings')}
                className={`pb-2 px-3 text-xs uppercase tracking-wider font-bold border-b-2 transition-all flex items-center gap-1.5 cursor-pointer ${
                  cockpitTab === 'settings'
                    ? 'border-violet-500 text-white'
                    : 'border-transparent text-slate-400 hover:text-slate-200'
                }`}
              >
                <Settings className="w-3.5 h-3.5" />
                Configuração MSP
              </button>
            )}
          </div>

          {/* Search and Filters */}
          <div className="flex gap-4 items-center">
            <div className="flex-1 relative">
              <Search className="absolute left-3.5 top-2.5 w-4.5 h-4.5 text-slate-500" />
              <input 
                type="text" 
                placeholder="Search alerts by summary, event type, metadata, source..."
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                className="w-full bg-surface/40 hover:bg-surface/60 focus:bg-surface/80 border border-white/5 rounded-xl pl-11 pr-4 py-2.5 text-sm focus:outline-none focus:border-violet-500/50 text-slate-200 transition-all placeholder:text-slate-500"
              />
            </div>
          </div>

          {/* Focus Mode Banner */}
          {selectedTenantIds.length === 1 && (
            <div className="p-3 rounded-xl bg-violet-950/20 border border-violet-500/35 flex items-center justify-between text-xs text-violet-300">
              <div className="flex items-center gap-2">
                <Target className="w-4 h-4 text-violet-400 animate-pulse animate-duration-1000" />
                <span>
                  Modo de Foco Ativo: Monitorando apenas o tenant <strong>{tenants.find(t => t.id === selectedTenantIds[0])?.name || 'Cliente Selecionado'}</strong>
                </span>
              </div>
              <button
                onClick={() => {
                  setSelectedTenantIds(tenants.map(t => t.id));
                }}
                className="px-3 py-1 rounded bg-violet-500/20 hover:bg-violet-500/35 text-violet-300 hover:text-white transition-all font-bold uppercase text-[9px] cursor-pointer"
              >
                Ver Todos os Clientes
              </button>
            </div>
          )}

          {/* Alerts Table/Feed */}
          {cockpitTab === 'alerts' ? (
            <div className="glass-card rounded-xl overflow-hidden flex flex-col border border-white/5">
              <div className="grid grid-cols-12 gap-4 px-6 py-3 border-b border-white/5 bg-surface/30 text-[10px] tracking-wider uppercase font-semibold text-slate-400">
              <div className="col-span-1">Severity</div>
              <div className="col-span-1 text-center">Source</div>
              <div className="col-span-2">Visual Domain</div>
              <div className="col-span-2">Event Type</div>
              <div className="col-span-2">Summary</div>
              <div className="col-span-1 text-center">Focar</div>
              <div className="col-span-1 text-center">Time / SLA</div>
              <div className="col-span-2 text-right">Status</div>
            </div>

            <div className="flex flex-col max-h-[500px] overflow-y-auto divide-y divide-white/5">
              {filteredAlerts.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-16 gap-3 text-slate-500">
                  <Activity className="w-10 h-10 text-slate-600 animate-pulse" />
                  <p className="text-sm">No active alerts reporting in this domain context.</p>
                  <p className="text-xs text-slate-600 bg-white/5 px-3 py-1 rounded">Webhook listener active on port 8080</p>
                </div>
              ) : (
                filteredAlerts.map(alert => (
                  <div 
                    key={alert.id}
                    onClick={() => setSelectedAlert(alert)}
                    className={`grid grid-cols-12 gap-4 px-6 py-4 items-center text-sm cursor-pointer transition-all hover:bg-white/[0.02] ${
                      selectedAlert?.id === alert.id ? 'bg-violet-950/15 border-l-2 border-violet-500' : ''
                    }`}
                  >
                    
                    {/* Severity Badge */}
                    <div className="col-span-1">
                      <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider ${
                        alert.severity === 'fatal' 
                          ? 'bg-severity-fatal/10 text-severity-fatal border border-severity-fatal/35 neon-pulse-fatal' 
                          : alert.severity === 'critical'
                            ? 'bg-severity-critical/10 text-severity-critical border border-severity-critical/30 neon-pulse-critical'
                            : alert.severity === 'warning'
                              ? 'bg-severity-warning/10 text-severity-warning border border-severity-warning/25'
                              : 'bg-severity-info/10 text-severity-info border border-severity-info/20'
                      }`}>
                        {alert.severity}
                      </span>
                    </div>

                    {/* Source Badge */}
                    <div className="col-span-1 text-center">
                      <span className={`inline-block px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider ${
                        (alert.ai_analysis?.source || 'generic') === 'prometheus'
                          ? 'bg-purple-500/10 text-purple-400 border border-purple-500/20'
                          : (alert.ai_analysis?.source || 'generic') === 'wazuh'
                            ? 'bg-blue-500/10 text-blue-400 border border-blue-500/20'
                            : (alert.ai_analysis?.source || 'generic') === 'sentinel'
                              ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
                              : (alert.ai_analysis?.source || 'generic') === 'uptimekuma'
                                ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                                : (alert.ai_analysis?.source || 'generic') === 'grafana'
                                  ? 'bg-violet-500/10 text-violet-400 border border-violet-500/20'
                                  : (alert.ai_analysis?.source || 'generic') === 'zabbix'
                                    ? 'bg-rose-500/10 text-rose-400 border border-rose-500/20'
                                    : 'bg-slate-500/10 text-slate-400 border border-slate-500/20'
                      }`}>
                        {alert.ai_analysis?.source || 'generic'}
                      </span>
                    </div>

                    {/* Visual Domain (Tenant Name) */}
                    <div className="col-span-2 truncate">
                      <span className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded text-[10px] font-extrabold uppercase tracking-wider bg-violet-500/10 text-violet-400 border border-violet-500/20">
                        {tenants.find(t => t.id === alert.tenant_id)?.name || 'Default Tenant'}
                      </span>
                    </div>

                    {/* Event Type */}
                    <div className="col-span-2 font-mono text-xs text-slate-300 font-bold flex items-center gap-1.5 truncate">
                      <Terminal className="w-3.5 h-3.5 text-slate-500" />
                      {alert.event_type}
                    </div>

                    {/* Summary */}
                    <div className="col-span-2 text-slate-200 font-medium truncate">
                      {alert.summary}
                    </div>

                    {/* Action (Fly-to-Context) */}
                    <div className="col-span-1 text-center">
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          setSelectedTenantIds([alert.tenant_id]);
                          const t = tenants.find(x => x.id === alert.tenant_id);
                          if (t) {
                            setSelectedTenant(t);
                          }
                        }}
                        title="Isolar foco neste cliente"
                        className="p-1 rounded bg-violet-600/15 hover:bg-violet-600/40 text-violet-400 border border-violet-500/20 hover:text-white transition-all cursor-pointer inline-flex items-center justify-center"
                      >
                        <Target className="w-3.5 h-3.5" />
                      </button>
                    </div>

                    {/* Timestamp / SLA */}
                    <div className="col-span-1 flex flex-col items-center gap-1">
                      <span className="text-xs text-slate-400 font-mono">
                        {new Date(alert.created_at).toLocaleTimeString()}
                      </span>
                      <SlaCountdown alert={alert} />
                    </div>

                    {/* Status Badge */}
                    <div className="col-span-2 text-right">
                      <span className={`inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider border ${
                        alert.status === 'resolved'
                          ? 'bg-emerald-500/10 border-emerald-500/20 text-emerald-400'
                          : alert.status === 'acknowledged'
                            ? 'bg-amber-500/10 border-amber-500/20 text-amber-400'
                            : alert.status === 'suppressed'
                              ? 'bg-slate-500/10 border-slate-500/20 text-slate-400'
                              : 'bg-rose-500/10 border-rose-500/20 text-rose-400'
                      }`}>
                        {alert.status === 'resolved' && <CheckCircle2 className="w-2.5 h-2.5" />}
                        {alert.status}
                      </span>
                    </div>

                  </div>
                ))
              )}
            </div>
          </div>
          ) : cockpitTab === 'topology' ? (
            // Interactive Topology CMDB view
            <div className="glass-card rounded-xl overflow-hidden flex flex-col border border-white/5 p-6 bg-[#040812]">
              <div className="flex justify-between items-center mb-6">
                <div className="flex flex-col gap-0.5">
                  <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider">Mapeamento de Topologia & CMDB</h4>
                  <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">Descoberta em tempo real de ativos de rede e segurança</p>
                </div>
                <div className="flex gap-4 text-[10px] font-bold text-slate-400">
                  <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-emerald-500"></span> Operacional</span>
                  <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-rose-500 animate-ping"></span> Incidente Ativo</span>
                </div>
              </div>

              <div className="relative w-full h-[360px] bg-black/60 rounded-xl border border-white/5 flex items-center justify-center overflow-hidden">
                <svg className="w-full h-full" viewBox="0 0 800 400">
                  {/* Grid background pattern */}
                  <defs>
                    <pattern id="grid" width="20" height="20" patternUnits="userSpaceOnUse">
                      <path d="M 20 0 L 0 0 0 20" fill="none" stroke="rgba(255,255,255,0.015)" strokeWidth="1" />
                    </pattern>
                  </defs>
                  <rect width="100%" height="100%" fill="url(#grid)" />

                  {/* Connective lines */}
                  {/* Internet -> NGFW */}
                  <line x1="150" y1="200" x2="280" y2="200" stroke="rgba(255,255,255,0.1)" strokeWidth="2" strokeDasharray="4 2" />
                  
                  {/* NGFW -> Core Switch */}
                  <line x1="280" y1="200" x2="430" y2="200" stroke="rgba(255,255,255,0.1)" strokeWidth="2" />
                  
                  {/* Core Switch -> SQL Server */}
                  <line x1="430" y1="200" x2="580" y2="100" stroke="rgba(255,255,255,0.1)" strokeWidth="2" />
                  
                  {/* Core Switch -> IIS Server */}
                  <line x1="430" y1="200" x2="580" y2="200" stroke="rgba(255,255,255,0.1)" strokeWidth="2" />
                  
                  {/* Core Switch -> Wazuh SOC Agent */}
                  <line x1="430" y1="200" x2="580" y2="300" stroke="rgba(255,255,255,0.1)" strokeWidth="2" />

                  {/* Nodes rendering */}
                  {/* Node 1: Internet Cloud */}
                  <g className="cursor-pointer" onClick={() => setSearchTerm('')}>
                    <circle cx="150" cy="200" r="28" className="fill-slate-900 stroke-slate-700 stroke-2" />
                    <text x="150" y="204" className="text-[10px] font-sans font-bold fill-slate-300 text-anchor-middle" textAnchor="middle">INTERNET</text>
                  </g>

                  {/* Node 2: NGFW Firewall */}
                  <g className="cursor-pointer" onClick={() => setSearchTerm('firewall')}>
                    <circle cx="280" cy="200" r="28" className="fill-[#1e1515] stroke-rose-500/40 stroke-2" />
                    <text x="280" y="204" className="text-[10px] font-sans font-bold fill-rose-400 text-anchor-middle" textAnchor="middle">NGFW</text>
                  </g>

                  {/* Node 3: Core Switch */}
                  <g className="cursor-pointer" onClick={() => setSearchTerm('switch')}>
                    <circle cx="430" cy="200" r="28" className="fill-slate-900 stroke-cyan-500/40 stroke-2" />
                    <text x="430" y="204" className="text-[10px] font-sans font-bold fill-cyan-400 text-anchor-middle" textAnchor="middle">SWITCH</text>
                  </g>

                  {/* Node 4: SQL Server (Database) */}
                  <g className="cursor-pointer" onClick={() => setSearchTerm('sql server')}>
                    {/* Pulsing indicator if has alerts matching sql */}
                    {alerts.some(a => a.summary.toLowerCase().includes('sql') || a.event_type.toLowerCase().includes('sql')) && (
                      <circle cx="580" cy="100" r="34" className="fill-none stroke-rose-500 stroke-1 animate-ping" />
                    )}
                    <circle cx="580" cy="100" r="28" className={`stroke-2 ${
                      alerts.some(a => a.status !== 'resolved' && (a.summary.toLowerCase().includes('sql') || a.event_type.toLowerCase().includes('sql')))
                        ? 'fill-[#221015] stroke-rose-500'
                        : 'fill-slate-900 stroke-emerald-500'
                    }`} />
                    <text x="580" y="104" className="text-[9px] font-sans font-bold fill-slate-200 text-anchor-middle" textAnchor="middle">SQL DB</text>
                  </g>

                  {/* Node 5: IIS Server (Web) */}
                  <g className="cursor-pointer" onClick={() => setSearchTerm('iis')}>
                    {alerts.some(a => a.summary.toLowerCase().includes('iis') || a.event_type.toLowerCase().includes('iis')) && (
                      <circle cx="580" cy="200" r="34" className="fill-none stroke-rose-500 stroke-1 animate-ping" />
                    )}
                    <circle cx="580" cy="200" r="28" className={`stroke-2 ${
                      alerts.some(a => a.status !== 'resolved' && (a.summary.toLowerCase().includes('iis') || a.event_type.toLowerCase().includes('iis')))
                        ? 'fill-[#221015] stroke-rose-500'
                        : 'fill-slate-900 stroke-emerald-500'
                    }`} />
                    <text x="580" y="204" className="text-[9px] font-sans font-bold fill-slate-200 text-anchor-middle" textAnchor="middle">IIS WEB</text>
                  </g>

                  {/* Node 6: Wazuh SOC Agent */}
                  <g className="cursor-pointer" onClick={() => setSearchTerm('wazuh')}>
                    {alerts.some(a => a.summary.toLowerCase().includes('wazuh') || a.event_type.toLowerCase().includes('security')) && (
                      <circle cx="580" cy="300" r="34" className="fill-none stroke-rose-500 stroke-1 animate-ping" />
                    )}
                    <circle cx="580" cy="300" r="28" className={`stroke-2 ${
                      alerts.some(a => a.status !== 'resolved' && (a.summary.toLowerCase().includes('wazuh') || a.event_type.toLowerCase().includes('security')))
                        ? 'fill-[#221015] stroke-rose-500'
                        : 'fill-slate-900 stroke-emerald-500'
                    }`} />
                    <text x="580" y="304" className="text-[9px] font-sans font-bold fill-slate-200 text-anchor-middle" textAnchor="middle">SOC AGENT</text>
                  </g>
                </svg>

                <div className="absolute bottom-4 left-6 text-[10px] text-slate-500 bg-black/60 border border-white/5 px-2.5 py-1 rounded-md">
                  💡 <em>Dica: Clique nos nós da topologia para filtrar os incidentes daquele ativo!</em>
                </div>
              </div>
            </div>
          ) : (
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
                          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">Monitoramento de batimento (heartbeat) de telemetria em tempo real para o cliente: <strong className="text-violet-400">{selectedTenant.name}</strong></p>
                        </div>
                        <button
                          onClick={() => fetchAllConnectorStatuses(selectedTenant.id)}
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
                            value={selectedTenant.id}
                            onChange={(e) => {
                              const t = tenants.find(x => x.id === e.target.value);
                              if (t) {
                                setSelectedTenant(t);
                                let newIds = [t.id];
                                setSelectedTenantIds(newIds);
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
                          { id: 'loki', name: 'Grafana Loki', method: 'API Polling (Pull)', desc: 'Leitura de logs distribuídos com inteligência AIOps em tempo real.', color: 'border-orange-500/20' }
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
                ) : ['uptimekuma', 'zabbix', 'prometheus', 'wazuh', 'grafana', 'sentinel', 'loki'].includes(selectedIntegrationTool) ? (
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
                          </h4>
                          <p className="text-[10px] text-slate-500 uppercase tracking-widest font-bold">
                            {['uptimekuma', 'zabbix', 'prometheus', 'wazuh', 'grafana'].includes(selectedIntegrationTool) ? 'Método: Webhook (Push / Envio de Alertas)' : 'Método: API Polling (Pull / Busca Ativa de Chaves)'}
                          </p>
                        </div>
                      </div>

                      {/* Push webhook forms */}
                      {['uptimekuma', 'zabbix', 'prometheus', 'wazuh', 'grafana'].includes(selectedIntegrationTool) ? (
                        <div className="flex flex-col gap-4">
                          {/* Active Integrations list */}
                          <div className="flex flex-col gap-2.5">
                            <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">
                              Integrações Ativas ({selectedTenant.name})
                            </label>
                            
                            {(integrations || []).filter(i => i.type === selectedIntegrationTool).length > 0 ? (
                              <div className="flex flex-col gap-2 max-h-[150px] overflow-y-auto pr-1">
                                {(integrations || []).filter(i => i.type === selectedIntegrationTool).map(item => (
                                  <div key={item.id} className="p-3 rounded-lg bg-[#040811] border border-white/5 flex items-center justify-between font-sans text-xs">
                                    <div className="flex flex-col gap-0.5">
                                      <span className="font-bold text-slate-200">{item.name}</span>
                                      <span className="text-[9px] font-mono text-cyan-400 select-all leading-none mt-1">
                                        {`${API_BASE_URL}/api/v1/webhook/${selectedIntegrationTool}/${selectedTenant.id}`}
                                      </span>
                                    </div>
                                    <div className="flex items-center gap-2 shrink-0 ml-4">
                                      <button
                                        onClick={() => handleCopyWebhookUrl(`${API_BASE_URL}/api/v1/webhook/${selectedIntegrationTool}/${selectedTenant.id}`)}
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
                      ) : ['sentinel', 'loki', 'ssh'].includes(selectedIntegrationTool) ? (
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
                              </select>
                            </div>

                            <div className="flex flex-col gap-2">
                              <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Valor da Credencial (Secret Value)</label>
                              <input
                                type="password"
                                required
                                value={vaultValue}
                                placeholder="Digite ou cole o valor confidencial aqui..."
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
                            <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Escopo de Atuação do Playbook</label>
                            <div className="flex gap-4 mt-1 mb-1">
                              <label className="flex items-center gap-2 text-xs text-slate-300 cursor-pointer">
                                <input
                                  type="radio"
                                  name="playbookScope"
                                  checked={!playbookIsGlobal}
                                  onChange={() => setPlaybookIsGlobal(false)}
                                  className="accent-cyan-400"
                                />
                                <span>Apenas este cliente ({selectedTenant.name})</span>
                              </label>
                              <label className="flex items-center gap-2 text-xs text-slate-300 cursor-pointer">
                                <input
                                  type="radio"
                                  name="playbookScope"
                                  checked={playbookIsGlobal}
                                  onChange={() => setPlaybookIsGlobal(true)}
                                  className="accent-cyan-400"
                                />
                                <span className="text-cyan-400 font-semibold flex items-center gap-1">🌐 Todos os Clientes (Global)</span>
                              </label>
                            </div>
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
                        <div className="flex items-center justify-between border-b border-white/5 pb-2">
                          <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400 block font-sans">
                            Playbooks Ativos ({selectedTenant.name})
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
                                          🏢 Tenant
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
                            <option value="operator">Operator (Operador - Acesso de Leitura/Ação)</option>
                            <option value="admin">Admin (Administrador - Acesso Completo/Cofre/Usuários)</option>
                            <option value="viewer">Viewer (Visualizador - Apenas Leitura de Painéis)</option>
                          </select>
                        </div>

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
                            <div className="grid grid-cols-3 gap-4">
                              <div className="p-4 rounded-xl bg-white/5 border border-white/5 flex flex-col gap-1 animate-fadeIn">
                                <span className="text-[9px] uppercase font-bold text-slate-400">Total de Incidentes</span>
                                <span className="text-2xl font-bold text-slate-100">{slaData.total_incidents}</span>
                              </div>
                              
                              <div className="p-4 rounded-xl bg-white/5 border border-white/5 flex flex-col gap-1 animate-fadeIn">
                                <span className="text-[9px] uppercase font-bold text-slate-400">Tempo Médio Resposta (MTTR)</span>
                                <span className="text-2xl font-bold text-slate-100">{slaData.mttr_minutes} min</span>
                              </div>

                              <div className="p-4 rounded-xl bg-white/5 border border-white/5 flex flex-col gap-1 animate-fadeIn">
                                <span className="text-[9px] uppercase font-bold text-slate-400">Nível Geral de SLA (Compliance)</span>
                                <span className="text-2xl font-bold text-emerald-400">{slaData.sla_percentage}%</span>
                              </div>
                            </div>

                            <div className="p-5 rounded-xl bg-white/5 border border-white/5 flex flex-col gap-3 animate-fadeIn">
                              <h5 className="text-xs font-bold text-slate-200 uppercase tracking-wide">Status Geral de Compliance</h5>
                              <div className="w-full bg-slate-950 rounded-full h-3.5 border border-white/5 overflow-hidden">
                                <div 
                                  className="bg-emerald-500 h-full rounded-full transition-all" 
                                  style={{ width: `${slaData.sla_percentage}%` }}
                                ></div>
                              </div>
                              <p className="text-[10px] text-slate-400 leading-relaxed">
                                O SLA (Service Level Agreement) é calculado com base no tempo de resposta inicial e tempo de mitigação acordado. As metas estabelecidas são de resposta rápida de até 15 minutos para alertas críticos e correção em até 1 hora.
                              </p>
                            </div>

                            {/* Export/Download SLA PDF */}
                            <div className="p-5 rounded-xl bg-[#0e1626] border border-cyan-500/10 flex items-center justify-between mt-2 animate-fadeIn">
                              <div className="flex flex-col gap-0.5">
                                <h5 className="text-xs font-bold text-white">Relatório Executivo Mensal</h5>
                                <p className="text-[10px] text-slate-400">Gere e baixe a via em PDF oficial com assinaturas e log de incidentes.</p>
                              </div>
                              <button
                                onClick={() => {
                                  window.open(`${API_BASE_URL}/api/v1/reports/sla?token=${token || selectedTenant.id}&tenant_name=${encodeURIComponent(selectedTenant.name)}`);
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
        </section>

        {/* Right Section (Glass detail Sidebar Panel) */}
        {selectedAlert && !isWallboardMode && (
          <aside className="w-[450px] shrink-0 glass-sidebar flex flex-col overflow-hidden border-l border-white/5">
            
            {/* Sidebar Title */}
            <div className="px-6 py-5 border-b border-white/5 flex items-center justify-between shrink-0">
              <div className="flex items-center gap-2">
                <Cpu className="w-4 h-4 text-violet-400" />
                <h3 className="text-sm font-bold uppercase tracking-wider">Alert Details</h3>
              </div>
              <button 
                onClick={() => setSelectedAlert(null)}
                className="text-xs text-slate-500 hover:text-slate-300 bg-white/5 hover:bg-white/10 px-2 py-1 rounded"
              >
                Close
              </button>
            </div>

            {/* Tab Selectors */}
            <div className="flex border-b border-white/5 bg-surface/20 shrink-0 text-xs font-semibold">
              <button
                onClick={() => setActiveTab('general')}
                className={`flex-1 py-3 text-center border-b-2 transition-all ${
                  activeTab === 'general' ? 'border-violet-500 text-violet-400' : 'border-transparent text-slate-400 hover:text-slate-200'
                }`}
              >
                General
              </button>
              <button
                onClick={() => setActiveTab('logs')}
                className={`flex-1 py-3 text-center border-b-2 transition-all ${
                  activeTab === 'logs' ? 'border-violet-500 text-violet-400' : 'border-transparent text-slate-400 hover:text-slate-200'
                }`}
              >
                Loki Logs
              </button>
              <button
                onClick={() => setActiveTab('grafana')}
                className={`flex-1 py-3 text-center border-b-2 transition-all ${
                  activeTab === 'grafana' ? 'border-violet-500 text-violet-400' : 'border-transparent text-slate-400 hover:text-slate-200'
                }`}
              >
                Grafana
              </button>
              <button
                onClick={() => setActiveTab('raw')}
                className={`flex-1 py-3 text-center border-b-2 transition-all ${
                  activeTab === 'raw' ? 'border-violet-500 text-violet-400' : 'border-transparent text-slate-400 hover:text-slate-200'
                }`}
              >
                Raw
              </button>
              <button
                onClick={() => setActiveTab('timeline')}
                className={`flex-1 py-3 text-center border-b-2 transition-all ${
                  activeTab === 'timeline' ? 'border-violet-500 text-violet-400' : 'border-transparent text-slate-400 hover:text-slate-200'
                }`}
              >
                Timeline
              </button>
              <button
                onClick={() => setActiveTab('chat')}
                className={`flex-1 py-3 text-center border-b-2 transition-all ${
                  activeTab === 'chat' ? 'border-violet-500 text-violet-400' : 'border-transparent text-slate-400 hover:text-slate-200'
                }`}
              >
                Co-Pilot
              </button>
            </div>

            {/* Alert Summary Panel */}
            <div className="flex-1 overflow-y-auto p-6 flex flex-col gap-6">
              {activeTab === 'general' && (
                <>
                  {/* Headline Info */}
                  <div className="flex flex-col gap-2">
                    <div className="flex items-center gap-2">
                      <span className={`text-[10px] font-bold uppercase px-2 py-0.5 rounded ${
                        selectedAlert.severity === 'fatal' 
                          ? 'bg-severity-fatal/15 text-severity-fatal'
                          : selectedAlert.severity === 'critical'
                            ? 'bg-severity-critical/15 text-severity-critical'
                            : selectedAlert.severity === 'warning'
                              ? 'bg-severity-warning/15 text-severity-warning'
                              : 'bg-severity-info/15 text-severity-info'
                      }`}>
                        {selectedAlert.severity} Severity
                      </span>
                      <span className="text-xs text-slate-500 font-mono">{selectedAlert.id}</span>
                    </div>
                    <h4 className="text-lg font-bold text-white leading-tight">{selectedAlert.summary}</h4>
                    <p className="text-xs text-slate-400">Received: {new Date(selectedAlert.created_at).toLocaleString()}</p>
                    {selectedAlert.resolved_at && (
                      <p className="text-xs text-emerald-400">Resolved: {new Date(selectedAlert.resolved_at).toLocaleString()}</p>
                    )}
                  </div>

                  {/* Incident Source */}
                  <div className="p-4 rounded-xl bg-white/5 border border-white/5 flex items-center justify-between">
                    <div className="flex items-center gap-2.5">
                      <LayoutDashboard className="w-5 h-5 text-violet-400" />
                      <div>
                        <h5 className="text-xs font-bold text-slate-300">Incident Source</h5>
                        <p className="text-[10px] text-slate-500 uppercase font-semibold">Normalized alert origin</p>
                      </div>
                    </div>
                    <div className="text-right">
                      <span className="text-sm font-extrabold text-violet-400 block uppercase">
                        {selectedAlert.ai_analysis?.source || 'generic'}
                      </span>
                    </div>
                  </div>

                  {/* Action Buttons */}
                  <div className="grid grid-cols-2 gap-3 shrink-0">
                    <button
                      disabled={selectedAlert.status === 'acknowledged' || selectedAlert.status === 'resolved' || selectedAlert.status === 'suppressed' || user?.role === 'viewer'}
                      onClick={() => handleUpdateStatus(selectedAlert.id, 'acknowledged')}
                      className="bg-amber-500/10 hover:bg-amber-500/20 disabled:opacity-40 disabled:hover:bg-amber-500/10 border border-amber-500/30 text-amber-300 py-2 rounded-lg text-xs font-bold uppercase tracking-wider flex items-center justify-center gap-2 transition-all"
                    >
                      Acknowledge
                    </button>
                    <button
                      disabled={selectedAlert.status === 'resolved' || user?.role === 'viewer'}
                      onClick={() => handleUpdateStatus(selectedAlert.id, 'resolved')}
                      className="bg-emerald-500/10 hover:bg-emerald-500/20 disabled:opacity-40 disabled:hover:bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 py-2 rounded-lg text-xs font-bold uppercase tracking-wider flex items-center justify-center gap-2 transition-all"
                    >
                      Resolve Alert
                    </button>
                  </div>

                  {/* Co-Pilot AI Diagnostics */}
                  <div className="flex flex-col gap-3 p-5 rounded-xl bg-violet-950/20 border border-violet-500/25">
                    <div className="flex items-center gap-2">
                      <Brain className="w-4 h-4 text-violet-400 animate-pulse" />
                      <h5 className="text-xs font-extrabold uppercase text-violet-300 tracking-wider">💡 IA Co-Pilot Diagnostics</h5>
                    </div>
                    {selectedAlert.ai_diagnostic ? (
                      <div className="text-slate-300 select-text flex flex-col gap-1.5 max-h-64 overflow-y-auto pr-1">
                        {formatMarkdown(selectedAlert.ai_diagnostic)}
                      </div>
                    ) : (
                      <div className="flex items-center gap-2 text-xs text-slate-400">
                        <RefreshCw className="w-3.5 h-3.5 animate-spin text-violet-400" />
                        <span>Gerando diagnóstico e sugestões causa raiz via Gemini...</span>
                      </div>
                    )}
                  </div>

                  {/* Playbooks & Auto-Cura (Runbooks SSH) */}
                  <div className="flex flex-col gap-3.5 p-5 rounded-xl bg-slate-900/40 border border-white/5">
                    <div className="flex items-center gap-2">
                      <Zap className="w-4 h-4 text-amber-400" />
                      <h5 className="text-xs font-extrabold uppercase text-slate-300 tracking-wider">⚡ Playbooks de Auto-Cura</h5>
                    </div>
                    <p className="text-[11px] text-slate-400 leading-normal">
                      Execute scripts remotos de mitigação no host afetado utilizando credenciais seguras do Vault.
                    </p>

                    {runbooks.length === 0 ? (
                      <div className="text-xs text-slate-500 italic bg-white/[0.01] p-3 rounded-lg border border-white/5">
                        Nenhum playbook SSH configurado para este cliente. Adicione na aba Admin.
                      </div>
                    ) : (
                      <div className="flex flex-col gap-2">
                        {runbooks.map(rb => (
                          <div key={rb.id} className="flex items-center justify-between p-2 rounded-lg bg-white/[0.02] border border-white/5">
                            <span className="text-xs font-medium text-slate-300">{rb.name}</span>
                            <button
                              disabled={isExecutingRunbook || user?.role === 'viewer'}
                              onClick={() => handleExecuteRunbook(rb.id)}
                              className="px-2.5 py-1 rounded bg-amber-500/10 hover:bg-amber-500/20 disabled:opacity-50 text-amber-300 text-[10px] font-bold uppercase tracking-wider border border-amber-500/20 transition-all flex items-center gap-1 cursor-pointer"
                            >
                              <Zap className="w-2.5 h-2.5" />
                              Executar
                            </button>
                          </div>
                        ))}
                      </div>
                    )}

                    {runbookLogs && (
                      <div className="flex flex-col gap-2 mt-2">
                        <label className="text-[10px] uppercase font-bold text-slate-500">Terminal SSH Output:</label>
                        <pre className="bg-black border border-white/5 rounded-lg p-3 text-[10px] font-mono text-emerald-400 overflow-x-auto max-h-48 whitespace-pre-wrap select-text leading-relaxed">
                          {runbookLogs}
                        </pre>
                      </div>
                    )}
                  </div>

                  {/* Friendly Explanation (For Beginners/Laypeople) */}
                  <div className="flex flex-col gap-2.5 p-4 rounded-xl bg-violet-950/10 border border-violet-500/10">
                    <div className="flex items-center gap-2">
                      <Brain className="w-4 h-4 text-violet-400" />
                      <h5 className="text-xs font-extrabold uppercase text-violet-300 tracking-wider">🔬 O que significa este alerta?</h5>
                    </div>
                    <p className="text-xs text-slate-300 leading-relaxed font-sans">
                      {selectedAlert.event_type === 'cpu' || selectedAlert.event_type === 'HostHighCpuLoad' ? (
                        "A CPU é o 'cérebro' do servidor. Este alerta significa que o servidor está sobrecarregado com muitas tarefas simultâneas, o que pode deixar os serviços lentos para os usuários finais."
                      ) : selectedAlert.event_type === 'memory' || selectedAlert.event_type === 'OOMKillerTriggered' ? (
                        "A memória RAM guarda dados temporários de aplicativos ativos. A falta de memória pode fazer o servidor travar ou derrubar bancos de dados críticos."
                      ) : selectedAlert.event_type === 'wazuh_security_event' || selectedAlert.event_type === 'sshd' || selectedAlert.event_type === 'syslog' ? (
                        "Um sistema ou invasor tentou acessar a conta 'root' (administrador) do servidor errando a senha repetidamente. Isso é um ataque de Força Bruta por SSH."
                      ) : (
                        "Um evento de monitoramento reportou um comportamento fora do comum neste dispositivo. Requer atenção do operador de turno."
                      )}
                    </p>
                  </div>

                  {/* Operational Runbook Checklist */}
                  <div className="flex flex-col gap-3 p-4 rounded-xl bg-slate-900/40 border border-white/5">
                    <h5 className="text-xs font-extrabold uppercase text-slate-300 tracking-wider flex items-center gap-1.5">
                      <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" /> Guia de Operação (Passo a Passo)
                    </h5>
                    <div className="flex flex-col gap-2 text-slate-400 font-sans leading-relaxed">
                      <div className="flex items-start gap-2">
                        <span className="w-4 h-4 rounded-full bg-white/5 border border-white/10 flex items-center justify-center text-[10px] font-bold text-slate-300 shrink-0 mt-0.5">1</span>
                        <p>Analise a gravidade do alerta e verifique a aba de <b>Loki Logs</b> para ver logs do host no momento do incidente.</p>
                      </div>
                      <div className="flex items-start gap-2">
                        <span className="w-4 h-4 rounded-full bg-white/5 border border-white/10 flex items-center justify-center text-[10px] font-bold text-slate-300 shrink-0 mt-0.5">2</span>
                        <p>Cheque a aba <b>Grafana</b> para validar o uso de recursos do host em tempo real.</p>
                      </div>
                      <div className="flex items-start gap-2">
                        <span className="w-4 h-4 rounded-full bg-white/5 border border-white/10 flex items-center justify-center text-[10px] font-bold text-slate-300 shrink-0 mt-0.5">3</span>
                        <p>Se o problema persistir após a auto-cura automática, clique em <b>Acknowledge</b> para assumir o chamado.</p>
                      </div>
                    </div>
                  </div>

                  {/* Debounce / Occurrences */}
                  <div className="p-4 rounded-xl bg-white/5 border border-white/5 flex items-center justify-between">
                    <div className="flex items-center gap-2.5">
                      <RefreshCw className="w-5 h-5 text-violet-400" />
                      <div>
                        <h5 className="text-xs font-bold text-slate-300">Redis Debounce Engine</h5>
                        <p className="text-[10px] text-slate-500 uppercase font-semibold">Cascade protection</p>
                      </div>
                    </div>
                    <div className="text-right">
                      <span className="text-xl font-extrabold text-white block">
                        {selectedAlert.payload?.occurrences || 1}x
                      </span>
                      <span className="text-[9px] text-slate-400 uppercase font-bold tracking-wider">Occurrences</span>
                    </div>
                  </div>
                </>
              )}

              {activeTab === 'logs' && (
                <div className="flex flex-col gap-3 h-full">
                  <div className="flex items-center justify-between">
                    <label className="text-[10px] uppercase font-bold tracking-wider text-slate-500 flex items-center gap-1.5">
                      <Terminal className="w-3.5 h-3.5 text-cyan-400" /> Grafana Loki Logql (On-Demand 50 Logs)
                    </label>
                    <span className="text-[10px] font-bold text-emerald-400 bg-emerald-500/10 border border-emerald-500/20 px-2 py-0.5 rounded">
                      Loki Connected
                    </span>
                  </div>
                  
                  <div className="flex-1 min-h-[300px] bg-[#030712] border border-white/5 rounded-xl p-4 overflow-y-auto font-mono text-xs leading-relaxed text-slate-300 select-text select-all">
                    {selectedAlert.ai_analysis?.loki_logs && selectedAlert.ai_analysis.loki_logs.length > 0 ? (
                      selectedAlert.ai_analysis.loki_logs.map((logLine: string, idx: number) => {
                        let colorClass = "text-slate-300";
                        if (logLine.includes("[ERROR]")) colorClass = "text-rose-400 font-bold";
                        else if (logLine.includes("[CRITICAL]")) colorClass = "text-red-500 font-bold bg-red-950/20 px-1 rounded";
                        else if (logLine.includes("[WARNING]")) colorClass = "text-amber-400";
                        return (
                          <div key={idx} className={`py-1 border-b border-white/[0.02] ${colorClass}`}>
                            {logLine}
                          </div>
                        );
                      })
                    ) : (
                      <div className="text-slate-500 italic py-10 text-center">
                        No logs retrieved from Loki for this host.
                      </div>
                    )}
                  </div>
                </div>
              )}

              {activeTab === 'grafana' && (
                <div className="flex flex-col gap-3 h-full">
                  <label className="text-[10px] uppercase font-bold tracking-wider text-slate-500 flex items-center gap-1.5">
                    <Activity className="w-3.5 h-3.5 text-violet-400" /> Secure Grafana Iframe Embed
                  </label>
                  
                  <div className="flex-1 min-h-[350px] relative border border-white/5 rounded-xl overflow-hidden bg-[#070a13] flex flex-col justify-center items-center">
                    {/* Visual Grafana Iframe Simulator */}
                    <div className="absolute inset-0 w-full h-full flex flex-col p-4 bg-[#080c14]">
                      {/* Iframe header */}
                      <div className="h-6 flex items-center justify-between border-b border-white/5 pb-2 mb-2 text-[10px] text-slate-500 font-mono">
                        <span>Panel ID: cpu-usage-gauge</span>
                        <span>var-host={selectedAlert.ai_analysis?.host || 'host-unknown'}</span>
                      </div>
                      {/* Simulating real-time SVG charts */}
                      <div className="flex-1 flex flex-col gap-4 justify-center items-center">
                        <div className="relative w-36 h-36 flex items-center justify-center rounded-full border-4 border-dashed border-violet-500/20">
                          <div className="w-28 h-28 flex flex-col items-center justify-center rounded-full bg-violet-950/30 border border-violet-500/40">
                            <span className="text-[10px] text-slate-500 uppercase tracking-widest font-bold">CPU Usage</span>
                            <span className="text-2xl font-black text-white animate-pulse">
                              {selectedAlert.payload?.value ? `${selectedAlert.payload.value}%` : "74.8%"}
                            </span>
                          </div>
                        </div>
                        {/* Simulating line chart */}
                        <svg className="w-full h-24 stroke-violet-500 fill-violet-500/10 stroke-2" viewBox="0 0 100 30">
                          <path d="M 0,25 Q 10,10 20,20 T 40,5 T 60,25 T 80,10 T 100,15 L 100,30 L 0,30 Z" />
                        </svg>
                      </div>
                    </div>
                    {/* Simulated Iframe container */}
                    <iframe 
                      title="Grafana Dashboard"
                      src={`http://localhost:3000/d-solo/tenant-dashboard?var-host=${selectedAlert.ai_analysis?.host || 'unknown'}&theme=dark&panelId=1`}
                      className="absolute inset-0 w-full h-full border-0 opacity-0 hover:opacity-100 transition-opacity bg-transparent"
                      sandbox="allow-scripts allow-same-origin"
                    />
                  </div>
                </div>
              )}

              {activeTab === 'raw' && (
                <div className="flex flex-col gap-2">
                  <label className="text-[10px] uppercase font-bold tracking-wider text-slate-500">Raw Event Payload</label>
                  <div className="bg-[#080b13] border border-white/5 rounded-xl p-4 overflow-x-auto font-mono text-xs text-slate-300">
                    <pre>{JSON.stringify(selectedAlert, null, 2)}</pre>
                  </div>
                </div>
              )}

              {activeTab === 'timeline' && (
                <div className="flex flex-col gap-4 h-full">
                  <div className="flex items-center justify-between">
                    <label className="text-[10px] uppercase font-bold tracking-wider text-slate-500 flex items-center gap-1.5">
                      <Clock className="w-3.5 h-3.5 text-violet-400" /> Timeline & Histórico do Alerta
                    </label>
                    <span className="text-[9px] font-bold text-slate-400">Total: {comments.length} ações</span>
                  </div>

                  {isLoadingComments ? (
                    <div className="flex items-center justify-center py-10 gap-2 text-xs text-slate-500">
                      <RefreshCw className="w-4 h-4 animate-spin text-violet-500" />
                      <span>Carregando timeline...</span>
                    </div>
                  ) : comments.length > 0 ? (
                    <div className="flex flex-col gap-4 max-h-[400px] overflow-y-auto pr-1">
                      {comments.map((c) => {
                        const isSystem = c.author === 'Sistema';
                        const isAI = c.author.includes('AI') || c.author.includes('🤖');
                        const isOperator = c.author === 'Operador';
                        
                        return (
                          <div key={c.id} className="p-3 rounded-lg bg-white/[0.02] border border-white/5 flex flex-col gap-1 text-xs">
                            <div className="flex justify-between items-center text-[10px]">
                              <span className={`font-bold uppercase tracking-wider ${
                                isSystem ? 'text-cyan-400' : isAI ? 'text-rose-400' : 'text-violet-400'
                              }`}>{c.author}</span>
                              <span className="text-slate-500 font-mono">{new Date(c.created_at).toLocaleTimeString()}</span>
                            </div>
                            <div className="text-slate-300 whitespace-pre-line leading-relaxed font-sans mt-0.5">
                              {c.comment}
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  ) : (
                    <div className="text-xs text-slate-500 italic text-center py-10">
                      Nenhuma ação ou comentário registrado neste incidente.
                    </div>
                  )}
                </div>
              )}

              {activeTab === 'chat' && (
                <div className="flex flex-col gap-4 h-full">
                  <div className="p-3 rounded-xl bg-violet-950/10 border border-violet-500/20 text-xs text-slate-300 leading-relaxed font-sans">
                    <div className="flex items-center gap-1.5 text-violet-400 font-extrabold uppercase text-[10px] mb-1">
                      <Cpu className="w-3.5 h-3.5" /> IA Co-Pilot Conversacional
                    </div>
                    Converse diretamente com o assistente Gemini AI sobre o contexto deste alerta. Faça perguntas técnicas ou peça ajuda com comandos.
                  </div>

                  {/* Chat Timeline (filter AI and Operator comments) */}
                  <div className="flex-1 min-h-[180px] max-h-[250px] overflow-y-auto bg-black/30 border border-white/5 rounded-xl p-4 flex flex-col gap-3 font-sans text-xs">
                    {comments.filter(c => c.author === 'Operador' || c.author.includes('AI')).length > 0 ? (
                      comments.filter(c => c.author === 'Operador' || c.author.includes('AI')).map((c, idx) => (
                        <div key={idx} className={`p-2 rounded-lg max-w-[85%] ${
                          c.author === 'Operador' ? 'bg-violet-600/10 border border-violet-500/20 text-white ml-auto' : 'bg-rose-950/10 border border-rose-500/10 text-slate-200'
                        }`}>
                          <div className="text-[8px] font-bold text-slate-500 mb-0.5">
                            {c.author === 'Operador' ? 'Você' : '🤖 Co-Pilot'}
                          </div>
                          <div className="whitespace-pre-line leading-relaxed">
                            {c.comment.replace('🤖 Co-Pilot AI: ', '')}
                          </div>
                        </div>
                      ))
                    ) : (
                      <div className="text-xs text-slate-600 italic text-center my-auto">
                        Faça uma pergunta abaixo para iniciar a conversa técnica com a IA.
                      </div>
                    )}
                  </div>

                  {/* Input form */}
                  <form onSubmit={handleSendChat} className="flex gap-2">
                    <input
                      type="text"
                      required
                      disabled={isSendingChat}
                      value={chatPrompt}
                      onChange={(e) => setChatPrompt(e.target.value)}
                      placeholder="Pergunte algo à IA (ex: Como posso mitigar isso?)..."
                      className="flex-1 bg-[#0b0f19] border border-white/10 rounded-lg px-3 py-2 text-xs text-white placeholder:text-slate-600 focus:outline-none focus:border-violet-500"
                    />
                    <button
                      type="submit"
                      disabled={isSendingChat}
                      className="bg-violet-600 hover:bg-violet-500 disabled:opacity-50 text-white font-bold text-xs px-4 rounded-lg flex items-center justify-center shrink-0 cursor-pointer"
                    >
                      {isSendingChat ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : 'Perguntar'}
                    </button>
                  </form>
                </div>
              )}
            </div>
          </aside>
        )}

      </main>

      {/* 2.5. Active Shift Handover Acknowledge Overlay (Blocking) */}
      {activeHandover && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/90 backdrop-blur-md p-4">
          <div className="glass-card w-full max-w-lg rounded-2xl p-6 border border-rose-500/25 shadow-2xl flex flex-col gap-4 text-center">
            <div className="flex flex-col items-center gap-2">
              <Clock className="w-12 h-12 text-rose-400 animate-pulse" />
              <h3 className="text-lg font-extrabold uppercase tracking-wider text-rose-400">Passagem de Bastão (Shift Handover)</h3>
              <p className="text-xs text-slate-400">Um operador de turno anterior registrou o encerramento das atividades. Você deve revisar as notas de bordo para prosseguir.</p>
            </div>

            <div className="p-4 rounded-xl bg-slate-900/60 border border-white/5 text-left text-xs text-slate-300 flex flex-col gap-2.5 max-h-60 overflow-y-auto">
              <div>
                <span className="text-[9px] uppercase font-bold text-slate-500 block">Operador de Saída</span>
                <span className="font-bold text-white">{activeHandover.outgoing_operator_name}</span>
              </div>
              <div>
                <span className="text-[9px] uppercase font-bold text-slate-500 block">Horário de Saída</span>
                <span className="font-mono text-slate-400">{new Date(activeHandover.created_at).toLocaleString()}</span>
              </div>
              <div>
                <span className="text-[9px] uppercase font-bold text-slate-500 block">Incidentes Críticos Pendentes</span>
                <span className="font-extrabold text-rose-400">{activeHandover.pending_alerts_count} incidentes</span>
              </div>
              <div className="border-t border-white/5 pt-2">
                <span className="text-[9px] uppercase font-bold text-slate-500 block mb-1">Resumo das Atividades / Diário de Bordo</span>
                <p className="whitespace-pre-wrap leading-relaxed italic">"{activeHandover.shift_summary}"</p>
              </div>
            </div>

            <button
              onClick={handleAckHandover}
              className="w-full py-3 rounded-xl bg-emerald-600 hover:bg-emerald-500 text-slate-950 font-extrabold uppercase tracking-wider text-xs transition-all shadow-lg hover:shadow-emerald-500/10 cursor-pointer"
            >
              Confirmar Leitura e Assumir Turno
            </button>
          </div>
        </div>
      )}

      {/* 2.6. Create Shift Handover Modal */}
      {showHandoverModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 backdrop-blur-sm p-4">
          <div className="glass-card w-full max-w-md rounded-2xl p-6 border border-white/10 shadow-2xl flex flex-col gap-4">
            <div className="flex justify-between items-center border-b border-white/5 pb-3">
              <h3 className="text-sm font-extrabold uppercase tracking-wider">Passar Turno (Shift Handover)</h3>
              <button onClick={() => setShowHandoverModal(false)} className="text-slate-500 hover:text-slate-300 text-xs">Fechar</button>
            </div>

            <form onSubmit={handleSubmitHandover} className="flex flex-col gap-4 text-xs">
              <div className="flex flex-col gap-1.5">
                <label className="text-[9px] uppercase font-bold tracking-wider text-slate-400">Resumo das Atividades / Notas de Bordo</label>
                <textarea
                  required
                  value={handoverSummary}
                  onChange={(e) => setHandoverSummary(e.target.value)}
                  placeholder="Descreva o andamento do turno, manutenções em execução ou incidentes críticos herdados..."
                  className="bg-slate-950 border border-white/10 rounded-lg p-3 text-xs text-white focus:outline-none focus:border-violet-500 h-28 resize-none"
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="text-[9px] uppercase font-bold tracking-wider text-slate-400">Quantidade de Alertas Críticos Pendentes</label>
                <input
                  type="number"
                  min="0"
                  value={handoverPendingAlerts}
                  onChange={(e) => setHandoverPendingAlerts(Number(e.target.value))}
                  className="bg-slate-950 border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500"
                />
              </div>

              <button
                type="submit"
                disabled={isSubmittingHandover}
                className="w-full py-3 rounded-xl bg-violet-600 hover:bg-violet-500 text-white font-extrabold uppercase tracking-wider text-xs transition-all cursor-pointer flex items-center justify-center gap-2"
              >
                {isSubmittingHandover && <RefreshCw className="w-4.5 h-4.5 animate-spin" />}
                Registrar Passagem de Turno
              </button>
            </form>
          </div>
        </div>
      )}

      {/* 2.5. Active Users / Operators Online Modal (Admin Only) */}
      {showActiveUsersModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 backdrop-blur-sm p-4 animate-fadeIn">
          <div className="glass-card w-full max-w-2xl h-[500px] rounded-2xl overflow-hidden flex flex-col border border-white/10 shadow-2xl bg-slate-900">
            {/* Header */}
            <div className="px-6 py-4 border-b border-white/5 bg-slate-950/40 flex items-center justify-between">
              <div className="flex items-center gap-2.5">
                <Users className="w-5 h-5 text-emerald-400" />
                <h3 className="text-md font-bold uppercase tracking-wider text-slate-100">Operadores Online no NOC</h3>
              </div>
              <button 
                onClick={() => setShowActiveUsersModal(false)}
                className="text-xs text-slate-400 hover:text-slate-200 bg-white/5 hover:bg-white/10 px-3 py-1.5 rounded-lg transition-all"
              >
                Fechar
              </button>
            </div>

            {/* Content */}
            <div className="flex-1 p-6 overflow-y-auto space-y-4">
              <div className="flex items-center justify-between">
                <p className="text-xs text-slate-400">
                  Lista de sessões ativas com conexão WebSocket estabelecida em tempo real.
                </p>
                <button
                  onClick={fetchActiveUsers}
                  disabled={isLoadingActiveUsers}
                  className="flex items-center gap-1.5 px-2.5 py-1 rounded bg-white/5 hover:bg-white/10 border border-white/10 text-xs text-slate-300 font-medium transition-all"
                >
                  <RefreshCw className={`w-3 h-3 ${isLoadingActiveUsers ? 'animate-spin' : ''}`} />
                  <span>Atualizar</span>
                </button>
              </div>

              {isLoadingActiveUsers ? (
                <div className="flex flex-col items-center justify-center h-64 space-y-3">
                  <RefreshCw className="w-8 h-8 text-emerald-400 animate-spin" />
                  <p className="text-xs text-slate-400">Carregando operadores online...</p>
                </div>
              ) : activeUsers.length === 0 ? (
                <div className="flex flex-col items-center justify-center h-64 space-y-3 border border-dashed border-white/5 rounded-xl bg-white/5">
                  <Users className="w-8 h-8 text-slate-500" />
                  <p className="text-xs text-slate-400 font-medium">Nenhum operador ativo via WebSocket.</p>
                  <p className="text-[10px] text-slate-500 max-w-xs text-center">
                    Geralmente indica que não há sessões abertas no painel Cockpit neste momento.
                  </p>
                </div>
              ) : (
                <div className="grid grid-cols-1 gap-3">
                  {activeUsers.map((activeUser: any) => {
                    const initials = activeUser.name ? activeUser.name.slice(0, 2).toUpperCase() : 'OP';
                    const isCurrentUser = activeUser.email === user?.email;
                    const durationMin = Math.max(1, Math.round((new Date().getTime() - new Date(activeUser.connected_at).getTime()) / 60000));
                    
                    return (
                      <div 
                        key={activeUser.session_id} 
                        className={`flex items-center justify-between p-4 rounded-xl border transition-all ${
                          isCurrentUser 
                            ? 'bg-emerald-950/15 border-emerald-500/30' 
                            : 'bg-white/5 border-white/5 hover:border-white/10'
                        }`}
                      >
                        <div className="flex items-center gap-3">
                          {/* Avatar */}
                          <div className={`w-10 h-10 rounded-xl flex items-center justify-center font-bold text-sm ${
                            isCurrentUser 
                              ? 'bg-emerald-600/20 text-emerald-400 border border-emerald-500/20' 
                              : 'bg-violet-600/20 text-violet-400 border border-violet-500/20'
                          }`}>
                            {initials}
                          </div>
                          
                          {/* Details */}
                          <div>
                            <div className="flex items-center gap-2">
                              <span className="text-sm font-bold text-slate-200">{activeUser.name}</span>
                              {isCurrentUser && (
                                <span className="px-1.5 py-0.5 rounded bg-emerald-500/20 text-emerald-400 text-[9px] font-bold uppercase tracking-wider">
                                  Você
                                </span>
                              )}
                              <span className={`px-1.5 py-0.5 rounded text-[9px] font-bold uppercase tracking-wider ${
                                activeUser.role === 'admin' 
                                  ? 'bg-violet-500/25 text-violet-400' 
                                  : 'bg-blue-500/25 text-blue-400'
                              }`}>
                                {activeUser.role}
                              </span>
                            </div>
                            <div className="text-xs text-slate-400 font-mono mt-0.5">{activeUser.email}</div>
                          </div>
                        </div>

                        {/* Status Pulse & Connected duration */}
                        <div className="flex flex-col items-end gap-1.5">
                          <div className="flex items-center gap-1.5">
                            <span className="relative flex h-2 w-2">
                              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                              <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
                            </span>
                            <span className="text-[10px] text-emerald-400 font-bold uppercase tracking-wider">Online</span>
                          </div>
                          <span className="text-[10px] text-slate-400 font-medium">
                            Conectado há {durationMin} min
                          </span>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {activeSummaryModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4 animate-fade-in">
          <div className="absolute inset-0" onClick={() => setActiveSummaryModal(null)}></div>
          <div className="glass-card bg-[#0b0f19]/95 border border-white/10 rounded-2xl shadow-2xl w-full max-w-4xl max-h-[85vh] flex flex-col overflow-hidden relative z-10">
            {/* Modal Header */}
            <div className="flex items-center justify-between p-5 border-b border-white/5 bg-slate-950/40">
              <div className="flex items-center gap-2.5">
                <Brain className="w-5 h-5 text-violet-400 animate-pulse" />
                <div>
                  <h3 className="text-sm font-bold uppercase tracking-wider text-slate-100">
                    Detalhes dos Alertas: {activeSummaryModal === 'total' ? 'Todos os Alertas Ativos' : activeSummaryModal.toUpperCase()}
                  </h3>
                  <p className="text-[10px] text-slate-400 uppercase tracking-widest mt-0.5">
                    Ações imediatas e triagem rápida de infraestrutura
                  </p>
                </div>
              </div>
              <button 
                onClick={() => setActiveSummaryModal(null)}
                className="text-slate-400 hover:text-white transition-all bg-white/5 hover:bg-white/10 px-3 py-1.5 rounded-lg cursor-pointer text-[10px] uppercase tracking-wider font-extrabold"
              >
                Fechar
              </button>
            </div>

            {/* Modal Body */}
            <div className="flex-1 overflow-y-auto p-5 flex flex-col gap-3">
              {alerts.filter(a => {
                if (!selectedTenantIds.includes(a.tenant_id)) return false;
                if (activeSummaryModal === 'total') return a.status !== 'resolved' && a.status !== 'suppressed';
                return a.severity === activeSummaryModal && a.status !== 'resolved' && a.status !== 'suppressed';
              }).length === 0 ? (
                <div className="text-center py-12 text-slate-500 italic text-xs">
                  Nenhum alerta ativo cadastrado com esta severidade.
                </div>
              ) : (
                alerts.filter(a => {
                  if (!selectedTenantIds.includes(a.tenant_id)) return false;
                  if (activeSummaryModal === 'total') return a.status !== 'resolved' && a.status !== 'suppressed';
                  return a.severity === activeSummaryModal && a.status !== 'resolved' && a.status !== 'suppressed';
                }).map(alert => (
                  <div key={alert.id} className="p-4 rounded-xl bg-white/[0.02] border border-white/5 flex items-center justify-between gap-4 hover:bg-white/[0.04] transition-all">
                    <div className="flex items-start gap-3 flex-1 min-w-0">
                      <span className={`px-2 py-0.5 rounded text-[8px] font-extrabold uppercase tracking-wider ${
                        alert.severity === 'fatal' ? 'bg-rose-500/10 text-rose-400 border border-rose-500/25' :
                        alert.severity === 'critical' ? 'bg-orange-500/10 text-orange-400 border border-orange-500/25' :
                        alert.severity === 'warning' ? 'bg-amber-500/10 text-amber-400 border border-amber-500/25' :
                        'bg-blue-500/10 text-blue-400 border border-blue-500/25'
                      }`}>
                        {alert.severity}
                      </span>
                      <div className="flex flex-col gap-1 min-w-0">
                        <span className="text-xs font-bold text-slate-200 truncate">{alert.summary}</span>
                        <div className="flex items-center gap-3 text-[10px] text-slate-400 uppercase font-semibold">
                          <span>Dispositivo: <strong className="text-slate-300 font-mono">{alert.ai_analysis?.host || 'N/A'}</strong></span>
                          <span>•</span>
                          <span>Evento: <strong className="text-slate-300 font-mono">{alert.event_type}</strong></span>
                          <span>•</span>
                          <span>Horário: <strong className="text-slate-300">{new Date(alert.created_at).toLocaleString()}</strong></span>
                        </div>
                      </div>
                    </div>

                    <div className="flex items-center gap-2">
                      <span className={`px-2 py-0.5 rounded text-[8px] font-extrabold uppercase tracking-widest ${
                        alert.status === 'triggered' ? 'bg-rose-500/10 text-rose-400 animate-pulse' :
                        alert.status === 'acknowledged' ? 'bg-amber-500/10 text-amber-400' :
                        'bg-emerald-500/10 text-emerald-400'
                      }`}>
                        {alert.status}
                      </span>

                      {alert.status === 'triggered' && (
                        <button
                          onClick={() => handleUpdateStatus(alert.id, 'acknowledged')}
                          className="bg-amber-600/20 hover:bg-amber-600/35 border border-amber-500/30 text-amber-300 px-2.5 py-1 rounded text-[10px] font-bold transition-all cursor-pointer uppercase tracking-wider"
                        >
                          Acknowledge
                        </button>
                      )}

                      <button
                        onClick={() => handleUpdateStatus(alert.id, 'resolved')}
                        className="bg-emerald-600/20 hover:bg-emerald-600/35 border border-emerald-500/30 text-emerald-300 px-2.5 py-1 rounded text-[10px] font-bold transition-all cursor-pointer uppercase tracking-wider"
                      >
                        Resolve
                      </button>

                      <button
                        onClick={() => {
                          setSelectedAlert(alert);
                          setCockpitTab('alerts');
                          setActiveSummaryModal(null);
                        }}
                        className="bg-violet-600/20 hover:bg-violet-600/35 border border-violet-500/30 text-violet-300 px-2.5 py-1 rounded text-[10px] font-bold transition-all cursor-pointer uppercase tracking-wider"
                      >
                        Inspecionar
                      </button>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>
      )}

      {simulatorNotification && (
        <div className="fixed bottom-6 right-6 z-50 bg-slate-900/90 backdrop-blur border border-violet-500/30 text-white rounded-xl shadow-2xl p-4 flex items-center gap-3 animate-pulse">
          <div className="w-2 h-2 rounded-full bg-violet-400 animate-ping"></div>
          <div className="flex flex-col">
            <span className="text-[10px] font-bold uppercase tracking-wider text-violet-400">Simulador de Eventos</span>
            <span className="text-xs text-slate-200 mt-0.5">{simulatorNotification}</span>
          </div>
        </div>
      )}
    </div>
  );
}
