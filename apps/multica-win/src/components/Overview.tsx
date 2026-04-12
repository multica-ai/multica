import { useMemo } from 'react';
import { 
  Layers, 
  Activity, 
  AlertCircle, 
  Bell, 
  Globe,
  ArrowUpRight,
  Workflow,
  Sparkles,
  Clock,
  FolderOpen
} from 'lucide-react';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';
import { api } from '../lib/api';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

interface Props {
  online: boolean;
  user: Record<string, any> | null;
  workspaces: any[];
  runtimes: any[];
  agents: any[];
  issues: any[];
  inbox: any[];
}

const PROVIDER_ICONS: Record<string, string> = {
  claude: '🟠', codex: '🟢', gemini: '🔵', opencode: '⚪', openclaw: '🟣', hermes: '🔶',
};

const ISSUE_STATUS_MAP: Record<string, { label: string, color: string }> = {
  in_progress: { label: 'Working', color: 'bg-amber-500/10 text-amber-500 border-amber-500/20' },
  todo: { label: 'Pending', color: 'bg-blue-500/10 text-blue-500 border-blue-500/20' },
  in_review: { label: 'Reviewing', color: 'bg-purple-500/10 text-purple-500 border-purple-500/20' },
  done: { label: 'Completed', color: 'bg-emerald-500/10 text-emerald-500 border-emerald-500/20' },
  blocked: { label: 'Blocked', color: 'bg-rose-500/10 text-rose-500 border-rose-500/20' },
};

