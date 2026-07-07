import { WifiOff } from "lucide-react";
import Link from "next/link";
import { Button } from "@multica/ui/components/ui/button";

export default function OfflinePage() {
  return (
    <div className="flex h-screen w-screen flex-col items-center justify-center bg-background text-foreground relative overflow-hidden">
      <div className="z-10 flex flex-col items-center max-w-md text-center space-y-6 p-8 rounded-2xl border border-border bg-card shadow-xl">
        <div className="w-16 h-16 rounded-full bg-muted flex items-center justify-center mb-2 ring-4 ring-background shadow-sm">
          <WifiOff className="w-8 h-8 text-muted-foreground" />
        </div>
        
        <div className="space-y-2">
          <h1 className="text-2xl font-semibold tracking-tight">You are offline</h1>
          <p className="text-sm text-muted-foreground leading-relaxed">
            It looks like you've lost your internet connection. Some features may be unavailable until you reconnect.
          </p>
        </div>
        
        <div className="w-full pt-4 space-y-3">
          <Link href="/" className="w-full">
            <Button variant="default" className="w-full">
              Retry Connection
            </Button>
          </Link>
        </div>
      </div>
    </div>
  );
}
