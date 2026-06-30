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
  Check
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
}



const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

const getWSUrl = (tenantId: string) => {
  const base = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
  const host = base.replace(/^https?:\/\//, '');
  
  // Force secure WebSocket (wss) if API base is https OR if the frontend page itself is loaded over https
  let wsProtocol = 'ws';
  if (base.startsWith('https') || (typeof window !== 'undefined' && window.location.protocol === 'https:')) {
    wsProtocol = 'wss';
  }
  return `${wsProtocol}://${host}/api/v1/ws?token=${tenantId}`;
};

export default function CockpitPage() {
  // Authentication States (Bypassed by default for easy access)
  const [token, setToken] = useState<string | null>('bypass-token');
  const [user, setUser] = useState<{ id: string, email: string, name: string, role: string } | null>({
    id: 'd567fae3-a2e6-42d4-bb6e-7119e34b123a',
    email: 'cadu.souza@itfacilservicos.com.br',
    name: 'Cadu Souza',
    role: 'admin'
  });
  const [authView, setAuthView] = useState<'login' | 'register'>('login');
  const [authEmail, setAuthEmail] = useState('');
  const [authPassword, setAuthPassword] = useState('');
  const [authName, setAuthName] = useState('');
  const [authTenant, setAuthTenant] = useState('e1b7c123-1234-4321-abcd-123456789abc');
  const [authStatus, setAuthStatus] = useState<{ status: 'idle' | 'loading' | 'success' | 'error', message?: string }>({ status: 'idle' });

  // Admin User Creation States
  const [adminUserEmail, setAdminUserEmail] = useState('');
  const [adminUserPassword, setAdminUserPassword] = useState('');
  const [adminUserName, setAdminUserName] = useState('');
  const [adminUserRole, setAdminUserRole] = useState('operator');
  const [adminUserStatus, setAdminUserStatus] = useState<{ status: 'idle' | 'saving' | 'success' | 'error', message?: string }>({ status: 'idle' });

  const [tenants, setTenants] = useState<{ id: string, name: string, slug: string }[]>([
    { id: 'e1b7c123-1234-4321-abcd-123456789abc', name: 'Telco Global Corp', slug: 'telco-global' }
  ]);
  const [selectedTenant, setSelectedTenant] = useState<{ id: string, name: string, slug: string }>({
    id: 'e1b7c123-1234-4321-abcd-123456789abc',
    name: 'Telco Global Corp',
    slug: 'telco-global'
  });
  const [newTenantName, setNewTenantName] = useState('');
  const [tenantCreateStatus, setTenantCreateStatus] = useState<{ status: 'idle' | 'saving' | 'success' | 'error', message?: string }>({ status: 'idle' });
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [selectedAlert, setSelectedAlert] = useState<Alert | null>(null);
  const [connStatus, setConnStatus] = useState<'connecting' | 'connected' | 'disconnected'>('disconnected');
  const [searchTerm, setSearchTerm] = useState('');
  const [severityFilter, setSeverityFilter] = useState<string>('all');
  const [activeTab, setActiveTab] = useState<'general' | 'logs' | 'grafana' | 'raw'>('general');
  
  // Integrations Modal States
  const [showIntegrationsModal, setShowIntegrationsModal] = useState(false);
  const [selectedIntegrationTool, setSelectedIntegrationTool] = useState('uptimekuma');
  const [copiedText, setCopiedText] = useState(false);
  
  // Vault secret storage states
  const [vaultKey, setVaultKey] = useState('ssh_private_key');
  const [vaultValue, setVaultValue] = useState('');
  const [saveStatus, setSaveStatus] = useState<{ status: 'idle' | 'saving' | 'success' | 'error', message?: string }>({ status: 'idle' });

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null);

  // Stats computation
  const stats = {
    total: alerts.length,
    fatal: alerts.filter(a => a.severity === 'fatal' && a.status !== 'resolved' && a.status !== 'suppressed').length,
    critical: alerts.filter(a => a.severity === 'critical' && a.status !== 'resolved' && a.status !== 'suppressed').length,
    warning: alerts.filter(a => a.severity === 'warning' && a.status !== 'resolved' && a.status !== 'suppressed').length,
    info: alerts.filter(a => a.severity === 'info' && a.status !== 'resolved' && a.status !== 'suppressed').length,
  };

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
      // Omitir autenticação temporariamente: Login automático como SRE Admin
      setToken('bypass-token');
      setUser({
        id: 'd567fae3-a2e6-42d4-bb6e-7119e34b123a',
        email: 'cadu.souza@itfacilservicos.com.br',
        name: 'Cadu Souza',
        role: 'admin'
      });
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
        }
      }
    } catch (err) {
      console.error("Falha ao buscar tenants:", err);
    }
  };

  useEffect(() => {
    if (token) {
      fetchTenants();
    }
  }, [token]);

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
    setAuthStatus({ status: 'loading' });
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/auth/register`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: authEmail, password: authPassword, name: authName, tenant_id: authTenant })
      });
      if (response.ok) {
        setAuthStatus({ status: 'success', message: 'Conta criada! Por favor, verifique seu e-mail para ativar.' });
        setAuthEmail('');
        setAuthPassword('');
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
          role: adminUserRole,
          tenant_id: selectedTenant.id
        })
      });
      if (response.ok) {
        setAdminUserStatus({ status: 'success', message: 'Novo usuário cadastrado e e-mail enviado!' });
        setAdminUserEmail('');
        setAdminUserPassword('');
        setAdminUserName('');
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

  // Connect to Go WebSocket Server
  const connectWebSocket = () => {
    if (!token) return;

    if (wsRef.current) {
      wsRef.current.close();
    }

    setConnStatus('connecting');
    const wsUrl = getWSUrl(token);
    
    const socket = new WebSocket(wsUrl);
    wsRef.current = socket;

    socket.onopen = () => {
      setConnStatus('connected');
      console.log(`WebSocket connected to tenant: ${selectedTenant.name}`);
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
    connectWebSocket();

    return () => {
      if (wsRef.current) {
        wsRef.current.close();
      }
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
    };
  }, [selectedTenant, token]);

  // Handle action triggers (simulate backend state changes locally or via fetch)
  const handleUpdateStatus = (alertId: string, newStatus: Alert['status']) => {
    setAlerts(prevAlerts => {
      const updated = prevAlerts.map(a => {
        if (a.id === alertId) {
          const updatedAlert: Alert = {
            ...a,
            status: newStatus,
            resolved_at: newStatus === 'resolved' ? new Date().toISOString() : undefined,
            updated_at: new Date().toISOString()
          };
          if (selectedAlert && selectedAlert.id === alertId) {
            setSelectedAlert(updatedAlert);
          }
          return updatedAlert;
        }
        return a;
      });
      return updated;
    });
  };

  // Filter alerts based on search and buttons
  const filteredAlerts = alerts.filter(a => {
    const matchesSearch = a.summary.toLowerCase().includes(searchTerm.toLowerCase()) || 
                          a.event_type.toLowerCase().includes(searchTerm.toLowerCase()) ||
                          (a.ai_analysis?.source || '').toLowerCase().includes(searchTerm.toLowerCase());
    const matchesSeverity = severityFilter === 'all' || a.severity === severityFilter;
    return matchesSearch && matchesSeverity;
  });

  // Simulator helper function
  const simulateEvent = async (type: 'cpu' | 'memory' | 'wazuh') => {
    try {
      let url = '';
      let payload: any = {};
      
      if (type === 'cpu') {
        url = `${API_BASE_URL}/api/v1/ingest/prometheus?token=${selectedTenant.id}`;
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
        url = `${API_BASE_URL}/api/v1/ingest/prometheus?token=${selectedTenant.id}`;
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
        url = `${API_BASE_URL}/api/v1/ingest/wazuh?token=${selectedTenant.id}`;
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
        console.log(`Simulation of type ${type} successfully sent to backend API.`);
      }
    } catch (err) {
      console.error("Simulation dispatch failed:", err);
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



  return (
    <div className="min-h-screen bg-background text-slate-100 flex flex-col font-sans select-none">
      
      {/* 1. Header (Navbar Glass) */}
      <header className="h-16 shrink-0 flex items-center justify-between px-6 border-b border-white/5 bg-surface/50 backdrop-blur-md sticky top-0 z-50">
        <div className="flex items-center gap-3">
          <div className="relative flex items-center justify-center h-8 w-24 overflow-hidden rounded-lg bg-white/5 p-1 border border-white/10">
            <img 
              src="https://lirp.cdn-website.com/2cf4cfdc/dms3rep/multi/opt/IT+Facil+-+logo+-+alta-47c0885e-158w.png" 
              alt="ITFácil Logo" 
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
            <div className="flex items-center gap-2 px-3 py-1 rounded-lg bg-white/5 border border-white/5">
              <User className="w-4 h-4 text-slate-400" />
              <span className="text-xs text-slate-300 font-medium">Visual Domain:</span>
              <select 
                value={selectedTenant.id} 
                onChange={(e) => {
                  const selected = tenants.find(t => t.id === e.target.value);
                  if (selected) setSelectedTenant(selected);
                }}
                className="bg-transparent text-xs text-violet-400 font-bold focus:outline-none cursor-pointer"
              >
                {tenants.map(t => (
                  <option key={t.id} value={t.id} className="bg-surface text-slate-200">{t.name}</option>
                ))}
              </select>
            </div>
          ) : (
            <div className="flex items-center gap-2 px-3 py-1 rounded-lg bg-white/5 border border-white/5">
              <User className="w-4 h-4 text-slate-400" />
              <span className="text-xs text-slate-300 font-medium">Tenant:</span>
              <span className="text-xs text-violet-400 font-bold">{selectedTenant.name}</span>
            </div>
          )}

          {/* Connections / Integrations Manager Trigger (Hidden for viewers) */}
          {user?.role !== 'viewer' && (
            <button
              onClick={() => setShowIntegrationsModal(true)}
              className="flex items-center gap-2 px-3 py-1 rounded-lg bg-cyan-600/20 hover:bg-cyan-600/30 border border-cyan-500/35 text-cyan-300 text-xs font-bold transition-all uppercase tracking-wider"
            >
              <LinkIcon className="w-3.5 h-3.5" />
              <span>Integrações</span>
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

          {/* Connection Status Badge */}
          <div className={`flex items-center gap-2 px-3 py-1 rounded-full text-xs font-semibold border ${
            connStatus === 'connected' 
              ? 'bg-emerald-500/10 border-emerald-500/30 text-emerald-400' 
              : connStatus === 'connecting'
                ? 'bg-amber-500/10 border-amber-500/30 text-amber-400'
                : 'bg-rose-500/10 border-rose-500/30 text-rose-400'
          }`}>
            {connStatus === 'connected' ? (
              <>
                <Wifi className="w-3.5 h-3.5" />
                <span>CONNECTED</span>
              </>
            ) : connStatus === 'connecting' ? (
              <>
                <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                <span>CONNECTING</span>
              </>
            ) : (
              <>
                <WifiOff className="w-3.5 h-3.5" />
                <span>DISCONNECTED</span>
              </>
            )}
          </div>

          {/* User Profile (Sem Logout) */}
          <div className="flex items-center gap-3 px-3 py-1 rounded-lg bg-white/5 border border-white/5 ml-2">
            <div className="flex flex-col text-right">
              <span className="text-[10px] text-white font-bold leading-none">{user?.name}</span>
              <span className="text-[8px] text-slate-400 uppercase tracking-widest font-semibold">{user?.role}</span>
            </div>
          </div>
        </div>
      </header>

      {/* 2. Main Dashboard Layout */}
      <main className="flex-1 flex overflow-hidden">
        
        {/* Left Section (Stats + Alerts Feed) */}
        <section className="flex-1 flex flex-col p-6 overflow-y-auto gap-6 max-w-7xl mx-auto w-full">
          
          {/* Stat Cards */}
          <div className="grid grid-cols-5 gap-4">
            <div className="glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer" onClick={() => setSeverityFilter('all')}>
              <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
                <Layers className="w-3.5 h-3.5 text-violet-400" /> Active Alerts
              </span>
              <span className="text-3xl font-extrabold tracking-tight text-white">{stats.total}</span>
              <div className="h-1 bg-violet-600/30 rounded mt-2 overflow-hidden">
                <div className="h-full bg-violet-500 rounded" style={{ width: '100%' }}></div>
              </div>
            </div>

            <div className={`glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer transition-all ${
              severityFilter === 'fatal' ? 'glass-card-active border-severity-fatal/50' : ''
            }`} onClick={() => setSeverityFilter('fatal')}>
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

            <div className={`glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer transition-all ${
              severityFilter === 'critical' ? 'glass-card-active border-severity-critical/50' : ''
            }`} onClick={() => setSeverityFilter('critical')}>
              <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
                <AlertOctagon className="w-3.5 h-3.5 text-severity-critical" /> Critical
              </span>
              <span className="text-3xl font-extrabold tracking-tight text-white">{stats.critical}</span>
              <div className="h-1 bg-severity-critical/20 rounded mt-2 overflow-hidden">
                <div className="h-full bg-severity-critical rounded" style={{ width: stats.total > 0 ? `${(stats.critical / stats.total) * 100}%` : '0%' }}></div>
              </div>
            </div>

            <div className={`glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer transition-all ${
              severityFilter === 'warning' ? 'glass-card-active border-severity-warning/50' : ''
            }`} onClick={() => setSeverityFilter('warning')}>
              <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
                <AlertTriangle className="w-3.5 h-3.5 text-severity-warning" /> Warnings
              </span>
              <span className="text-3xl font-extrabold tracking-tight text-white">{stats.warning}</span>
              <div className="h-1 bg-severity-warning/20 rounded mt-2 overflow-hidden">
                <div className="h-full bg-severity-warning rounded" style={{ width: stats.total > 0 ? `${(stats.warning / stats.total) * 100}%` : '0%' }}></div>
              </div>
            </div>

            <div className={`glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer transition-all ${
              severityFilter === 'info' ? 'glass-card-active border-severity-info/50' : ''
            }`} onClick={() => setSeverityFilter('info')}>
              <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
                <Info className="w-3.5 h-3.5 text-severity-info" /> Informational
              </span>
              <span className="text-3xl font-extrabold tracking-tight text-white">{stats.info}</span>
              <div className="h-1 bg-severity-info/20 rounded mt-2 overflow-hidden">
                <div className="h-full bg-severity-info rounded" style={{ width: stats.total > 0 ? `${(stats.info / stats.total) * 100}%` : '0%' }}></div>
              </div>
            </div>
          </div>

          {/* NOC/SOC Sandbox Simulator (Para Iniciantes, Oculto para Viewers) */}
          {user?.role !== 'viewer' && (
            <div className="glass-card p-5 rounded-xl border border-white/5 bg-surface/20 flex flex-col gap-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Brain className="w-4 h-4 text-violet-400" />
                  <h4 className="text-xs font-bold uppercase tracking-wider text-slate-200">
                    Simulador de Alertas (Painel de Treinamento NOC/SOC)
                  </h4>
                </div>
                <span className="text-[9px] font-bold text-slate-500 bg-white/5 px-2 py-0.5 rounded font-mono">
                  MODO DIDÁTICO
                </span>
              </div>
              <p className="text-xs text-slate-400 leading-relaxed">
                Clique nos botões abaixo para gerar e injetar alertas simulados na API e ver a triagem, de-duplicação e supressão em tempo real. Útil para demonstrações rápidas e treinamento de novos analistas!
              </p>
              <div className="flex gap-3 flex-wrap">
                <button
                  onClick={() => simulateEvent('cpu')}
                  className="flex-1 bg-violet-600/10 hover:bg-violet-600/20 border border-violet-500/30 text-violet-300 py-2.5 px-3 rounded-lg text-xs font-bold flex items-center justify-center gap-2 transition-all"
                >
                  <Cpu className="w-3.5 h-3.5" />
                  <span>Simular Sobrecarga CPU (Prometheus)</span>
                </button>
                <button
                  onClick={() => simulateEvent('memory')}
                  className="flex-1 bg-cyan-600/10 hover:bg-cyan-600/20 border border-cyan-500/30 text-cyan-300 py-2.5 px-3 rounded-lg text-xs font-bold flex items-center justify-center gap-2 transition-all"
                >
                  <Layers className="w-3.5 h-3.5" />
                  <span>Simular Falta Memória (Prometheus)</span>
                </button>
                <button
                  onClick={() => simulateEvent('wazuh')}
                  className="flex-1 bg-blue-600/10 hover:bg-blue-600/20 border border-blue-500/30 text-blue-300 py-2.5 px-3 rounded-lg text-xs font-bold flex items-center justify-center gap-2 transition-all"
                >
                  <Terminal className="w-3.5 h-3.5" />
                  <span>Simular Ataque SSH (Wazuh SIEM)</span>
                </button>
              </div>
            </div>
          )}

          {/* Search and Filters */}
          <div className="flex gap-4">
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

          {/* Alerts Table/Feed */}
          <div className="glass-card rounded-xl overflow-hidden flex flex-col border border-white/5">
            <div className="grid grid-cols-12 gap-4 px-6 py-3 border-b border-white/5 bg-surface/30 text-[10px] tracking-wider uppercase font-semibold text-slate-400">
              <div className="col-span-1">Severity</div>
              <div className="col-span-1 text-center">Source</div>
              <div className="col-span-2">Event Type</div>
              <div className="col-span-3">Summary</div>
              <div className="col-span-1 text-center">Debounce</div>
              <div className="col-span-2">Time Received</div>
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

                    {/* Event Type */}
                    <div className="col-span-2 font-mono text-xs text-slate-300 font-bold flex items-center gap-1.5 truncate">
                      <Terminal className="w-3.5 h-3.5 text-slate-500" />
                      {alert.event_type}
                    </div>

                    {/* Summary */}
                    <div className="col-span-3 text-slate-200 font-medium truncate">
                      {alert.summary}
                    </div>

                    {/* Occurrences count (Redis Debounce Metrics) */}
                    <div className="col-span-1 text-center font-mono text-xs">
                      {alert.payload?.occurrences ? (
                        <span className={`inline-block px-2 py-0.5 rounded font-bold ${
                          alert.payload.occurrences > 1
                            ? 'bg-violet-500/10 text-violet-400 border border-violet-500/20'
                            : 'bg-white/5 text-slate-500'
                        }`}>
                          x{alert.payload.occurrences}
                        </span>
                      ) : (
                        <span className="text-slate-600">-</span>
                      )}
                    </div>

                    {/* Timestamp */}
                    <div className="col-span-2 text-xs text-slate-400 font-mono">
                      {new Date(alert.created_at).toLocaleTimeString()}
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
        </section>

        {/* Right Section (Glass detail Sidebar Panel) */}
        {selectedAlert && (
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
                Raw JSON
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

                  {/* AI & Noise Heuristics */}
                  <div className="flex flex-col gap-3 p-5 rounded-xl bg-cyan-950/10 border border-cyan-500/20">
                    <div className="flex items-center gap-2">
                      <Brain className="w-4 h-4 text-cyan-400" />
                      <h5 className="text-xs font-extrabold uppercase text-cyan-400 tracking-wider">AI & Suppression Insight</h5>
                    </div>
                    <p className="text-xs text-slate-300 leading-relaxed">
                      {selectedAlert.status === 'suppressed' || selectedAlert.ai_analysis?.suppressed ? (
                        <>
                          <strong className="text-rose-400">ALERT SUPPRESSED:</strong> {selectedAlert.ai_analysis?.suppression_reason || 'Alert flagged as flapping noise.'}
                        </>
                      ) : selectedAlert.ai_analysis?.downgraded ? (
                        <>
                          <strong className="text-amber-400">SEVERITY DOWNGRADED:</strong> {selectedAlert.ai_analysis?.downgrade_reason}
                        </>
                      ) : (
                        <>No suppression or downgrades triggered. Event frequency is within stable bounds.</>
                      )}
                    </p>
                    <div className="flex items-center justify-between text-[10px] text-slate-500 font-semibold mt-1">
                      <span>Noise Filter Applied: Yes</span>
                      <span>Signal Strength: 100%</span>
                    </div>
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
            </div>
          </aside>
        )}

      </main>

      {/* 3. Didactic Connections / Integrations Modal */}
      {showIntegrationsModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 backdrop-blur-sm p-4 animate-fadeIn">
          <div className="glass-card w-full max-w-4xl h-[600px] rounded-2xl overflow-hidden flex flex-col border border-white/10 shadow-2xl">
            
            {/* Modal Header */}
            <div className="px-6 py-4 border-b border-white/5 bg-surface/30 flex items-center justify-between">
              <div className="flex items-center gap-2">
                <LinkIcon className="w-5 h-5 text-cyan-400" />
                <h3 className="text-md font-bold uppercase tracking-wider">Painel de Conexões e Integrações (Connectors)</h3>
              </div>
              <button 
                onClick={() => {
                  setShowIntegrationsModal(false);
                  setSaveStatus({ status: 'idle' });
                }}
                className="text-xs text-slate-400 hover:text-slate-200 bg-white/5 hover:bg-white/10 px-3 py-1.5 rounded-lg transition-all"
              >
                Fechar
              </button>
            </div>

            {/* Modal Content */}
            <div className="flex-1 flex overflow-hidden">
              
              {/* Left Column (Tool list) */}
              <div className="w-[220px] bg-slate-950/20 border-r border-white/5 overflow-y-auto flex flex-col p-3 gap-1">
                <span className="text-[9px] font-bold text-slate-500 uppercase tracking-widest px-2.5 py-2">Push Webhooks</span>
                {[
                  { id: 'uptimekuma', name: 'Uptime Kuma', color: 'text-emerald-400' },
                  { id: 'zabbix', name: 'Zabbix Monitor', color: 'text-rose-400' },
                  { id: 'prometheus', name: 'Prometheus / Alert', color: 'text-purple-400' },
                  { id: 'wazuh', name: 'Wazuh SIEM', color: 'text-blue-400' },
                  { id: 'grafana', name: 'Grafana Webhook', color: 'text-violet-400' }
                ].map(tool => (
                  <button
                    key={tool.id}
                    onClick={() => {
                      setSelectedIntegrationTool(tool.id);
                      setSaveStatus({ status: 'idle' });
                    }}
                    className={`px-3 py-2.5 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                      selectedIntegrationTool === tool.id ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                    }`}
                  >
                    <span className={`w-2 h-2 rounded-full ${selectedIntegrationTool === tool.id ? 'bg-cyan-400' : 'bg-slate-600'}`}></span>
                    {tool.name}
                  </button>
                ))}

                <span className="text-[9px] font-bold text-slate-500 uppercase tracking-widest px-2.5 py-2 mt-4">Secure Vault (Pull)</span>
                {[
                  { id: 'sentinel', name: 'Microsoft Sentinel' },
                  { id: 'loki', name: 'Grafana Loki' },
                  { id: 'ssh', name: 'Credenciais SSH Runbook' }
                ].map(tool => (
                  <button
                    key={tool.id}
                    onClick={() => {
                      setSelectedIntegrationTool(tool.id);
                      setSaveStatus({ status: 'idle' });
                      if (tool.id === 'sentinel') setVaultKey('sentinel_client_secret');
                      else if (tool.id === 'loki') setVaultKey('loki_password');
                      else if (tool.id === 'ssh') setVaultKey('ssh_private_key');
                    }}
                    className={`px-3 py-2.5 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                      selectedIntegrationTool === tool.id ? 'bg-white/5 text-white border-l-2 border-cyan-400' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                    }`}
                  >
                    <span className={`w-2 h-2 rounded-full ${selectedIntegrationTool === tool.id ? 'bg-cyan-400' : 'bg-slate-600'}`}></span>
                    {tool.name}
                  </button>
                ))}

                {user?.role === 'admin' && (
                  <>
                    <span className="text-[9px] font-bold text-slate-500 uppercase tracking-widest px-2.5 py-2 mt-4">Administração</span>
                    <button
                      onClick={() => {
                        setSelectedIntegrationTool('users_admin');
                        setAdminUserStatus({ status: 'idle' });
                      }}
                      className={`px-3 py-2.5 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                        selectedIntegrationTool === 'users_admin' ? 'bg-white/5 text-white border-l-2 border-violet-500' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                      }`}
                    >
                      <span className={`w-2 h-2 rounded-full ${selectedIntegrationTool === 'users_admin' ? 'bg-violet-500' : 'bg-slate-600'}`}></span>
                      Usuários (Admin)
                    </button>
                    <button
                      onClick={() => {
                        setSelectedIntegrationTool('tenants_admin');
                        setTenantCreateStatus({ status: 'idle' });
                      }}
                      className={`px-3 py-2.5 rounded-lg text-left text-xs font-bold transition-all flex items-center gap-2 ${
                        selectedIntegrationTool === 'tenants_admin' ? 'bg-white/5 text-white border-l-2 border-violet-500' : 'text-slate-400 hover:bg-white/[0.02] hover:text-slate-200'
                      }`}
                    >
                      <span className={`w-2 h-2 rounded-full ${selectedIntegrationTool === 'tenants_admin' ? 'bg-violet-500' : 'bg-slate-600'}`}></span>
                      Tenants (Admin)
                    </button>
                  </>
                )}
              </div>

              {/* Right Column (Instructions & Credentials Form) */}
              <div className="flex-1 p-6 overflow-y-auto flex flex-col gap-6 bg-[#090d16]">
                
                {/* 1. Header of Tool */}
                <div className="flex items-center gap-3">
                  <div className="w-10 h-10 rounded-lg bg-cyan-950/20 border border-cyan-500/20 flex items-center justify-center text-cyan-400">
                    <HelpCircle className="w-6 h-6" />
                  </div>
                  <div>
                    <h4 className="text-sm font-bold uppercase text-white tracking-wide">
                      {selectedIntegrationTool === 'uptimekuma' && 'Integração Uptime Kuma'}
                      {selectedIntegrationTool === 'zabbix' && 'Integração Zabbix Monitor'}
                      {selectedIntegrationTool === 'prometheus' && 'Integração Prometheus Alertmanager'}
                      {selectedIntegrationTool === 'wazuh' && 'Integração Wazuh SIEM'}
                      {selectedIntegrationTool === 'grafana' && 'Integração Grafana Alerts'}
                      {selectedIntegrationTool === 'sentinel' && 'Conexão Microsoft Sentinel'}
                      {selectedIntegrationTool === 'loki' && 'Conexão Grafana Loki'}
                      {selectedIntegrationTool === 'ssh' && 'Cofre de Credenciais SSH'}
                      {selectedIntegrationTool === 'users_admin' && 'Gerenciamento de Equipe e Permissões'}
                      {selectedIntegrationTool === 'tenants_admin' && 'Gerenciamento de Multi-tenancy (Empresas)'}
                    </h4>
                    <p className="text-[10px] text-slate-500 uppercase tracking-widest font-bold">
                      {selectedIntegrationTool === 'users_admin' || selectedIntegrationTool === 'tenants_admin' ? 'Método: Administração Local / Cadastro' : ['uptimekuma', 'zabbix', 'prometheus', 'wazuh', 'grafana'].includes(selectedIntegrationTool) ? 'Método: Webhook (Push / Envio de Alertas)' : 'Método: API Polling (Pull / Busca Ativa de Chaves)'}
                    </p>
                  </div>
                </div>

                {/* 2. Webhook URLs (Push) */}
                {['uptimekuma', 'zabbix', 'prometheus', 'wazuh', 'grafana'].includes(selectedIntegrationTool) ? (
                  <div className="flex flex-col gap-4">
                    <div className="flex flex-col gap-2">
                      <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400">
                        Sua URL de Ingestão Exclusiva (Webhook URL)
                      </label>
                      <div className="flex bg-[#040811] border border-white/5 rounded-lg overflow-hidden p-2.5 items-center justify-between font-mono text-xs text-cyan-400 select-all select-text">
                        <span className="truncate mr-3">
                          {`${API_BASE_URL}/api/v1/ingest/${selectedIntegrationTool}?token=${selectedTenant.id}`}
                        </span>
                        <button
                          onClick={() => handleCopyWebhookUrl(`${API_BASE_URL}/api/v1/ingest/${selectedIntegrationTool}?token=${selectedTenant.id}`)}
                          className="p-1.5 rounded bg-white/5 hover:bg-white/10 text-slate-400 hover:text-white transition-all shrink-0"
                          title="Copiar URL"
                        >
                          {copiedText ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
                        </button>
                      </div>
                    </div>

                    <div className="flex flex-col gap-3 p-4 rounded-xl bg-slate-900/40 border border-white/5 text-xs text-slate-300 leading-relaxed font-sans">
                      <h5 className="font-bold text-slate-200 uppercase tracking-wider text-[10px]">Como configurar na sua ferramenta:</h5>
                      
                      {selectedIntegrationTool === 'uptimekuma' && (
                        <p>No seu painel do <b>Uptime Kuma</b>, vá em <i>Configurações &gt; Notificações &gt; Adicionar Notificação</i>. Defina o tipo de notificação como <b>Webhook</b>, cole a URL acima no campo <b>Post URL</b> e salve. O Uptime Kuma enviará notificações automáticas de Down/Up.</p>
                      )}
                      
                      {selectedIntegrationTool === 'zabbix' && (
                        <p>No <b>Zabbix</b>, vá em <i>Administration &gt; Media Types</i> e crie um novo tipo de mídia como <b>Webhook</b>. Defina os parâmetros padrão (como `EventID`, `Host`, `Severity`, `AlertMessage`) e insira a URL acima na requisição HTTP POST.</p>
                      )}

                      {selectedIntegrationTool === 'prometheus' && (
                        <div className="flex flex-col gap-2">
                          <p>No seu arquivo de configuração do <b>Alertmanager</b> (`alertmanager.yml`), defina um receiver de webhook apontando para a nossa URL:</p>
                          <pre className="bg-[#03060f] p-3 rounded-lg font-mono text-[10px] text-slate-400 overflow-x-auto leading-relaxed border border-white/5">
{`receivers:
  - name: 'noc-soc-webhook'
    webhook_configs:
      - url: '${API_BASE_URL}/api/v1/ingest/prometheus?token=${selectedTenant.id}'`}
                          </pre>
                        </div>
                      )}

                      {selectedIntegrationTool === 'wazuh' && (
                        <div className="flex flex-col gap-2">
                          <p>No arquivo `/var/ossec/etc/ossec.conf` do seu <b>Wazuh Manager</b>, registre um bloco de integração HTTP:</p>
                          <pre className="bg-[#03060f] p-3 rounded-lg font-mono text-[10px] text-slate-400 overflow-x-auto leading-relaxed border border-white/5">
{`<integration>
  <name>custom-webhook</name>
  <hook_url>${API_BASE_URL}/api/v1/ingest/wazuh?token=${selectedTenant.id}</hook_url>
  <alert_format>json</alert_format>
  <level>7</level>