export default function Overview({ online, user, workspaces = [], runtimes = [], agents = [], issues = [], inbox = [] }: Props) {
  const activeIssuesCount = useMemo(() => 
    issues.filter((i) => ['in_progress', 'in_review', 'todo'].includes(i.status as string)).length
  , [issues]);
  
  const workingAgentsCount = useMemo(() => 
    agents.filter((a) => a.status === 'working').length
  , [agents]);
  
  const providers = useMemo(() => 
    [...new Set(runtimes.map((r) => String(r.provider)))]
  , [runtimes]);

  const stats = [
    { label: 'Workspaces', value: workspaces.length, icon: Layers, color: 'text-blue-500', bg: 'bg-blue-500/10' },
    { label: 'Runtimes', value: runtimes.length, icon: Globe, color: 'text-emerald-500', bg: 'bg-emerald-500/10' },
    { label: 'Agents', value: agents.length, sub: workingAgentsCount > 0 ? `${workingAgentsCount} Active` : null, icon: Workflow, color: 'text-amber-500', bg: 'bg-amber-500/10' },
    { label: 'Issues', value: issues.length, sub: activeIssuesCount > 0 ? `${activeIssuesCount} Priority` : null, icon: AlertCircle, color: 'text-rose-500', bg: 'bg-rose-500/10' },
    { label: 'Inbox', value: inbox.length, icon: Bell, color: 'text-purple-500', bg: 'bg-purple-500/10' },
  ];

  return (
    <div className="space-y-8 animate-in fade-in duration-500">
      {/* Header Section */}
      <div className="flex flex-col md:flex-row md:items-end justify-between gap-4">
        <div className="min-w-0">
           <div className="flex items-center gap-2 mb-2">
             <span className="bg-blue-500/10 text-blue-500 text-xs font-black uppercase tracking-widest px-2 py-1 rounded border border-blue-500/20 shrink-0">Control Center</span>
             <span className="text-zinc-600 text-xs font-bold uppercase tracking-tighter truncate">Production v2.4</span>
           </div>
           <h2 className="text-2xl sm:text-3xl font-black tracking-tighter text-white flex items-center gap-3 truncate">
             Hi, {String(user?.name || 'Commander')} <Sparkles className="text-amber-400 w-6 h-6 shrink-0" />
           </h2>
        </div>
        
        <div className="bg-zinc-900/50 border border-zinc-800/50 rounded-xl px-4 py-3 flex items-center gap-3 shrink-0">
          <div className={cn("w-2 h-2 rounded-full shadow-[0_0_8px] shrink-0", online ? "bg-emerald-500 shadow-emerald-500/50" : "bg-rose-500 shadow-rose-500/50")} />
          <div className="flex flex-col">
            <span className="text-xs font-black text-zinc-600 uppercase tracking-tighter">System Link</span>
            <span className="text-sm font-bold text-zinc-300 tracking-tight">{online ? 'Cloud Synchronized' : 'Offline Mode'}</span>
          </div>
        </div>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-5 gap-4">
        {stats.map((s) => {
          const Icon = s.icon;
          return (
            <div key={s.label} className="bg-zinc-900/40 border border-zinc-800/60 p-4 sm:p-5 rounded-2xl hover:border-zinc-700 transition-all group relative overflow-hidden min-w-0">
               <div className={cn("absolute top-0 right-0 w-16 h-16 -mr-6 -mt-6 rounded-full blur-2xl opacity-10", s.bg)} />
               <div className="relative z-10 flex flex-col h-full">
                  <div className="flex items-center justify-between mb-4">
                    <div className={cn("p-2.5 rounded-lg border border-zinc-800/50 bg-zinc-950 shrink-0", s.color)}>
                      <Icon className="w-5 h-5" />
                    </div>
                    <ArrowUpRight className="w-4 h-4 text-zinc-700 group-hover:text-zinc-500 transition-colors shrink-0" />
                  </div>
                  <p className="text-xs font-bold text-zinc-500 uppercase tracking-tight mb-1 truncate">{s.label}</p>
                  <div className="flex items-baseline gap-2 mt-auto min-w-0">
                    <span className="text-3xl font-black text-zinc-100 tracking-tighter truncate">{s.value}</span>
                    {s.sub && <span className="text-[10px] font-bold text-amber-500/80 bg-amber-500/5 px-1.5 py-0.5 rounded border border-amber-500/10 whitespace-nowrap shrink-0 hidden sm:inline-block">{s.sub}</span>}
                  </div>
               </div>
            </div>
          );
        })}
      </div>

      {/* Main Content Sections */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
        {/* Workspaces Section */}
        <section className="lg:col-span-4 bg-zinc-900/40 border border-zinc-800/60 rounded-3xl flex flex-col overflow-hidden min-w-0">
          <div className="px-5 py-4 border-b border-zinc-800/40 flex items-center justify-between bg-zinc-900/20 min-h-[3.5rem]">
            <h3 className="text-xs font-black text-zinc-400 uppercase tracking-widest flex items-center gap-2 truncate">
              <Layers className="w-4 h-4 shrink-0" /> Workspaces
            </h3>
            <span className="text-xs font-black text-zinc-600 bg-zinc-950 px-2 py-1 rounded border border-zinc-800/50 shrink-0">{workspaces.length}</span>
          </div>
          
          <div className="p-3 flex-1 min-h-[24rem] overflow-y-auto no-scrollbar">
            {workspaces.length > 0 ? (
              <div className="space-y-1">
                {workspaces.map((ws, i) => (
                  <div key={ws.id || i} className="px-4 py-3 flex items-center justify-between hover:bg-zinc-800/40 rounded-xl transition-all group min-w-0">
                    <div className="flex items-center gap-3 min-w-0">
                       <div className="w-2 h-2 rounded-full bg-zinc-800 group-hover:bg-blue-500 transition-colors shrink-0" />
                       <span className="text-sm font-bold text-zinc-400 group-hover:text-zinc-200 truncate">{String(ws.name || 'Untitled')}</span>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      <button 
                        onClick={() => api.openFolder(String(ws.path || `~/multica_workspaces/${ws.id}`))}
                        title="Open Workspace Folder"
                        className="p-1.5 rounded-md hover:bg-zinc-700 text-zinc-600 hover:text-white transition-colors"
                      >
                        <FolderOpen className="w-3.5 h-3.5" />
                      </button>
                      <span className="text-xs font-mono text-zinc-700 group-hover:text-zinc-500 tracking-tighter">{String(ws.id || '').slice(0, 8)}</span>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="flex flex-col items-center justify-center py-20 opacity-20">
                <Layers className="w-8 h-8" />
              </div>
            )}
          </div>
        </section>

        {/* Activity Section */}
        <section className="lg:col-span-8 bg-zinc-900/40 border border-zinc-800/60 rounded-3xl flex flex-col overflow-hidden min-w-0">
          <div className="px-5 py-4 border-b border-zinc-800/40 flex items-center justify-between bg-zinc-900/20 min-h-[3.5rem]">
            <h3 className="text-xs font-black text-zinc-400 uppercase tracking-widest flex items-center gap-2 truncate">
              <Activity className="w-4 h-4 shrink-0" /> Live Activity
            </h3>
            <div className="flex items-center gap-1.5 shrink-0">
               {providers.map(p => (
                  <span key={p} className="text-xs bg-zinc-950 px-1.5 py-0.5 rounded border border-zinc-800/60" title={p}>
                    {PROVIDER_ICONS[p] || '⚫'}
                  </span>
               ))}
            </div>
          </div>

          <div className="p-3 flex-1 min-h-[24rem] overflow-y-auto no-scrollbar">
            {issues.length > 0 ? (
              <div className="space-y-1.5">
                {[...issues]
                  .sort((a, b) => new Date(b.created_at as string).getTime() - new Date(a.created_at as string).getTime())
                  .slice(0, 10)
                  .map((issue, i) => {
                    const status = ISSUE_STATUS_MAP[String(issue.status)] || { label: 'Unknown', color: 'bg-zinc-800 text-zinc-500' };
                    return (
                      <div key={issue.id || i} className="p-3 sm:p-4 flex items-center gap-4 hover:bg-zinc-800/30 rounded-2xl transition-all border border-transparent hover:border-zinc-800/50 group min-w-0">
                        <div className="w-10 h-10 rounded-xl bg-zinc-950 border border-zinc-800/60 flex items-center justify-center text-zinc-700 group-hover:text-zinc-500 shrink-0">
                           <Clock className="w-5 h-5" />
                        </div>
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-2 mb-1">
                            <span className="text-[10px] font-black text-zinc-600 uppercase tracking-tighter truncate max-w-[8rem]">{String(issue.workspace || 'SYSTEM')}</span>
                          </div>
                          <p className="text-sm font-bold text-zinc-300 truncate group-hover:text-white transition-colors tracking-tight">{String(issue.title || 'Task in progress...')}</p>
                        </div>
                        <div className={cn("text-[10px] font-black uppercase tracking-widest px-3 py-1 rounded border whitespace-nowrap shrink-0", status.color)}>
                          {status.label}
                        </div>
                      </div>
                    );
                  })}
              </div>
            ) : (
              <div className="flex flex-col items-center justify-center py-20 opacity-20">
                <Activity className="w-8 h-8" />
              </div>
            )}
          </div>
          
          <div className="p-3 border-t border-zinc-800/30 bg-zinc-950/10 text-center shrink-0">
             <button className="text-xs font-black text-zinc-600 hover:text-zinc-400 transition-colors uppercase tracking-widest">Full History</button>
          </div>
        </section>
      </div>
    </div>
  );
}
