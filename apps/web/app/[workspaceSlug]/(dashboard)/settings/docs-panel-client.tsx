"use client";

import { useState, useEffect, type ReactNode } from "react";
import { useSearchParams } from "next/navigation";
import { cn } from "@multica/ui/lib/utils";

export interface DocPage {
  slug: string;
  title: string;
  description?: string;
  /** First path segment, used to group entries in the left nav. */
  group?: string;
  content: ReactNode;
}

interface DocsPanelClientProps {
  pages: DocPage[];
}

/**
 * Native docs panel for the Settings page. Renders a list of docs in a
 * left nav and the active page's content on the right. Uses Multica's
 * existing prose tokens for styling — no fumadocs UI, no iframe.
 */
export function DocsPanelClient({ pages }: DocsPanelClientProps) {
  const searchParams = useSearchParams();
  const initialSlug = searchParams.get("doc") ?? pages[0]?.slug ?? "";
  const [activeSlug, setActiveSlug] = useState(initialSlug);
  const active = pages.find((p) => p.slug === activeSlug) ?? pages[0];

  // Keep ?doc=<slug> in sync with the active page so users can deep-link.
  // Use replaceState to avoid pushing history entries for every nav click,
  // and skip the Next router so the rest of Settings doesn't re-render.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (active?.slug && active.slug !== "/") {
      url.searchParams.set("doc", active.slug);
    } else {
      url.searchParams.delete("doc");
    }
    window.history.replaceState(null, "", url.toString());
  }, [active?.slug]);

  // Group pages by their top-level segment so the nav is structured.
  const grouped = new Map<string, DocPage[]>();
  for (const page of pages) {
    const key = page.group ?? "";
    const list = grouped.get(key) ?? [];
    list.push(page);
    grouped.set(key, list);
  }
  // Sort: ungrouped (top-level) first, then alphabetical groups.
  const sortedGroups = Array.from(grouped.entries()).sort(([a], [b]) => {
    if (a === "") return -1;
    if (b === "") return 1;
    return a.localeCompare(b);
  });

  return (
    <div className="absolute inset-0 flex">
      {/* Inner left nav */}
      <nav className="w-56 shrink-0 border-r overflow-y-auto p-3">
        {sortedGroups.map(([group, groupPages]) => (
          <div key={group || "_top"} className="mb-3">
            {group && (
              <div className="px-2 pb-1 pt-2 text-xs font-medium text-muted-foreground capitalize">
                {group.replace(/-/g, " ")}
              </div>
            )}
            {groupPages.map((page) => (
              <button
                key={page.slug}
                type="button"
                onClick={() => setActiveSlug(page.slug)}
                className={cn(
                  "w-full text-left rounded-md px-2 py-1.5 text-sm",
                  page.slug === active?.slug
                    ? "bg-accent text-accent-foreground"
                    : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
                )}
              >
                {page.title}
              </button>
            ))}
          </div>
        ))}
      </nav>

      {/* Page content */}
      <div className="flex-1 min-w-0 overflow-y-auto">
        <article className="docs-prose max-w-3xl mx-auto px-8 py-8">
          {active && (
            <>
              <h1 className="text-2xl font-semibold mb-1">{active.title}</h1>
              {active.description && (
                <p className="text-muted-foreground mt-0 mb-6">
                  {active.description}
                </p>
              )}
              {active.content}
            </>
          )}
        </article>
      </div>

      {/* Inline styles for MDX typography. Scoped to .docs-prose so it
          doesn't leak elsewhere, and uses Multica's design tokens for
          colors (no hardcoded hex). */}
      <style>{`
        .docs-prose { color: var(--foreground); font-size: 0.9rem; line-height: 1.7; }
        .docs-prose h2 { font-size: 1.25rem; font-weight: 600; margin-top: 2rem; margin-bottom: 0.75rem; }
        .docs-prose h3 { font-size: 1.0625rem; font-weight: 600; margin-top: 1.5rem; margin-bottom: 0.5rem; }
        .docs-prose p { margin: 0.75rem 0; }
        .docs-prose ul { list-style: disc; padding-left: 1.25rem; margin: 0.75rem 0; }
        .docs-prose ol { list-style: decimal; padding-left: 1.25rem; margin: 0.75rem 0; }
        .docs-prose li { margin: 0.25rem 0; }
        .docs-prose a { color: var(--primary); text-decoration: underline; text-underline-offset: 2px; }
        .docs-prose strong { font-weight: 600; }
        .docs-prose code { background: var(--muted); padding: 0.125rem 0.375rem; border-radius: 0.25rem; font-size: 0.85em; font-family: var(--font-mono); }
        .docs-prose pre { background: var(--muted); border: 1px solid var(--border); border-radius: 0.5rem; padding: 1rem; overflow-x: auto; margin: 1rem 0; }
        .docs-prose pre code { background: transparent; padding: 0; font-size: 0.85em; }
        .docs-prose blockquote { border-left: 3px solid var(--border); padding-left: 1rem; color: var(--muted-foreground); margin: 1rem 0; }
        .docs-prose hr { border: 0; border-top: 1px solid var(--border); margin: 2rem 0; }
        .docs-prose table { border-collapse: collapse; width: 100%; margin: 1rem 0; }
        .docs-prose th, .docs-prose td { border: 1px solid var(--border); padding: 0.5rem 0.75rem; text-align: left; }
        .docs-prose th { background: var(--muted); font-weight: 600; }
      `}</style>
    </div>
  );
}
