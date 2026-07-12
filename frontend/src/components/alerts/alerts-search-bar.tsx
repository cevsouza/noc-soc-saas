import { Search } from 'lucide-react';

export function AlertsSearchBar({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <div className="flex gap-4 items-center">
      <div className="flex-1 relative">
        <Search className="absolute left-3.5 top-2.5 w-4.5 h-4.5 text-slate-500" />
        <input
          type="text"
          placeholder="Search alerts by summary, event type, metadata, source..."
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className="w-full bg-surface/40 hover:bg-surface/60 focus:bg-surface/80 border border-white/5 rounded-xl pl-11 pr-4 py-2.5 text-sm focus:outline-none focus:border-violet-500/50 text-slate-200 transition-all placeholder:text-slate-500"
        />
      </div>
    </div>
  );
}
