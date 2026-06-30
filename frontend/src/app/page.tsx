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
  FileText as RawJsonIcon
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

// 2 Static simulation Tenants with distinct UUIDs
const MOCK_TENANTS = [
  { id: 'e1b7c123-1234-4321-abcd-123456789abc', name: 'Telco Global Corp (Tenant A)', slug: 'telco-global' },
  { id: 'fa2b2345-5678-8765-dcba-987654321fed', name: 'Quantum Cloud Inc (Tenant B)', slug: 'quantum-cloud' }
];

export default function CockpitPage() {
  const [selectedTenant, setSelectedTenant] = useState(MOCK_TENANTS[0]);
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [selectedAlert, setSelectedAlert] = useState<Alert | null>(null);
  const [connStatus, setConnStatus] = useState<'connecting' | 'connected' | 'disconnected'>('disconnected');
  const [searchTerm, setSearchTerm] = useState('');
  const [severityFilter, setSeverityFilter] = useState<string>('all');
  const [activeTab, setActiveTab] = useState<'general' | 'logs' | 'grafana' | 'raw'>('general');
  
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

  // Reset tab to general when selected incident changes
  useEffect(() => {
    if (selectedAlert) {
      setActiveTab('general');
    }
  }, [selectedAlert?.id]);

  // Connect to Go WebSocket Server
  const connectWebSocket = () => {
    if (wsRef.current) {
      wsRef.current.close();
    }

    setConnStatus('connecting');
    // Using the direct Tenant ID UUID as the token parameter to allow the visual simulator to work.
    const wsUrl = `ws://localhost:8080/api/v1/ws?token=${selectedTenant.id}`;
    
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

  // Triggers reconnection when tenant changes
  useEffect(() => {
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
  }, [selectedTenant]);

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

  return (
    <div className="min-h-screen bg-background text-slate-100 flex flex-col font-sans select-none">
      
      {/* 1. Header (Navbar Glass) */}
      <header className="h-16 shrink-0 flex items-center justify-between px-6 border-b border-white/5 bg-surface/50 backdrop-blur-md sticky top-0 z-50">
        <div className="flex items-center gap-3">
          <div className="relative flex items-center justify-center w-8 h-8 rounded-lg bg-violet-600/35 border border-violet-500/40 text-violet-400">
            <Activity className="w-5 h-5 animate-pulse" />
          </div>
          <div>
            <h1 className="text-sm font-bold tracking-wider text-slate-100 flex items-center gap-2">
              ANTIGRAVITY NOC <span className="text-xs px-2 py-0.5 rounded-full bg-violet-900/60 border border-violet-500/30 text-violet-300">2.0 ENGINE</span>
            </h1>
            <p className="text-[10px] text-slate-400 tracking-wide uppercase">Real-Time Cockpit</p>
          </div>
        </div>

        {/* Dynamic Tenant Selector (Multi-tenancy Visual Testbench) */}
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2 px-3 py-1 rounded-lg bg-white/5 border border-white/5">
            <User className="w-4 h-4 text-slate-400" />
            <span className="text-xs text-slate-300 font-medium">Visual Domain:</span>
            <select 
              value={selectedTenant.id} 
              onChange={(e) => {
                const selected = MOCK_TENANTS.find(t => t.id === e.target.value);
                if (selected) setSelectedTenant(selected);
              }}
              className="bg-transparent text-xs text-violet-400 font-bold focus:outline-none cursor-pointer"
            >
              {MOCK_TENANTS.map(t => (
                <option key={t.id} value={t.id} className="bg-surface text-slate-200">{t.name}</option>
              ))}
            </select>
          </div>

          {/* SLA PDF Report Downloader */}
          <button
            onClick={() => {
              window.open(`http://localhost:8080/api/v1/reports/sla?token=${selectedTenant.id}&tenant_name=${encodeURIComponent(selectedTenant.name)}`);
            }}
            className="flex items-center gap-2 px-3 py-1 rounded-lg bg-violet-600/20 hover:bg-violet-600/30 border border-violet-500/35 text-violet-300 text-xs font-bold transition-all uppercase tracking-wider"
          >
            <FileText className="w-3.5 h-3.5" />
            <span>SLA Report</span>
          </button>

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
                      disabled={selectedAlert.status === 'acknowledged' || selectedAlert.status === 'resolved' || selectedAlert.status === 'suppressed'}
                      onClick={() => handleUpdateStatus(selectedAlert.id, 'acknowledged')}
                      className="bg-amber-500/10 hover:bg-amber-500/20 disabled:opacity-40 disabled:hover:bg-amber-500/10 border border-amber-500/30 text-amber-300 py-2 rounded-lg text-xs font-bold uppercase tracking-wider flex items-center justify-center gap-2 transition-all"
                    >
                      Acknowledge
                    </button>
                    <button
                      disabled={selectedAlert.status === 'resolved'}
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
    </div>
  );
}
