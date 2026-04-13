import { useState, useMemo } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { 
  LayoutDashboard, 
  Cpu, 
  Users, 
  ListTodo, 
  TrendingUp, 
  Bell, 
  Terminal,
  RefreshCcw,
  X,
  Minus,
  Settings,
  FolderOpen,
  ChevronRight,
  ShieldCheck,
  Power
} from 'lucide-react';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

import { 
  useHealth, 
  useUser, 
  useWorkspaces, 
  useWorkspaceData, 
  useDaemon 
} from '../hooks/useApi';
import { api } from '../lib/api';

import Overview from './Overview';
import RuntimesTable from './RuntimesTable';
import AgentsTable from './AgentsTable';
import IssuesTable from './IssuesTable';
import TokenUsage from './TokenUsage';
import InboxPanel from './InboxPanel';
import DaemonLogs from './DaemonLogs';
import logo from '../assets/logo.svg';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

type Tab = 'overview' | 'runtimes' | 'agents' | 'issues' | 'usage' | 'inbox' | 'logs';

const menuItems: Array<{ id: Tab; label: string; icon: any }> = [
  { id: 'overview', label: 'Overview', icon: LayoutDashboard },
  { id: 'runtimes', label: 'Runtimes', icon: Cpu },
  { id: 'agents', label: 'Agents', icon: Users },
  { id: 'issues', label: 'Issues', icon: ListTodo },
  { id: 'usage', label: 'Usage', icon: TrendingUp },
  { id: 'inbox', label: 'Inbox', icon: Bell },
  { id: 'logs', label: 'Logs', icon: Terminal },
];

