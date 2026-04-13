import { useState } from 'react';
import Layout from './components/Layout';
import Setup from './components/Setup';
import { useConfig } from './hooks/useApi';
import { Loader2 } from 'lucide-react';

export default function App() {
  const { data: config, isLoading } = useConfig();
  const [showSetup, setShowSetup] = useState(false);

  if (isLoading) {
    return (
      <div className="h-screen w-screen flex items-center justify-center bg-zinc-950">
        <Loader2 className="w-8 h-8 text-blue-500 animate-spin" />
      </div>
    );
  }

  const needsSetup = !config?.token || showSetup;

  if (needsSetup) {
    return (
      <Setup 
        initialConfig={config} 
        onComplete={() => setShowSetup(false)} 
        allowCancel={!!config?.token}
      />
    );
  }

  return <Layout onOpenSetup={() => setShowSetup(true)} />;
}
