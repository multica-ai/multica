"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const LINKS = [
  { href: "/", label: "Workspaces" },
  { href: "/analytics", label: "Analytics" },
] as const;

export function Nav() {
  const pathname = usePathname();
  return (
    <nav className="border-b bg-card">
      <div className="mx-auto flex max-w-7xl items-center gap-1 px-6 py-2">
        {LINKS.map((link) => {
          const active = pathname === link.href;
          return (
            <Link
              key={link.href}
              href={link.href}
              className={`rounded-md px-3 py-1.5 text-body font-medium transition-colors ${
                active ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:text-foreground"
              }`}
            >
              {link.label}
            </Link>
          );
        })}
      </div>
    </nav>
  );
}