export default function Layout({ onOpenSetup }: { onOpenSetup: () => void }) {
  const [tab, setTab] = useState<Tab>('overview');
  const [isRefreshing, setIsRefreshing] = useState(false);
  const queryClient = useQueryClient();
  const appWindow = getCurrentWindow();

  const { data: online } = useHealth();
  const { data: user } = useUser();
  const { data: workspaces = [] } = useWorkspaces();
  const { data: workspaceData = { runtimes: [], agents: [], issues: [], inbox: [] } } = useWorkspaceData(workspaces);
  const { data: daemon, refetch: refetchDaemon } = useDaemon();

  const { runtimes, agents, issues, inbox } = workspaceData;

  const activeIssuesCount = useMemo(() => 
    (issues || []).filter((i: any) => ['in_progress', 'in_review', 'todo'].includes(i.status as string)).length
  , [issues]);

  const handleRefresh = async () => {
    setIsRefreshing(true);
    await queryClient.refetchQueries();
    setTimeout(() => setIsRefreshing(false), 1000);
  };

  const handleDaemon = async (action: 'start' | 'stop') => {
    try {
      if (action === 'start') await api.startDaemon();
      else await api.stopDaemon();
      await refetchDaemon();
    } catch (e) {
      console.error(e);
    }
  };

  return (
    <div className="h-screen w-screen flex overflow-hidden bg-zinc-950 font-sans antialiased text-zinc-200">
      {/* Sidebar - Scalable width */}
      <aside className="w-64 min-w-[16rem] border-r border-zinc-800/60 bg-zinc-950 flex flex-col shrink-0 grow-0 select-none z-20 shadow-2xl overflow-hidden">
        <div className="min-h-14 py-2 flex items-center px-5 shrink-0" data-tauri-drag-region>
          <div className="flex items-center gap-3 pointer-events-none">
            <div className="w-7 h-7 shrink-0 bg-white rounded flex items-center justify-center p-0.5 shadow-lg shadow-white/5">
              <img src={logo} alt="Multica" className="w-full h-full" />
            </div>
            <span className="text-base font-black tracking-tighter text-white uppercase truncate">Multica</span>
          </div>
        </div>

        <nav className="flex-1 px-3 py-4 space-y-1 overflow-y-auto no-scrollbar">
          {menuItems.map((item) => {
            const Icon = item.icon;
            const isActive = tab === item.id;
            return (
              <button
                key={item.id}
                onClick={() => setTab(item.id)}
                className={cn(
                  "w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-bold transition-all duration-200 group relative",
                  isActive 
                    ? "bg-zinc-900 text-white border border-zinc-800" 
                    : "text-zinc-500 hover:text-zinc-300 hover:bg-zinc-900/40"
                )}
              >
                <Icon className={cn("w-5 h-5 shrink-0 transition-colors", isActive ? "text-blue-500" : "text-zinc-600 group-hover:text-zinc-400")} />
                <span className="truncate">{item.label}</span>
                
                <div className="ml-auto flex items-center gap-2 shrink-0">
                  {item.id === 'inbox' && (inbox?.length || 0) > 0 && (
                    <span className="bg-blue-600 text-white text-xs px-2 py-0.5 rounded font-black tracking-tighter">
                      {inbox.length}
                    </span>
                  )}
                  {item.id === 'issues' && activeIssuesCount > 0 && (
                     <span className="bg-amber-600/10 text-amber-500 text-xs px-2 py-0.5 rounded font-black border border-amber-500/20">
                      {activeIssuesCount}
                    </span>
                  )}
                </div>
              </button>
            );
          })}
        </nav>

        {/* Sidebar Footer */}
        <div className="p-3 border-t border-zinc-900 bg-zinc-950/80 shrink-0">
          <div className="bg-zinc-900/60 rounded-xl border border-zinc-800/50 p-3 space-y-3">
            <div className="flex items-center gap-3">
              <div className="flex-1 min-w-0">
                <p className="text-sm font-black text-white truncate">{String(user?.name || 'Commander')}</p>
                <div className="flex items-center gap-1.5 mt-1">
                  <div className={cn("w-2 h-2 shrink-0 rounded-full", online ? "bg-emerald-500 shadow-[0_0_5px_rgba(16,185,129,0.5)]" : "bg-zinc-600")} />
                  <span className="text-xs font-bold text-zinc-500 uppercase tracking-widest truncate">{online ? 'Cloud Link' : 'Offline'}</span>
                </div>
              </div>
              
              <button 
                data-tauri-drag-region="false"
                onClick={onOpenSetup}
                title="Connection Settings"
                className="w-6 h-6 shrink-0 flex items-center justify-center bg-zinc-800 hover:bg-zinc-700 rounded-md text-zinc-400 hover:text-white transition-colors"
              >
                <Settings className="w-3.5 h-3.5" />
              </button>
              
              <button 
                data-tauri-drag-region="false"
                onClick={() => api.openFolder("~/.multica")} 
                title="Open Config Folder"
                className="w-6 h-6 shrink-0 flex items-center justify-center bg-zinc-800 hover:bg-zinc-700 rounded-md text-zinc-400 hover:text-white transition-colors"
              >
                <FolderOpen className="w-3.5 h-3.5" />
              </button>
            </div>

            <div className="pt-3 border-t border-zinc-800/40">
               <div className="flex items-center justify-between mb-3 px-1">
                 <div className="flex items-center gap-2 shrink-0">
                    <ShieldCheck className={cn("w-4 h-4", daemon?.running ? "text-blue-500" : "text-zinc-600")} />
                    <span className="text-xs font-black text-zinc-500 uppercase tracking-tighter truncate">Daemon</span>
                 </div>
                 <span className={cn(
                   "text-xs px-2 py-1 shrink-0 rounded font-black uppercase tracking-widest",
                   daemon?.running ? "bg-blue-500/10 text-blue-500" : "bg-zinc-800 text-zinc-600"
                 )}>
                   {daemon?.running ? 'Running' : 'Stopped'}
                 </span>
               </div>
               
               <button 
                data-tauri-drag-region="false"
                onClick={() => handleDaemon(daemon?.running ? 'stop' : 'start')}
                className={cn(
                  "w-full py-2.5 rounded-lg text-xs font-black transition-all active:scale-95 shadow-lg flex items-center justify-center gap-2",
                  daemon?.running 
                    ? "bg-zinc-800 text-zinc-400 border border-zinc-700 hover:bg-zinc-700 hover:text-white" 
                    : "bg-blue-600 text-white hover:bg-blue-500 shadow-blue-900/20 border border-blue-400/20"
                )}
               >
                 <Power className="w-4 h-4 shrink-0" />
                 <span className="truncate">{daemon?.running ? 'Stop Daemon' : 'Start Daemon'}</span>
               </button>
            </div>
          </div>
        </div>
      </aside>

      {/* Main Container */}
      <div className="flex-1 flex flex-col min-w-0 bg-zinc-950 relative overflow-hidden">
        <header data-tauri-drag-region className="min-h-14 py-2 flex items-center justify-between px-6 shrink-0 bg-zinc-950/90 z-10 border-b border-zinc-900/50">
          <div className="flex items-center gap-4 min-w-0">
             <div className="flex items-center gap-2 text-zinc-600 shrink-0 pointer-events-none">
                <span className="text-xs font-bold tracking-tight">Main</span>
                <ChevronRight className="w-4 h-4" />
                <h1 className="text-sm font-black text-white uppercase tracking-tighter truncate">
                  {menuItems.find(m => m.id === tab)?.label}
                </h1>
             </div>
             
             <div className="h-4 w-px bg-zinc-800/50 mx-2 shrink-0" />

             <button 
                onClick={handleRefresh}
                disabled={isRefreshing}
                className={cn(
                  "flex items-center gap-2 px-4 py-2 shrink-0 rounded-full border border-zinc-800/80 hover:bg-zinc-900 text-xs font-black text-zinc-400 transition-all group",
                  isRefreshing && "text-blue-500 bg-blue-500/5 border-blue-500/30"
                )}
             >
               <RefreshCcw className={cn("w-4 h-4 shrink-0", isRefreshing && "animate-spin")} />
               <span className="hidden sm:inline">{isRefreshing ? 'Syncing...' : 'Sync System'}</span>
             </button>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <button 
              onClick={async () => await appWindow.minimize()}
              className="w-10 h-10 flex items-center justify-center rounded-lg hover:bg-zinc-900 text-zinc-600 transition-all cursor-pointer"
            >
              <Minus className="w-5 h-5 pointer-events-none" />
            </button>
            <button 
              onClick={async () => await appWindow.close()}
              className="w-10 h-10 flex items-center justify-center rounded-lg hover:bg-rose-500/10 hover:text-rose-500 text-zinc-600 transition-all cursor-pointer"
            >
              <X className="w-5 h-5 pointer-events-none" />
            </button>
          </div>
        </header>

        <main className="flex-1 overflow-y-auto no-scrollbar scroll-smooth bg-zinc-950">
          <div className="w-full max-w-7xl mx-auto px-6 py-8 sm:px-10 sm:py-10">
            {tab === 'overview' && (
              <Overview 
                online={!!online} 
                user={user || null} 
                workspaces={workspaces || []}
                runtimes={runtimes || []} 
                agents={agents || []} 
                issues={issues || []} 
                inbox={inbox || []} 
              />
            )}
            {tab === 'runtimes' && <RuntimesTable runtimes={runtimes || []} />}
            {tab === 'agents' && <AgentsTable agents={agents || []} />}
            {tab === 'issues' && <IssuesTable issues={issues || []} />}
            {tab === 'usage' && <TokenUsage workspaces={workspaces || []} />}
            {tab === 'inbox' && <InboxPanel items={inbox || []} />}
            {tab === 'logs' && <DaemonLogs />}
          </div>
        </main>
      </div>
    </div>
  );
}
