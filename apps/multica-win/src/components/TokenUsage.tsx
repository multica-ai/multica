import { useState, useEffect } from 'react';
import { TrendingUp, Calendar, BarChart3, Activity, PieChart, Zap } from 'lucide-react';
import { api } from '../lib/api';

interface Props {
  workspaces: Array<Record<string, any>>;
}

function formatTokens(n: number) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

const BAR_COLORS = [
  'bg-blue-500', 
  'bg-emerald-500', 
  'bg-amber-500', 
  'bg-purple-500', 
  'bg-rose-500', 
  'bg-cyan-500', 
  'bg-orange-500'
];

export default function TokenUsage({ workspaces }: Props) {
  const [summary, setSummary] = useState({ today: 0, week: 0, month: 0 });
  const [daily, setDaily] = useState<Array<{ date: string; tokens: number }>>([]);
  const [byRuntime, setByRuntime] = useState<Array<{ runtime: string; tokens: number }>>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    const load = async () => {
      let today = 0, week = 0, month = 0;
      const d: typeof daily = [];
      const r: typeof byRuntime = [];
      
      try {
        await Promise.all(workspaces.map(async (ws) => {
          const data = await api.getTokenUsage(ws.id as string);
          if (!data) return;
          const s = (data.summary || {}) as Record<string, number>;
          today += s.today || 0;
          week += s.week || 0;
          month += s.month || 0;
          if (Array.isArray(data.daily)) d.push(...(data.daily as typeof daily));
          if (Array.isArray(data.by_runtime)) r.push(...(data.by_runtime as typeof byRuntime));
        }));
        
        if (alive) {
          setSummary({ today, week, month });
          setDaily(d);
          setByRuntime(r);
          setLoading(false);
        }
      } catch (e) {
        console.error('Failed to load usage data', e);
      }
    };

    load();
    const interval = setInterval(load, 30000);
    return () => { alive = false; clearInterval(interval); };
  }, [workspaces]);

  const maxDaily = Math.max(...daily.map((x) => x.tokens), 1);
  const maxRuntime = Math.max(...byRuntime.map((x) => x.tokens), 1);

  const stats = [
    { label: 'Today', value: summary.today, icon: Zap, color: 'text-blue-400' },
    { label: 'This Week', value: summary.week, icon: TrendingUp, color: 'text-emerald-400' },
    { label: 'This Month', value: summary.month, icon: Calendar, color: 'text-amber-400' },
  ];

  return (
    <div className="flex flex-col gap-6 select-none">
      {/* Summary Cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {stats.map((stat) => (
          <div key={stat.label} className="bg-zinc-900 border border-zinc-800 rounded-xl p-4 shadow-sm group hover:border-zinc-700 transition-colors">
            <div className="flex items-center justify-between mb-3">
              <span className="text-[10px] font-bold text-zinc-500 uppercase tracking-widest">{stat.label}</span>
              <stat.icon size={14} className={stat.color} />
            </div>
            <div className="flex items-baseline gap-1">
              <span className="text-2xl font-bold text-zinc-100 tracking-tight">{formatTokens(stat.value)}</span>
              <span className="text-[10px] font-medium text-zinc-500">tokens</span>
            </div>
            {/* Visual indicator bar */}
            <div className="mt-3 w-full h-1 bg-zinc-800 rounded-full overflow-hidden">
               <div className={`h-full transition-all duration-1000 ${stat.color.replace('text-', 'bg-')}`} 
                    style={{ width: loading ? '0%' : '60%' }} />
            </div>
          </div>
        ))}
      </div>

      {/* Daily Usage Chart */}
      {daily.length > 0 && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 shadow-sm">
          <div className="flex items-center gap-2 mb-6">
            <BarChart3 size={16} className="text-zinc-400" />
            <h3 className="text-[11px] font-bold text-zinc-400 uppercase tracking-widest">Daily Usage History</h3>
          </div>
          
          <div className="flex items-end gap-1 sm:gap-2 h-32 px-1">
            {daily.slice(-14).map((d, i) => (
              <div key={i} className="flex-1 group relative flex flex-col items-center gap-2">
                {/* Tooltip */}
                <div className="absolute -top-8 left-1/2 -translate-x-1/2 bg-zinc-800 text-zinc-100 text-[10px] px-2 py-1 rounded opacity-0 group-hover:opacity-100 pointer-events-none transition-opacity whitespace-nowrap z-10 border border-zinc-700">
                  {d.date}: {formatTokens(d.tokens)}
                </div>
                {/* Bar */}
                <div 
                  className="w-full bg-blue-500/80 group-hover:bg-blue-400 rounded-t-sm transition-all duration-500 min-h-[1px]"
                  style={{ height: `${Math.max((d.tokens / maxDaily) * 100, 2)}%` }}
                />
                <span className="text-[9px] font-medium text-zinc-600 group-hover:text-zinc-400 font-mono">
                  {d.date.slice(-5)}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Usage By Runtime */}
      {byRuntime.length > 0 && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 shadow-sm">
          <div className="flex items-center gap-2 mb-6">
            <PieChart size={16} className="text-zinc-400" />
            <h3 className="text-[11px] font-bold text-zinc-400 uppercase tracking-widest">Usage by Runtime</h3>
          </div>
          
          <div className="space-y-5">
            {byRuntime.map((runtime, idx) => (
              <div key={idx} className="space-y-1.5">
                <div className="flex justify-between items-end">
                  <div className="flex items-center gap-2">
                    <div className={`w-2 h-2 rounded-full ${BAR_COLORS[idx % BAR_COLORS.length]}`} />
                    <span className="text-sm font-semibold text-zinc-300 tracking-tight">{runtime.runtime}</span>
                  </div>
                  <div className="flex items-center gap-1">
                    <span className="text-[13px] font-mono font-bold text-zinc-200">{formatTokens(runtime.tokens)}</span>
                    <span className="text-[10px] font-medium text-zinc-500">tokens</span>
                  </div>
                </div>
                <div className="h-2 w-full bg-zinc-800 rounded-full overflow-hidden border border-zinc-800/50">
                  <div 
                    className={`h-full rounded-full transition-all duration-1000 ${BAR_COLORS[idx % BAR_COLORS.length]}`}
                    style={{ width: `${(runtime.tokens / maxRuntime) * 100}%` }}
                  />
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {loading && !daily.length && (
        <div className="flex flex-col items-center justify-center py-12 text-zinc-500 animate-pulse">
           <Activity size={24} className="mb-2" />
           <p className="text-xs">Aggregating token usage metrics...</p>
        </div>
      )}
    </div>
  );
}
