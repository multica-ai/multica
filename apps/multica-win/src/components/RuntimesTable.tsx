import type { ReactNode } from 'react';
import { Cpu, Activity, Clock, Server } from 'lucide-react';

interface Props {
  runtimes: Array<Record<string, any>>;
}

const PROVIDER_ICONS: Record<string, ReactNode> = {
  claude: <span className="text-orange-500">🟠</span>,
  codex: <span className="text-green-500">🟢</span>,
  gemini: <span className="text-blue-500">🔵</span>,
  opencode: <span className="text-zinc-400">⚪</span>,
  openclaw: <span className="text-purple-500">🟣</span>,
  hermes: <span className="text-amber-500">🔶</span>,
};

function formatTimeAgo(dateStr: unknown): string {
  if (!dateStr || typeof dateStr !== 'string') return '--';
  const seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}

export default function RuntimesTable({ runtimes }: Props) {
  if (runtimes.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-16 px-4 bg-zinc-900/50 border border-zinc-800 rounded-xl text-zinc-500 space-y-3">
        <Server size={32} strokeWidth={1.5} />
        <p className="text-sm font-medium">No runtimes registered.</p>
        <p className="text-xs text-zinc-600">Start a daemon to register runtimes in this workspace.</p>
      </div>
    );
  }

  return (
    <div className="w-full overflow-hidden bg-zinc-900 border border-zinc-800 rounded-xl shadow-sm">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-left select-text">
          <thead>
            <tr className="border-b border-zinc-800 bg-zinc-900/50">
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Provider</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider text-center">Status</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Version</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Last Seen</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Workspace</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-800">
            {runtimes.map((runtime, idx) => (
              <tr key={idx} className="hover:bg-zinc-800/30 transition-colors group">
                <td className="px-4 py-3 text-sm font-medium whitespace-nowrap">
                  <div className="flex items-center gap-2">
                    <span className="flex-shrink-0 w-4 h-4 flex items-center justify-center grayscale group-hover:grayscale-0 transition-all">
                      {PROVIDER_ICONS[String(runtime.provider)] || <Cpu size={14} className="text-zinc-500" />}
                    </span>
                    <span className="capitalize text-zinc-200">{String(runtime.provider)}</span>
                  </div>
                </td>
                <td className="px-4 py-3 whitespace-nowrap text-center">
                  <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-tight ${
                    runtime.status === 'online' 
                      ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20' 
                      : 'bg-zinc-800 text-zinc-500 border border-zinc-700'
                  }`}>
                    {runtime.status === 'online' && <Activity size={10} className="animate-pulse" />}
                    {String(runtime.status || 'offline')}
                  </span>
                </td>
                <td className="px-4 py-3 text-xs text-zinc-500 font-mono whitespace-nowrap">
                  {String(runtime.version || 'v0.0.0')}
                </td>
                <td className="px-4 py-3 text-xs text-zinc-500 whitespace-nowrap">
                  <div className="flex items-center gap-1.5">
                    <Clock size={12} className="text-zinc-600" />
                    {formatTimeAgo(runtime.last_seen)}
                  </div>
                </td>
                <td className="px-4 py-3 text-xs text-zinc-500 truncate max-w-[120px]">
                  {String(runtime.workspace || '--')}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
