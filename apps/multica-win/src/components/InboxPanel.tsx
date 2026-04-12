import { Inbox, Bell, Info, CheckCircle2, AlertCircle, MessageSquare, Tag } from 'lucide-react';

interface Props {
  items: Array<Record<string, any>>;
}

function formatTimeAgo(dateStr: unknown): string {
  if (!dateStr || typeof dateStr !== 'string') return '--';
  const seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}

const TYPE_CONFIG: Record<string, { color: string; bg: string; icon: any }> = {
  issue_assigned: { color: 'text-blue-400', bg: 'bg-blue-400', icon: Bell },
  issue_updated: { color: 'text-emerald-400', bg: 'bg-emerald-400', icon: Info },
  task_completed: { color: 'text-emerald-400', bg: 'bg-emerald-400', icon: CheckCircle2 },
  task_failed: { color: 'text-rose-500', bg: 'bg-rose-500', icon: AlertCircle },
  mention: { color: 'text-purple-400', bg: 'bg-purple-400', icon: MessageSquare },
};

export default function InboxPanel({ items }: Props) {
  if (items.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-24 bg-zinc-900/50 border border-zinc-800 rounded-2xl text-zinc-500 space-y-4">
        <div className="w-16 h-16 rounded-full bg-zinc-800 flex items-center justify-center border border-zinc-700">
          <Inbox size={32} strokeWidth={1.5} className="text-zinc-600" />
        </div>
        <div className="text-center">
          <p className="text-sm font-semibold text-zinc-300">Your inbox is empty</p>
          <p className="text-xs text-zinc-500 mt-1">When agents notify you, they'll appear here.</p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {items.map((item, idx) => {
        const config = TYPE_CONFIG[String(item.type)] || { color: 'text-zinc-400', bg: 'bg-zinc-400', icon: Bell };
        const Icon = config.icon;

        return (
          <div 
            key={idx} 
            className="group relative overflow-hidden bg-zinc-900 border border-zinc-800 rounded-xl p-4 hover:border-zinc-700 transition-all shadow-sm active:scale-[0.99]"
          >
            {/* Type Indicator Bar */}
            <div className={`absolute left-0 top-0 bottom-0 w-1 ${config.bg} opacity-80 group-hover:opacity-100 transition-opacity`} />
            
            <div className="flex gap-4">
              <div className={`mt-0.5 flex-shrink-0 w-8 h-8 rounded-lg bg-zinc-800 border border-zinc-700 flex items-center justify-center ${config.color}`}>
                <Icon size={16} />
              </div>
              
              <div className="flex-1 min-w-0">
                <div className="flex justify-between items-start gap-4">
                  <h4 className="text-[13px] font-semibold text-zinc-200 leading-tight">
                    {String(item.title ?? item.message ?? 'Notification')}
                  </h4>
                  <span className="text-[10px] font-medium text-zinc-500 whitespace-nowrap bg-zinc-800 px-1.5 py-0.5 rounded border border-zinc-700">
                    {formatTimeAgo(String(item.created_at ?? item.timestamp ?? ''))}
                  </span>
                </div>
                
                {item.workspace != null && (
                  <div className="flex items-center gap-1.5 mt-2 text-[11px] text-zinc-500 font-medium">
                    <Tag size={10} className="text-zinc-600" />
                    <span className="truncate">{String(item.workspace)}</span>
                  </div>
                )}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}
