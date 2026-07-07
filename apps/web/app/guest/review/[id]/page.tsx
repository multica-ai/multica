import Link from "next/link";
import { Lock, ArrowRight } from "lucide-react";

export default async function GuestReviewPage({ params }: { params: Promise<{ id: string }> }) {
  const resolvedParams = await params;
  return (
    <div className="flex h-screen w-screen flex-col items-center justify-center bg-background text-foreground relative overflow-hidden">
      {/* Background gradients for a premium feel */}
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[800px] h-[800px] bg-primary/5 rounded-full blur-[100px] pointer-events-none" />
      <div className="absolute top-0 left-0 w-full h-1 bg-gradient-to-r from-transparent via-primary/50 to-transparent" />
      
      <div className="z-10 flex flex-col items-center max-w-md text-center space-y-6 p-8 rounded-2xl border border-border bg-card shadow-xl">
        <div className="w-16 h-16 rounded-full bg-muted flex items-center justify-center mb-2 ring-4 ring-background shadow-sm">
          <Lock className="w-8 h-8 text-muted-foreground" />
        </div>
        
        <div className="space-y-2">
          <h1 className="text-2xl font-semibold tracking-tight">Guest Access Required</h1>
          <p className="text-sm text-muted-foreground leading-relaxed">
            Guest share mode is currently disabled for this workspace. To view and comment on this media asset, you must log in with an authorized account.
          </p>
        </div>
        
        <div className="w-full pt-4 space-y-3">
          <Link 
            href="/auth/login" 
            className="flex items-center justify-center w-full gap-2 px-4 py-2.5 text-sm font-medium text-primary-foreground bg-primary rounded-lg hover:bg-primary/90 transition-colors shadow-sm"
          >
            Go to Login
            <ArrowRight className="w-4 h-4" />
          </Link>
          <p className="text-[11px] text-muted-foreground">
            Asset ID: <span className="font-mono text-muted-foreground/70">{resolvedParams.id}</span>
          </p>
        </div>
      </div>
    </div>
  );
}
