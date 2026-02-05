"use client";

import { ThemeToggle } from "./theme-toggle";

export function Header() {
  return (
      <header className="container flex justify-between items-center p-2">
          <div className="flex items-center gap-2.5">
            <img src="/logo.svg" alt="Multica" className="size-6 rounded-md" />
            <span className="text-sm tracking-wide font-[family-name:var(--font-brand)]">
              Multica
            </span>
          </div>
          <div className="flex items-center gap-1">
            <ThemeToggle />
          </div>
      </header>
  );
}
