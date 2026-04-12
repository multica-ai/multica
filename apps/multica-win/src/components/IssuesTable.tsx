import { AlertCircle, Clock, User as UserIcon, Tag } from 'lucide-react';

interface Props {
  issues: Array<Record<string, any>>;
}

function formatTimeAgo(dateStr: unknown): string {
  if (!dateStr || typeof dateStr !== 'string') return '--';
  const seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}

const STATUS_CONFIG: Record<string, { color: string; label: string }> = {
  backlog: { color: 'text-zinc-500', label: 'Backlog' },
  todo: { color: 'text-blue-400', label: 'Todo' },
  in_progress: { color: 'text-amber-400', label: 'In Progress' },
  in_review: { color: 'text-purple-400', label: 'In Review' },
  done: { color: 'text-emerald-400', label: 'Done' },
  blocked: { color: 'text-rose-500', label: 'Blocked' },
  cancelled: { color: 'text-zinc-600', label: 'Cancelled' },
};

const PRIORITY_CONFIG: Record<string, { color: string; bg: string }> = {
  urgent: { color: 'text-rose-400', bg: 'bg-rose-500/10 border-rose-500/20' },
  high: { color: 'text-amber-400', bg: 'bg-amber-500/10 border-amber-500/20' },
  medium: { color: 'text-blue-400', bg: 'bg-blue-500/10 border-blue-500/20' },
  low: { color: 'text-zinc-500', bg: 'bg-zinc-800 border-zinc-700' },
};

export default function IssuesTable({ issues }: Props) {
  if (issues.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-16 px-4 bg-zinc-900/50 border border-zinc-800 rounded-xl text-zinc-500 space-y-3">
        <AlertCircle size={32} strokeWidth={1.5} />
        <p className="text-sm font-medium">No issues found.</p>
        <p className="text-xs text-zinc-600">Track and manage tasks assigned to your AI agents here.</p>
      </div>
    );
  }

  const sorted = [...issues].sort((a, b) => 
    new Date(b.created_at as string).getTime() - new Date(a.created_at as string).getTime()
  );

  return (
    <div className="w-full overflow-hidden bg-zinc-900 border border-zinc-800 rounded-xl shadow-sm">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-left select-text">
          <thead>
            <tr className="border-b border-zinc-800 bg-zinc-900/50">
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">ID</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Title</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Status</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Priority</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Assignee</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Created</th>
              <th className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-wider">Workspace</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-800">
            {sorted.slice(0, 30).map((issue, idx) => {
              const status = STATUS_CONFIG[String(issue.status)] || STATUS_CONFIG.backlog;
              const priority = PRIORITY_CONFIG[String(issue.priority)] || PRIORITY_CONFIG.low;

              return (
                <tr key={idx} className="hover:bg-zinc-800/30 transition-colors group">
                  <td className="px-4 py-3 text-[11px] font-mono text-zinc-600 group-hover:text-zinc-400 whitespace-nowrap">
                    {String(issue.identifier || '')}
                  </td>
                  <td className="px-4 py-3 text-sm font-medium text-zinc-200 min-w-[200px] max-w-[320px]">
                    <div className="truncate" title={String(issue.title)}>
                      {String(issue.title || 'Untitled')}
                    </div>
                  </td>
                  <td className="px-4 py-3 whitespace-nowrap">
                    <div className={`flex items-center gap-1.5 text-[11px] font-bold uppercase tracking-tight ${status.color}`}>
                      <div className={`w-1.5 h-1.5 rounded-full bg-current`} />
                      {status.label}
                    </div>
                  </td>
                  <td className="px-4 py-3 whitespace-nowrap text-center">
                    <span className={`inline-flex px-2 py-0.5 rounded text-[10px] font-bold uppercase border ${priority.bg} ${priority.color}`}>
                      {String(issue.priority || 'none')}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-xs text-zinc-400 whitespace-nowrap">
                    <div className="flex items-center gap-1.5">
                      <UserIcon size={12} className="text-zinc-600" />
                      {String(issue.assignee_type === 'agent' ? 'Agent' : issue.assignee_type || '--')}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-xs text-zinc-500 whitespace-nowrap">
                    <div className="flex items-center gap-1.5">
                      <Clock size={12} className="text-zinc-600" />
                      {formatTimeAgo(issue.created_at)}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-xs text-zinc-500 truncate max-w-[120px]">
                    <div className="flex items-center gap-1.5">
                      <Tag size={12} className="text-zinc-600" />
                      {String(issue.workspace || '--')}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
