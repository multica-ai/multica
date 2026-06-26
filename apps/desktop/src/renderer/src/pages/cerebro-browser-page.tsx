// FIR-2037: personal browser tab (renderer side).
//
// This React page is the *chrome* of the Browser tab — the address bar and
// back/forward/reload controls. The actual web page is a native
// WebContentsView owned by the main process (see main/cerebro-browser-pane.ts);
// this page measures the host rectangle below the toolbar and tells main to
// position the native pane exactly there. Switching away unmounts the page,
// which hides the pane (its logged-in session is kept alive for re-show).
//
// Gated by the `cerebro_browser` feature flag.

import { useEffect, useRef, useState, useCallback } from "react";
import { ArrowLeft, ArrowRight, RotateCw, Globe } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

interface NavState {
  url: string;
  title: string;
  canGoBack: boolean;
  canGoForward: boolean;
  isLoading: boolean;
}

export function CerebroBrowserPage(): React.JSX.Element {
  const enabled = useFeatureFlag("cerebro_browser");
  const hostRef = useRef<HTMLDivElement | null>(null);
  const [address, setAddress] = useState("");
  // Don't fight the user while they're editing the address bar — only mirror
  // the live URL into the field when the input is not focused.
  const addressFocusedRef = useRef(false);
  const [nav, setNav] = useState<NavState>({
    url: "",
    title: "",
    canGoBack: false,
    canGoForward: false,
    isLoading: false,
  });

  const api = typeof window !== "undefined" ? window.cerebroBrowser : undefined;

  // Push the current host-div rectangle to main so the native pane lines up
  // exactly under the toolbar. Called on mount, resize, and layout shifts.
  const syncBounds = useCallback(() => {
    if (!api || !hostRef.current) return;
    const r = hostRef.current.getBoundingClientRect();
    void api.setBounds({
      x: r.left,
      y: r.top,
      width: r.width,
      height: r.height,
    });
  }, [api]);

  // Open the pane on mount, hide it on unmount. Keep the native session alive
  // across tab switches — we only hide, never destroy.
  useEffect(() => {
    if (!enabled || !api) return;
    void api.open().then(syncBounds);
    const unsub = api.onNavState((state) => {
      setNav(state);
      if (!addressFocusedRef.current) setAddress(state.url);
    });
    return () => {
      unsub();
      void api.hide();
    };
  }, [enabled, api, syncBounds]);

  // Keep the pane glued to the host rect as the window/layout resizes.
  useEffect(() => {
    if (!enabled || !api || !hostRef.current) return;
    const ro = new ResizeObserver(syncBounds);
    ro.observe(hostRef.current);
    window.addEventListener("resize", syncBounds);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", syncBounds);
    };
  }, [enabled, api, syncBounds]);

  // Reflect the browsed page title into the OS tab title (the tab system reads
  // document.title). Fall back to a static label before the first navigation.
  useEffect(() => {
    if (!enabled) return;
    document.title = nav.title || "Browser";
  }, [enabled, nav.title]);

  const submit = (e: React.FormEvent): void => {
    e.preventDefault();
    if (api && address.trim()) void api.navigate(address);
  };

  if (!enabled) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        The personal browser is not enabled for this workspace.
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col bg-background">
      <div className="flex items-center gap-1 border-b border-border px-2 py-1.5">
        <ToolbarButton
          label="Back"
          disabled={!nav.canGoBack}
          onClick={() => api?.back()}
        >
          <ArrowLeft className="size-4" />
        </ToolbarButton>
        <ToolbarButton
          label="Forward"
          disabled={!nav.canGoForward}
          onClick={() => api?.forward()}
        >
          <ArrowRight className="size-4" />
        </ToolbarButton>
        <ToolbarButton label="Reload" onClick={() => api?.reload()}>
          <RotateCw className={`size-4 ${nav.isLoading ? "animate-spin" : ""}`} />
        </ToolbarButton>
        <form onSubmit={submit} className="flex flex-1 items-center">
          <div className="flex flex-1 items-center gap-2 rounded-md border border-border bg-muted/40 px-2.5">
            <Globe className="size-4 shrink-0 text-muted-foreground" />
            <input
              value={address}
              onChange={(e) => setAddress(e.target.value)}
              onFocus={() => {
                addressFocusedRef.current = true;
              }}
              onBlur={() => {
                addressFocusedRef.current = false;
                setAddress(nav.url);
              }}
              placeholder="Search or enter address"
              className="h-8 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
            />
          </div>
        </form>
      </div>
      {/* The native browser pane is painted over this spacer by the main process. */}
      <div ref={hostRef} className="flex-1" />
    </div>
  );
}

function ToolbarButton({
  children,
  label,
  onClick,
  disabled,
}: {
  children: React.ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}): React.JSX.Element {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      disabled={disabled}
      className="flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-40"
    >
      {children}
    </button>
  );
}
