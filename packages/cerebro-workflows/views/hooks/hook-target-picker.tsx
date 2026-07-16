"use client";

import { useEffect, useMemo, useState } from "react";
import { Check, ChevronsUpDown, Search } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";

export interface HookTargetOption { value: string; label: string; description?: string }
export type HookDirectory = Partial<Record<"agent" | "member" | "model" | "issue" | "project" | "workflow" | "session" | "squad" | "skill" | "artifact", HookTargetOption[]>> & {
  searchIssues?: (query: string) => Promise<HookTargetOption[]>;
};

export function HookTargetPicker({ label, value, options, onChange, onSearch }: { label: string; value: string; options: HookTargetOption[]; onChange: (value: string) => void; onSearch?: (query: string) => Promise<HookTargetOption[]> }) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [remoteOptions, setRemoteOptions] = useState<HookTargetOption[]>([]);
  const allOptions = remoteOptions.length > 0 ? remoteOptions : options;
  const selected = [...options, ...remoteOptions].find((option) => option.value === value);
  useEffect(() => {
    if (!onSearch || !open || search.trim().length < 2) { setRemoteOptions([]); return; }
    let current = true;
    const timer = setTimeout(() => { void onSearch(search.trim()).then((next) => { if (current) setRemoteOptions(next); }); }, 250);
    return () => { current = false; clearTimeout(timer); };
  }, [onSearch, open, search]);
  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    return onSearch ? allOptions : query ? allOptions.filter((option) => `${option.label} ${option.description ?? ""}`.toLowerCase().includes(query)) : allOptions;
  }, [allOptions, onSearch, search]);

  return <Popover open={open} onOpenChange={(next) => { setOpen(next); if (!next) setSearch(""); }}>
    <PopoverTrigger render={<Button type="button" variant="outline" className="h-9 w-full justify-between font-normal" aria-label={label} />}>
      <span className={selected ? "truncate" : "truncate text-muted-foreground"}>{selected?.label ?? `Select ${label.toLowerCase()}`}</span>
      <ChevronsUpDown className="size-3.5 text-muted-foreground" />
    </PopoverTrigger>
    <PopoverContent align="start" className="w-80 max-w-[calc(100vw-2rem)] gap-0 p-0">
      <div className="flex items-center gap-2 border-b p-2">
        <Search className="size-3.5 text-muted-foreground" />
        <Input autoFocus aria-label={`Search ${label.toLowerCase()}`} value={search} onChange={(event) => setSearch(event.target.value)} className="h-7 border-0 shadow-none focus-visible:ring-0" />
      </div>
      <div className="max-h-64 overflow-y-auto p-1">
        {filtered.map((option) => <button key={option.value} type="button" className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-sm hover:bg-accent" onClick={() => { onChange(option.value); setOpen(false); }}>
          <span className="min-w-0 flex-1"><span className="block truncate">{option.label}</span>{option.description && <span className="block truncate text-xs text-muted-foreground">{option.description}</span>}</span>
          {option.value === value && <Check className="size-3.5" />}
        </button>)}
        {filtered.length === 0 && <p className="p-3 text-center text-sm text-muted-foreground">No matching options</p>}
      </div>
    </PopoverContent>
  </Popover>;
}
