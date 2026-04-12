import { useState } from 'react';
import { Server, KeyRound, ChevronRight, CheckCircle2, ShieldCheck, X, Minus } from 'lucide-react';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';
import { api, type AppConfig } from '../lib/api';
import { useQueryClient } from '@tanstack/react-query';
import { getCurrentWindow } from '@tauri-apps/api/window';
import logo from '../assets/logo.svg';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

interface Props {
  initialConfig?: AppConfig | null;
  onComplete: () => void;
  allowCancel?: boolean;
}

export default function Setup({ initialConfig, onComplete, allowCancel }: Props) {
  const queryClient = useQueryClient();
  const appWindow = getCurrentWindow();
  
  const [serverUrl, setServerUrl] = useState(initialConfig?.server_url || 'http://localhost:8080');
  const [token, setToken] = useState(initialConfig?.token || '');
  const [isSaving, setIsSaving] = useState(false);
  const [status, setStatus] = useState<'idle' | 'testing' | 'success' | 'error'>('idle');
  const [errorMsg, setErrorMsg] = useState('');

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSaving(true);
    setStatus('testing');
    setErrorMsg('');

    try {
      const newConfig: AppConfig = {
        ...initialConfig,
        server_url: serverUrl.trim().replace(/\/+$/, ''),
        token: token.trim(),
      };

      // Save it first
      await api.saveConfig(newConfig);

      // Test connection by fetching user
      const user = await api.getCurrentUser();
      
      if (user && !('error' in user)) {
        setStatus('success');
        setTimeout(async () => {
          await queryClient.refetchQueries();
          onComplete();
        }, 800);
      } else {
        setStatus('error');
        setErrorMsg('Connection failed or Invalid Token. Please verify your settings.');
      }
    } catch (err: any) {
      setStatus('error');
      setErrorMsg(err?.message || 'An unexpected error occurred while saving.');
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <div className="h-screen w-screen flex flex-col bg-zinc-950 font-sans antialiased text-zinc-200 overflow-hidden select-none">
      {/* Custom Title Bar */}
      <header className="h-14 flex items-center justify-between px-6 drag-region shrink-0 bg-zinc-950 z-10 border-b border-zinc-900/50">
        <div className="flex items-center gap-3 no-drag">
          <div className="w-6 h-6 bg-white rounded flex items-center justify-center p-0.5 shadow-lg shadow-white/5">
            <img src={logo} alt="Multica" className="w-full h-full" />
          </div>
          <span className="text-xs font-black tracking-tighter text-white uppercase">Multica Node Setup</span>
        </div>
        
        <div className="flex items-center gap-2 no-drag">
          <button 
            onClick={() => appWindow.minimize()}
            className="w-8 h-8 flex items-center justify-center rounded-lg hover:bg-zinc-900 text-zinc-600 transition-all"
          >
            <Minus className="w-4 h-4" />
          </button>
          {allowCancel ? (
            <button 
              onClick={onComplete}
              className="w-8 h-8 flex items-center justify-center rounded-lg hover:bg-zinc-900 text-zinc-600 transition-all"
            >
              <X className="w-4 h-4" />
            </button>
          ) : (
            <button 
              onClick={() => appWindow.close()}
              className="w-8 h-8 flex items-center justify-center rounded-lg hover:bg-rose-500/10 hover:text-rose-500 text-zinc-600 transition-all"
            >
              <X className="w-4 h-4" />
            </button>
          )}
        </div>
      </header>

      {/* Main Content */}
      <div className="flex-1 flex flex-col items-center justify-center p-6 bg-zinc-950/50 relative">
        <div className="absolute inset-0 flex items-center justify-center pointer-events-none overflow-hidden">
           <div className="w-[600px] h-[600px] bg-blue-500/5 rounded-full blur-3xl" />
        </div>

        <div className="w-full max-w-md bg-zinc-900/60 border border-zinc-800/80 rounded-3xl p-8 backdrop-blur-xl relative z-10 shadow-2xl">
          <div className="text-center mb-8">
            <div className="w-16 h-16 bg-zinc-950 border border-zinc-800 rounded-2xl flex items-center justify-center mx-auto mb-4 shadow-inner">
               <ShieldCheck className="w-8 h-8 text-blue-500" />
            </div>
            <h2 className="text-xl font-black text-white tracking-tighter">Connection Settings</h2>
            <p className="text-xs font-bold text-zinc-500 mt-2 tracking-tight">Configure your Multica node to connect with the cloud mesh or your private server.</p>
          </div>

          <form onSubmit={handleSave} className="space-y-5">
            <div className="space-y-1.5 text-left no-drag">
              <label className="text-[10px] font-black uppercase tracking-widest text-zinc-500 flex items-center gap-1.5 ml-1">
                <Server className="w-3 h-3" /> Server URL
              </label>
              <input 
                type="text" 
                value={serverUrl}
                onChange={(e) => setServerUrl(e.target.value)}
                placeholder="http://localhost:8080"
                className="w-full bg-zinc-950 border border-zinc-800 rounded-xl px-4 py-3 text-sm text-white placeholder:text-zinc-700 focus:outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500/50 transition-all"
                required
              />
            </div>

            <div className="space-y-1.5 text-left no-drag">
              <label className="text-[10px] font-black uppercase tracking-widest text-zinc-500 flex items-center gap-1.5 ml-1">
                <KeyRound className="w-3 h-3" /> Personal Access Token (PAT)
              </label>
              <input 
                type="password" 
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="mul_xxxxxxxxxxxxxxxx"
                className="w-full bg-zinc-950 border border-zinc-800 rounded-xl px-4 py-3 text-sm text-white placeholder:text-zinc-700 focus:outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500/50 transition-all font-mono tracking-wider"
                required
              />
              <p className="text-[9px] font-bold text-zinc-600 mt-1.5 ml-1 tracking-tight">You can generate a token in your web dashboard settings.</p>
            </div>

            {status === 'error' && (
              <div className="p-3 bg-rose-500/10 border border-rose-500/20 rounded-xl text-center">
                <p className="text-xs font-bold text-rose-500 tracking-tight">{errorMsg}</p>
              </div>
            )}

            {status === 'success' && (
              <div className="p-3 bg-emerald-500/10 border border-emerald-500/20 rounded-xl text-center flex items-center justify-center gap-2">
                <CheckCircle2 className="w-4 h-4 text-emerald-500" />
                <p className="text-xs font-bold text-emerald-500 tracking-tight">Connection established!</p>
              </div>
            )}

            <div className="pt-4 flex items-center gap-3 no-drag">
              {allowCancel && (
                <button 
                  type="button"
                  onClick={onComplete}
                  className="flex-1 py-3 px-4 rounded-xl text-xs font-black text-zinc-400 hover:text-white bg-zinc-800 hover:bg-zinc-700 transition-colors"
                >
                  Cancel
                </button>
              )}
              <button 
                type="submit"
                disabled={isSaving || status === 'success'}
                className={cn(
                  "flex-1 flex items-center justify-center gap-2 py-3 px-4 rounded-xl text-xs font-black transition-all active:scale-95 shadow-lg",
                  isSaving || status === 'success' 
                    ? "bg-zinc-800 text-zinc-500 border border-zinc-700 cursor-not-allowed" 
                    : "bg-blue-600 text-white hover:bg-blue-500 shadow-blue-900/20 border border-blue-400/20"
                )}
              >
                {isSaving ? 'Testing Connection...' : (
                  <>
                    Connect to Mesh <ChevronRight className="w-4 h-4" />
                  </>
                )}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  );
}
