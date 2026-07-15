'use client';

import { useCallback, useEffect, useState } from 'react';
import { CalendarClock, ChevronDown, ChevronRight, Plus, RefreshCw, Trash2, UserCheck } from 'lucide-react';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import type { AdminUser, OncallSchedule, OncallShift } from '@/types';

// On-call scheduling (B5 slice 1). A tenant defines named SCHEDULES (rotations); each schedule has
// SHIFTS assigning a user to a time window. The panel shows who is on-call NOW per schedule, lets an
// admin create schedules, and expand one to add/remove shifts. Self-contained MSP panel, same style as
// the other settings panels. Create/delete require tenant-admin (enforced by the backend). Accepts an
// optional tenantId so it respects the MSP tenant selector.
export function OncallPanel({ tenantId }: { tenantId?: string }) {
  const qtenant = tenantId ? `?tenant_id=${tenantId}` : '';
  const sep = qtenant ? '&' : '?';
  const [schedules, setSchedules] = useState<OncallSchedule[]>([]);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [newName, setNewName] = useState('');
  const [savingSchedule, setSavingSchedule] = useState(false);

  // Expanded schedule + its shifts + the add-shift form.
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [shifts, setShifts] = useState<OncallShift[]>([]);
  const [shiftsLoading, setShiftsLoading] = useState(false);
  const [shiftUser, setShiftUser] = useState('');
  const [shiftStart, setShiftStart] = useState('');
  const [shiftEnd, setShiftEnd] = useState('');
  const [savingShift, setSavingShift] = useState(false);
  const [shiftError, setShiftError] = useState<string | null>(null);

  const fetchSchedules = useCallback(async () => {
    setIsLoading(true);
    try {
      const data = await apiFetchJson<OncallSchedule[]>(`/api/v1/oncall/schedules${qtenant}`);
      setSchedules(data || []);
    } catch (err) {
      console.error('Failed to fetch on-call schedules:', err);
    } finally {
      setIsLoading(false);
    }
  }, [qtenant]);

  useEffect(() => {
    fetchSchedules();
    apiFetchJson<AdminUser[]>('/api/v1/admin/users')
      .then((u) => setUsers(u || []))
      .catch((err) => console.error('Failed to fetch users:', err));
  }, [fetchSchedules]);

  const loadShifts = useCallback(
    async (scheduleId: string) => {
      setShiftsLoading(true);
      setShiftError(null);
      try {
        const data = await apiFetchJson<OncallShift[]>(`/api/v1/oncall/shifts?schedule_id=${scheduleId}${tenantId ? `&tenant_id=${tenantId}` : ''}`);
        setShifts(data || []);
      } catch (err) {
        console.error('Failed to fetch shifts:', err);
        setShifts([]);
      } finally {
        setShiftsLoading(false);
      }
    },
    [tenantId],
  );

  const toggleExpand = (scheduleId: string) => {
    if (expandedId === scheduleId) {
      setExpandedId(null);
      return;
    }
    setExpandedId(scheduleId);
    setShiftUser('');
    setShiftStart('');
    setShiftEnd('');
    setShiftError(null);
    loadShifts(scheduleId);
  };

  const createSchedule = async () => {
    setError(null);
    if (!newName.trim()) {
      setError('Nome da agenda é obrigatório.');
      return;
    }
    setSavingSchedule(true);
    try {
      const res = await apiFetch(`/api/v1/oncall/schedules${qtenant}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newName.trim() }),
      });
      if (!res.ok) {
        setError('Falha ao criar a agenda (requer admin do tenant).');
        return;
      }
      setNewName('');
      fetchSchedules();
    } catch {
      setError('Erro de conectividade com o backend.');
    } finally {
      setSavingSchedule(false);
    }
  };

  const deleteSchedule = async (id: string) => {
    try {
      await apiFetch(`/api/v1/oncall/schedules${qtenant}${sep}id=${id}`, { method: 'DELETE' });
      if (expandedId === id) setExpandedId(null);
      fetchSchedules();
    } catch (err) {
      console.error('Failed to delete schedule:', err);
    }
  };

  const addShift = async (scheduleId: string) => {
    setShiftError(null);
    if (!shiftUser || !shiftStart || !shiftEnd) {
      setShiftError('Selecione o plantonista e o período.');
      return;
    }
    if (new Date(shiftEnd) <= new Date(shiftStart)) {
      setShiftError('O fim deve ser depois do início.');
      return;
    }
    setSavingShift(true);
    try {
      const res = await apiFetch(`/api/v1/oncall/shifts${qtenant}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          schedule_id: scheduleId,
          user_id: shiftUser,
          starts_at: new Date(shiftStart).toISOString(),
          ends_at: new Date(shiftEnd).toISOString(),
        }),
      });
      if (!res.ok) {
        setShiftError('Falha ao adicionar o turno (requer admin do tenant).');
        return;
      }
      setShiftUser('');
      setShiftStart('');
      setShiftEnd('');
      loadShifts(scheduleId);
      fetchSchedules(); // the current on-call assignee may have changed
    } catch {
      setShiftError('Erro de conectividade com o backend.');
    } finally {
      setSavingShift(false);
    }
  };

  const deleteShift = async (scheduleId: string, id: string) => {
    try {
      await apiFetch(`/api/v1/oncall/shifts${qtenant}${sep}id=${id}`, { method: 'DELETE' });
      loadShifts(scheduleId);
      fetchSchedules();
    } catch (err) {
      console.error('Failed to delete shift:', err);
    }
  };

  const fmt = (iso: string) => new Date(iso).toLocaleString();

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center justify-between border-b border-white/5 pb-4">
        <div className="flex flex-col gap-0.5">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
            <CalendarClock className="w-4 h-4 text-emerald-400" /> Plantão (On-Call)
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            Agendas de rotação e quem está de plantão agora
          </p>
        </div>
        <button
          onClick={fetchSchedules}
          disabled={isLoading}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 text-xs text-slate-300 font-bold transition-all cursor-pointer disabled:opacity-50"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${isLoading ? 'animate-spin' : ''}`} /> Atualizar
        </button>
      </div>

      {/* Create schedule */}
      <div className="p-4 rounded-xl bg-black/40 border border-white/5 flex flex-col gap-3">
        <div className="flex flex-col sm:flex-row gap-3">
          <input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="Nova agenda (ex: Plantão NOC 24x7)"
            className="flex-1 bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-emerald-500"
          />
          <button
            onClick={createSchedule}
            disabled={savingSchedule}
            className="flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-slate-950 text-xs font-bold uppercase tracking-wider transition-all cursor-pointer"
          >
            {savingSchedule ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />} Criar agenda
          </button>
        </div>
        {error && <p className="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/20 rounded-lg p-2.5">{error}</p>}
      </div>

      {/* Schedules list */}
      {isLoading && schedules.length === 0 ? (
        <div className="text-xs text-slate-500 py-6 text-center">Carregando…</div>
      ) : schedules.length === 0 ? (
        <div className="text-xs text-slate-500 py-6 text-center">Nenhuma agenda de plantão cadastrada.</div>
      ) : (
        <div className="flex flex-col gap-2">
          {schedules.map((s) => (
            <div key={s.id} className="rounded-lg bg-white/[0.02] border border-white/5 overflow-hidden">
              <div className="p-3 flex items-center justify-between gap-3">
                <button onClick={() => toggleExpand(s.id)} className="flex items-center gap-2 min-w-0 text-left cursor-pointer">
                  {expandedId === s.id ? <ChevronDown className="w-4 h-4 text-slate-400 shrink-0" /> : <ChevronRight className="w-4 h-4 text-slate-400 shrink-0" />}
                  <span className="text-xs font-bold text-slate-200 truncate">{s.name}</span>
                </button>
                <div className="flex items-center gap-2 shrink-0">
                  {s.oncall_name ? (
                    <span className="flex items-center gap-1.5 text-[10px] font-bold text-emerald-300 bg-emerald-500/10 border border-emerald-500/25 rounded-full px-2.5 py-1" title={s.oncall_until ? `Até ${fmt(s.oncall_until)}` : undefined}>
                      <UserCheck className="w-3 h-3" /> {s.oncall_name}
                    </span>
                  ) : (
                    <span className="text-[10px] text-slate-500 uppercase font-bold">Sem plantonista agora</span>
                  )}
                  <button
                    onClick={() => deleteSchedule(s.id)}
                    className="p-1.5 rounded bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 border border-rose-500/20 transition-all cursor-pointer"
                    title="Excluir agenda"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </div>
              </div>

              {expandedId === s.id && (
                <div className="border-t border-white/5 p-3 flex flex-col gap-3 bg-black/20">
                  {/* Add shift */}
                  <div className="grid grid-cols-1 sm:grid-cols-4 gap-2 items-end">
                    <label className="flex flex-col gap-1 text-[10px] text-slate-500 uppercase font-bold">
                      Plantonista
                      <select
                        value={shiftUser}
                        onChange={(e) => setShiftUser(e.target.value)}
                        className="bg-[#0b0f19] border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-emerald-500"
                      >
                        <option value="">Selecione…</option>
                        {users.map((u) => (
                          <option key={u.id} value={u.id}>
                            {u.name} ({u.email})
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="flex flex-col gap-1 text-[10px] text-slate-500 uppercase font-bold">
                      Início
                      <input type="datetime-local" value={shiftStart} onChange={(e) => setShiftStart(e.target.value)} className="bg-[#0b0f19] border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-emerald-500" />
                    </label>
                    <label className="flex flex-col gap-1 text-[10px] text-slate-500 uppercase font-bold">
                      Fim
                      <input type="datetime-local" value={shiftEnd} onChange={(e) => setShiftEnd(e.target.value)} className="bg-[#0b0f19] border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-emerald-500" />
                    </label>
                    <button
                      onClick={() => addShift(s.id)}
                      disabled={savingShift}
                      className="flex items-center justify-center gap-2 px-3 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-slate-950 text-xs font-bold uppercase tracking-wider transition-all cursor-pointer"
                    >
                      {savingShift ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />} Turno
                    </button>
                  </div>
                  {shiftError && <p className="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/20 rounded-lg p-2.5">{shiftError}</p>}

                  {/* Shifts list */}
                  {shiftsLoading ? (
                    <div className="text-xs text-slate-500 py-3 text-center">Carregando turnos…</div>
                  ) : shifts.length === 0 ? (
                    <div className="text-xs text-slate-500 py-3 text-center">Nenhum turno nesta agenda.</div>
                  ) : (
                    <div className="flex flex-col gap-1.5">
                      {shifts.map((sh) => {
                        const now = Date.now();
                        const active = new Date(sh.starts_at).getTime() <= now && new Date(sh.ends_at).getTime() > now;
                        return (
                          <div key={sh.id} className={`p-2.5 rounded-lg border flex items-center justify-between gap-3 ${active ? 'bg-emerald-500/[0.06] border-emerald-500/25' : 'bg-white/[0.02] border-white/5'}`}>
                            <div className="flex flex-col gap-0.5 min-w-0">
                              <div className="flex items-center gap-2">
                                <span className="text-xs font-bold text-slate-200">{sh.user_name}</span>
                                {active && <span className="text-[9px] text-emerald-300 uppercase font-bold">de plantão agora</span>}
                              </div>
                              <span className="text-[10px] text-slate-500">
                                {fmt(sh.starts_at)} → {fmt(sh.ends_at)}
                              </span>
                            </div>
                            <button
                              onClick={() => deleteShift(s.id, sh.id)}
                              className="p-1.5 rounded bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 border border-rose-500/20 transition-all cursor-pointer shrink-0"
                              title="Remover turno"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
