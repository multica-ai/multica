import type { ReactNode } from 'react';
import { Users, Bot, Zap, Monitor, Boxes, CheckCircle2, Loader2 } from 'lucide-react';

interface Props {
  agents: Array<Record<string, any>>;
}

const PROVIDER_ICONS: Record<string, ReactNode> = {
  claude: <span className="text-orange-500">🟠</span>,
  codex: <span className="text-green-500">🟢</span>,
  gemini: <span className="text-blue-500">🔵</span>,
  opencode: <span className="text-zinc-400">⚪</span>,
  openclaw: <span className="text-purple-500">🟣</span>,
  hermes: <span className="text-amber-500">🔶</span>,
};

function StatusBadge({ status }: { status: unknown }) {
  const s = String(status || 'unknown').toLowerCase();
  
  if (s === 'working') {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-tight bg-amber-500/10 text-amber-500 border border-amber-500/20">
        <Loader2 size={10} className="animate-spin" />
        Working
      </span>
    );
  }
  
  if (s === 'idle') {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-tight bg-emerald-500/10 text-emerald-500 border border-emerald-500/20">
        <CheckCircle2 size={10} />
        Idle
      </span>
    );
  }

  return (
    <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-tight bg-zinc-800 text-zinc-500 border border-zinc-700">
      {s}
    </span>
  );
}

export default function AgentsTable({ agents }: Props) {
  if (agents.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-16 px-4 bg-zinc-900/50 border border-zinc-800 rounded-xl text-zinc-500 space-y-3">
        <Users size={32} strokeWidth={1.5} />
        <p className="text-sm font-medium">No agents active.</p>
        <p className="text-xs text-zinc-600">Create or connect agents in the Multica web dashboard.</p>
      </div>
    );
  }

  return (
    <div className="w-full overflow-hidden bg-zinc-900 border border-zinc-800 rounded-xl shadow-sm">
      <div className="overflow-x-auto text-zinc-200">
        <table className="w-full border-collapse text-left select-text">
          <thead>
            <tr className="border-b border-zinc-800 bg-zinc-900/50">
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Name</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Provider</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider text-center">Status</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Runtime</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider text-center">Skills</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Workspace</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-800">
            {agents.map((agent, idx) => (
              <tr key={idx} className="hover:bg-zinc-800/30 transition-colors group">
                <td className="px-4 py-3 whitespace-nowrap">
                  <div className="flex items-center gap-2.5">
                    <div className="w-8 h-8 rounded-lg bg-zinc-800 flex items-center justify-center border border-zinc-700 text-zinc-400 group-hover:text-zinc-200 transition-colors">
                      <Bot size={16} />
                    </div>
                    <span className="text-sm font-semibold tracking-tight">{String(agent.name || 'Unknown Agent')}</span>
                  </div>
                </td>
                <td className="px-4 py-3 text-sm whitespace-nowrap">
                  <div className="flex items-center gap-2">
                    <span className="flex-shrink-0 grayscale group-hover:grayscale-0 transition-all">
                      {PROVIDER_ICONS[String(agent.provider)] || <Zap size={14} className="text-zinc-500" />}
                    </span>
                    <span className="capitalize">{String(agent.provider || '--')}</span>
                  </div>
                </td>
                <td className="px-4 py-3 whitespace-nowrap text-center">
                  <StatusBadge status={agent.status} />
                </td>
                <td className="px-4 py-3 text-xs text-zinc-500 whitespace-nowrap">
                  <div className="flex items-center gap-1.5 font-mono">
                    <Monitor size={12} className="text-zinc-600" />
                    {String(agent.runtime_name || '--')}
                  </div>
                </td>
                <td className="px-4 py-3 text-center whitespace-nowrap">
                  <div className="inline-flex items-center justify-center min-w-[32px] px-1.5 py-0.5 rounded bg-zinc-800 text-zinc-400 text-[10px] font-bold border border-zinc-700">
                    {Array.isArray(agent.skills) ? agent.skills.length : 0}
                  </div>
                </td>
                <td className="px-4 py-3 text-xs text-zinc-500 truncate max-w-[120px]">
                  <div className="flex items-center gap-1.5">
                    <Boxes size={12} className="text-zinc-600" />
                    {String(agent.workspace || '--')}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
