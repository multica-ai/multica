"use client";

import { useState, useMemo } from "react";
import { Puzzle, Search, Check, Loader2 } from "lucide-react";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { Input } from "@multica/ui/components/ui/input";

interface PluginPickerListProps {
  plugins: BuiltinPlugin[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  loading?: boolean;
}

/**
 * Searchable list of builtin plugins. Used by both PluginSelect (create
 * dialog) and PluginAttach (inspector). Displays each plugin as a row with
 * name (bold) + description (muted, one line, truncated). Click to select;
 * selected row gets a Check icon + accent background.
 */
export function PluginPickerList({
  plugins,
  selectedId,
  onSelect,
  loading = false,
}: PluginPickerListProps) {
  const [query, setQuery] = useState("");

  const filtered = useMemo(() => {
    if (!query.trim()) return plugins;
    const q = query.toLowerCase();
    return plugins.filter(
      (p) =>
        p.name.toLowerCase().includes(q) ||
        p.description.toLowerCase().includes(q) ||
        p.slug.toLowerCase().includes(q) ||
        p.category.toLowerCase().includes(q),
    );
  }, [plugins, query]);

  if (loading) {
    return (
      <div className="flex items-center gap-2 px-3 py-6 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        Loading plugins...
      </div>
    );
  }

  return (
    <div>
      {/* Search */}
      <div className="border-b border-border p-2">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground/50" />
          <Input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search plugins..."
            className="h-8 pl-7 text-xs"
          />
        </div>
      </div>

      {/* List */}
      <div className="max-h-72 overflow-y-auto p-1">
        {plugins.length === 0 ? (
          <div className="px-3 py-6 text-center text-sm text-muted-foreground">
            No plugins available
          </div>
        ) : filtered.length === 0 ? (
          <div className="px-3 py-6 text-center text-xs text-muted-foreground">
            No plugins match your search
          </div>
        ) : (
          filtered.map((plugin) => {
            const isSelected = plugin.id === selectedId;
            return (
              <button
                key={plugin.id}
                type="button"
                onClick={() => onSelect(plugin.id)}
                className={`flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-sm transition-colors hover:bg-accent/50 ${
                  isSelected ? "bg-accent" : ""
                }`}
              >
                <Puzzle className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">
                    {plugin.name}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">
                    {plugin.description}
                  </div>
                </div>
                {isSelected && (
                  <Check className="h-4 w-4 shrink-0 text-primary" />
                )}
              </button>
            );
          })
        )}
      </div>
    </div>
  );
}