</integration>`}
                          </pre>
                        </div>
                      )}

                      {selectedIntegrationTool === 'grafana' && (
                        <p>No <b>Grafana</b>, vá em <i>Alerting &gt; Contact Points &gt; New Contact Point</i>. Escolha o tipo <b>Webhook</b>, insira a URL acima no campo de URL e salve. O Grafana enviará notificações completas de alerta.</p>
                      )}
                    </div>
                  </div>
                ) : ['sentinel', 'loki', 'ssh'].includes(selectedIntegrationTool) ? (
                  // 3. Vault forms (Pull / Sentinel & Loki credentials saving)
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
                      className="bg-cyan-600 hover:bg-cyan-500 disabled:bg-cyan-800 disabled:opacity-50 text-slate-950 font-bold uppercase tracking-wider text-xs py-2.5 rounded-lg flex items-center justify-center gap-2 transition-all mt-2"
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
                  </form>
                ) : selectedIntegrationTool === 'users_admin' ? (
                  // 4. Admin Users Form
                  <div className="flex flex-col gap-4">
                    <div className="flex flex-col gap-3 p-4 rounded-xl bg-violet-950/10 border border-violet-500/20 text-xs text-slate-300 leading-relaxed font-sans mb-2">
                      <div className="flex items-center gap-1.5 text-violet-400 font-extrabold uppercase text-[10px]">
                        <User className="w-3.5 h-3.5" /> Painel de Controle de Usuários (RBAC)
                      </div>
                      <p>Como administrador do NOC, você pode cadastrar e gerenciar perfis de novos colaboradores. Escolha se o nível de privilégio será **Admin** (acesso irrestrito), **Operator** (gerenciamento e SLA) ou **Viewer** (somente visualização).</p>
                    </div>

                    <form onSubmit={handleAdminCreateUser} className="flex flex-col gap-4 max-w-md">
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
                        <input
                          type="password"
                          required
                          value={adminUserPassword}
                          onChange={(e) => setAdminUserPassword(e.target.value)}
                          placeholder="Mínimo de 6 caracteres"
                          className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-violet-500 transition-all placeholder:text-slate-600"
                        />
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
                        className="bg-gradient-to-r from-violet-600 to-indigo-600 hover:from-violet-500 hover:to-indigo-500 text-white font-bold text-xs py-3 px-4 rounded-lg transition-all shadow-md shadow-violet-950/30 flex items-center justify-center gap-2"
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
                  </div>
                ) : selectedIntegrationTool === 'tenants_admin' ? (
                  // 5. Admin Tenants Form
                  <div className="flex flex-col gap-4">
                    <div className="flex flex-col gap-3 p-4 rounded-xl bg-violet-950/10 border border-violet-500/20 text-xs text-slate-300 leading-relaxed font-sans mb-2">
                      <div className="flex items-center gap-1.5 text-violet-400 font-extrabold uppercase text-[10px]">
                        <Activity className="w-3.5 h-3.5" /> Painel de Controle de Tenants (Multi-tenancy)
                      </div>
                      <p>Adicione novos Tenants (clientes, empresas ou divisões internas) para segmentação física e isolamento de alertas. Cada novo Tenant ganha um UUID exclusivo para ser configurado nas integrações.</p>
                    </div>

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                      {/* Left: Create Form */}
                      <form onSubmit={handleCreateTenant} className="flex flex-col gap-4">
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
                          className="bg-gradient-to-r from-violet-600 to-indigo-600 hover:from-violet-500 hover:to-indigo-500 text-white font-bold text-xs py-3 px-4 rounded-lg transition-all shadow-md shadow-violet-950/30 flex items-center justify-center gap-2"
                        >
                          {tenantCreateStatus.status === 'saving' && <RefreshCw className="w-3.5 h-3.5 animate-spin" />}
                          Cadastrar Novo Tenant
                        </button>

                        {tenantCreateStatus.status === 'success' && (
                          <div className="p-3 bg-emerald-950/20 border border-emerald-500/20 text-emerald-400 text-xs rounded-lg font-sans">
                            {tenantCreateStatus.message}
                          </div>
                        )}
                        {tenantCreateStatus.status === 'error' && (
                          <div className="p-3 bg-rose-950/20 border border-rose-500/20 text-rose-400 text-xs rounded-lg font-sans">
                            {tenantCreateStatus.message}
                          </div>
                        )}
                      </form>

                      {/* Right: Active Tenants List */}
                      <div className="flex flex-col gap-3">
                        <label className="text-[10px] uppercase font-bold tracking-wider text-slate-400 block">Tenants Ativos no Banco</label>
                        <div className="flex flex-col gap-2 max-h-[300px] overflow-y-auto pr-1">
                          {tenants.map(t => (
                            <div key={t.id} className="p-3 rounded-lg bg-white/5 border border-white/5 flex flex-col gap-1">
                              <span className="text-xs font-bold text-slate-200">{t.name}</span>
                              <span className="text-[10px] font-mono text-slate-500 select-all truncate">{t.id}</span>
                            </div>
                          ))}
                        </div>
                      </div>
                    </div>
                  </div>
                ) : null}

              </div>
            </div>

          </div>
        </div>
      )}

    </div>
  );
}
