import { useState, useEffect, useRef } from 'react';
import { Terminal, ScrollText, Trash2, ChevronRight } from 'lucide-react';
import { api } from '../lib/api';

export default function DaemonLogs() {
  const [lines, setLines] = useState<string[]>([]);
  const [autoScroll, setAutoScroll] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let alive = false;
    const load = async () => {
      try {
        const status = await api.daemonStatus();
        if (alive && status) {
          setLines(status.log_lines || []);
        }
      } catch (e) {
        console.error('Failed to fetch logs', e);
      }
    };
    
    const t1 = setTimeout(() => { alive = true; load(); }, 1500);
    const t2 = setInterval(() => { if (alive) load(); }, 5000);
    
    return () => { 
      alive = false; 
      clearTimeout(t1); 
      clearInterval(t2); 
    };
  }, []);

  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [lines, autoScroll]);

  const getLineColor = (line: string) => {
    if (line.includes(' INF ')) return 'text-emerald-400';
    if (line.includes(' WRN ')) return 'text-amber-400';
    if (line.includes(' ERR ')) return 'text-rose-500 font-bold';
    if (line.includes(' DBG ')) return 'text-zinc-500 italic';
    return 'text-zinc-300';
  };

  return (
    <div className="flex flex-col gap-4 h-[calc(100vh-140px)]">
      {/* Header Controls */}
      <div className="flex items-center justify-between px-1">
        <div className="flex items-center gap-2">
          <Terminal size={18} className="text-zinc-400" />
          <h2 className="text-sm font-bold text-zinc-200 tracking-tight">System Daemon Logs</h2>
        </div>
        
        <div className="flex items-center gap-4">
          <label className="flex items-center gap-2 cursor-pointer group select-none">
            <div className={`w-8 h-4 rounded-full transition-colors relative ${autoScroll ? 'bg-blue-600' : 'bg-zinc-700'}`}>
              <div className={`absolute top-1 w-2 h-2 rounded-full bg-white transition-all ${autoScroll ? 'left-5' : 'left-1'}`} />
              <input 
                type="checkbox" 
                className="hidden" 
                checked={autoScroll} 
                onChange={(e) => setAutoScroll(e.target.checked)} 
              />
            </div>
            <span className="text-[11px] font-bold text-zinc-500 group-hover:text-zinc-300 uppercase tracking-tighter transition-colors">
              Auto-scroll
            </span>
          </label>
          
          <button 
            onClick={() => setLines([])}
            className="p-1.5 text-zinc-500 hover:text-rose-400 transition-colors"
            title="Clear Logs"
          >
            <Trash2 size={14} />
          </button>
        </div>
      </div>

      {/* Terminal View */}
      <div className="flex-1 min-h-0 bg-[#050505] border border-zinc-800 rounded-xl shadow-2xl flex flex-col overflow-hidden relative group">
        {/* Terminal Header Decoration */}
        <div className="h-8 bg-zinc-900/50 border-b border-zinc-800/50 flex items-center px-4 gap-1.5 flex-shrink-0">
          <div className="w-2.5 h-2.5 rounded-full bg-rose-500/20 border border-rose-500/30" />
          <div className="w-2.5 h-2.5 rounded-full bg-amber-500/20 border border-amber-500/30" />
          <div className="w-2.5 h-2.5 rounded-full bg-emerald-500/20 border border-emerald-500/30" />
          <div className="ml-4 flex items-center gap-1.5 text-[10px] font-mono text-zinc-500">
             <ScrollText size={10} />
             multica-daemon.log
          </div>
        </div>

        {/* Log Content */}
        <div 
          ref={scrollRef}
          className="flex-1 overflow-y-auto p-4 font-mono text-[12px] leading-relaxed selection:bg-blue-500/30 custom-scrollbar"
        >
          {lines.length === 0 ? (
            <div className="h-full flex flex-col items-center justify-center text-zinc-600 gap-3">
              <div className="animate-pulse flex items-center gap-2 text-zinc-700">
                <ChevronRight size={14} />
                <span className="w-2 h-4 bg-zinc-700" />
              </div>
              <p className="text-[11px] font-bold uppercase tracking-widest italic">Waiting for process output...</p>
            </div>
          ) : (
            lines.map((line, idx) => (
              <div key={idx} className="group/line flex gap-3 py-0.5 border-l-2 border-transparent hover:border-zinc-800 hover:bg-zinc-900/30 transition-colors">
                <span className="text-zinc-600 w-8 shrink-0 text-right select-none text-[10px] mt-0.5">{idx + 1}</span>
                <span className={`break-all whitespace-pre-wrap ${getLineColor(line)}`}>
                  {line}
                </span>
              </div>
            ))
          )}
        </div>
        
        {/* Status Indicator */}
        <div className="absolute bottom-4 right-6 pointer-events-none">
           <div className="flex items-center gap-2 bg-zinc-900/80 backdrop-blur border border-zinc-800 px-2 py-1 rounded text-[10px] font-bold text-zinc-500 shadow-lg">
             <div className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
             LIVE
           </div>
        </div>
      </div>
      
      {/* Footer Info */}
      <div className="flex items-center justify-between text-[10px] text-zinc-600 font-medium px-1">
        <span>Encoding: UTF-8</span>
        <span>Lines: {lines.length}</span>
      </div>
    </div>
  );
}
