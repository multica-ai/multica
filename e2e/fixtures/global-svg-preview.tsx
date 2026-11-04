import React, { useRef } from "react";
import { createRoot } from "react-dom/client";
import "../../apps/desktop/src/renderer/src/globals.css";

function Fixture() {
  const iframe = useRef<HTMLIFrameElement>(null);
  return <div className="flex h-dvh bg-background text-foreground">
    <aside className="w-72 shrink-0 border-r p-4"><span>Workspace navigation</span><input aria-label="Main draft" defaultValue="Keep my main draft" className="mt-4 w-full border" /></aside>
    <main className="flex min-h-0 min-w-0 flex-1 flex-col">
      <header className="flex h-12 shrink-0 gap-4 border-b p-2"><button id="finish">Finish generation</button><button id="theme">Switch theme</button><button id="locale">Switch language</button></header>
      <iframe ref={iframe} title="HTML GUI validation" className="min-h-0 w-full flex-1 border-0" />
    </main>
  </div>;
}
document.documentElement.classList.add("dark");
document.body.style.margin = "0";
createRoot(document.getElementById("root")!).render(<Fixture />);
