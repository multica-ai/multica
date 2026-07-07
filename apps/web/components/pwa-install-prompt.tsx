"use client";

import { useEffect, useState } from "react";
import { Download, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";

interface BeforeInstallPromptEvent extends Event {
  readonly platforms: string[];
  readonly userChoice: Promise<{
    outcome: "accepted" | "dismissed";
    platform: string;
  }>;
  prompt(): Promise<void>;
}

export function PwaInstallPrompt() {
  const [deferredPrompt, setDeferredPrompt] = useState<BeforeInstallPromptEvent | null>(null);
  const [showPrompt, setShowPrompt] = useState(false);

  useEffect(() => {
    const handleBeforeInstallPrompt = (e: Event) => {
      // Prevent the mini-infobar from appearing on mobile
      e.preventDefault();
      // Stash the event so it can be triggered later.
      setDeferredPrompt(e as BeforeInstallPromptEvent);
      
      // Optionally check if we've dismissed it recently in localStorage
      const dismissed = localStorage.getItem("pwa-install-dismissed");
      if (dismissed) {
        const dismissTime = parseInt(dismissed, 10);
        // Only show again after 7 days
        if (Date.now() - dismissTime < 7 * 24 * 60 * 60 * 1000) {
          return;
        }
      }

      setShowPrompt(true);
    };

    window.addEventListener("beforeinstallprompt", handleBeforeInstallPrompt);
    
    // If the app is successfully installed, we can hide the prompt and clear the event
    window.addEventListener("appinstalled", () => {
      setShowPrompt(false);
      setDeferredPrompt(null);
    });

    return () => {
      window.removeEventListener("beforeinstallprompt", handleBeforeInstallPrompt);
    };
  }, []);

  const handleInstallClick = async () => {
    if (!deferredPrompt) return;
    
    // Show the install prompt
    deferredPrompt.prompt();
    
    // Wait for the user to respond to the prompt
    const { outcome } = await deferredPrompt.userChoice;
    
    // We've used the prompt, and can't use it again, discard it
    setDeferredPrompt(null);
    setShowPrompt(false);
  };

  const handleDismiss = () => {
    setShowPrompt(false);
    localStorage.setItem("pwa-install-dismissed", Date.now().toString());
  };

  if (!showPrompt) return null;

  return (
    <div className="fixed bottom-4 left-4 right-4 md:left-auto md:right-4 md:w-80 z-50 animate-in slide-in-from-bottom-5 fade-in duration-300">
      <div className="bg-card border border-border shadow-lg rounded-xl p-4 flex flex-col gap-3 relative overflow-hidden">
        <button 
          onClick={handleDismiss}
          className="absolute top-2 right-2 text-muted-foreground hover:text-foreground transition-colors"
        >
          <X className="w-4 h-4" />
        </button>
        
        <div className="flex items-start gap-3">
          <div className="bg-primary/10 text-primary p-2 rounded-lg shrink-0">
            <Download className="w-5 h-5" />
          </div>
          <div className="flex-1 pr-4">
            <h3 className="text-sm font-semibold text-foreground">Install Multica</h3>
            <p className="text-xs text-muted-foreground mt-0.5">
              Install our app for a better mobile experience and offline access.
            </p>
          </div>
        </div>
        
        <div className="flex justify-end gap-2 mt-1">
          <Button variant="ghost" size="sm" onClick={handleDismiss} className="text-xs h-8">
            Later
          </Button>
          <Button size="sm" onClick={handleInstallClick} className="text-xs h-8">
            Install App
          </Button>
        </div>
      </div>
    </div>
  );
}
