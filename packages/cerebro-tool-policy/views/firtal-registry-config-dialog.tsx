"use client";

import { useEffect, useMemo, useState } from "react";
import { Loader2, Search } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Label } from "@multica/ui/components/ui/label";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  getFirtalRegistryGrantConfig,
  listFirtalRegistryDataSources,
  setFirtalRegistryGrantConfig,
  type FirtalRegistryGrantConfig,
} from "../api";

const agentToolsKey = (agentId: string) =>
  ["cerebro", "agent-tools", agentId] as const;

const dataSourcesKey = (agentId: string) =>
  ["cerebro", "firtal-registry-data-sources", agentId] as const;

const configKey = (agentId: string) =>
  [...agentToolsKey(agentId), "firtal_registry-config"] as const;

function parseGrantConfig(raw: FirtalRegistryGrantConfig): {
  allowAll: boolean;
  selected: Set<string>;
} {
  if (raw.allowed_data_sources_all) {
    return { allowAll: true, selected: new Set() };
  }
  const ids = Array.isArray(raw.allowed_data_sources)
    ? raw.allowed_data_sources.filter((id) => typeof id === "string" && id.trim())
    : [];
  return { allowAll: false, selected: new Set(ids) };
}

export function FirtalRegistryConfigDialog({
  agentId,
  open,
  onOpenChange,
}: {
  agentId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const qc = useQueryClient();
  const [search, setSearch] = useState("");
  const [allowAll, setAllowAll] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(() => new Set());

  const configQuery = useQuery({
    queryKey: configKey(agentId),
    queryFn: () => getFirtalRegistryGrantConfig(agentId),
    enabled: open,
  });

  const dataSourcesQuery = useQuery({
    queryKey: dataSourcesKey(agentId),
    queryFn: () => listFirtalRegistryDataSources(agentId),
    enabled: open,
  });

  useEffect(() => {
    if (!open || !configQuery.isSuccess) return;
    const parsed = parseGrantConfig(configQuery.data);
    setAllowAll(parsed.allowAll);
    setSelected(new Set(parsed.selected));
    setSearch("");
  }, [open, configQuery.isSuccess, configQuery.data]);

  const saveMutation = useMutation({
    mutationFn: async () => {
      const config: FirtalRegistryGrantConfig = allowAll
        ? { allowed_data_sources_all: true }
        : { allowed_data_sources: Array.from(selected) };
      await setFirtalRegistryGrantConfig(agentId, config);
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: agentToolsKey(agentId) });
      toast.success("Data source permissions saved");
      onOpenChange(false);
    },
    onError: (err) => {
      toast.error(
        err instanceof Error ? err.message : "Failed to save permissions",
      );
    },
  });

  const filteredSources = useMemo(() => {
    const needle = search.trim().toLowerCase();
    const items = dataSourcesQuery.data ?? [];
    if (!needle) return items;
    return items.filter((ds) => {
      const desc = ds.description ?? "";
      return (
        ds.name.toLowerCase().includes(needle) ||
        ds.id.toLowerCase().includes(needle) ||
        desc.toLowerCase().includes(needle)
      );
    });
  }, [dataSourcesQuery.data, search]);

  const toggleId = (id: string, checked: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full max-w-xl flex-col gap-0 p-0 sm:max-w-xl"
      >
        <SheetHeader className="border-b">
          <SheetTitle>firtal_registry — data sources</SheetTitle>
          <SheetDescription>
            Choose which data sources the agent may see via Firtal Data Registry.
            Without permission the agent sees no sources.
          </SheetDescription>
        </SheetHeader>

        <div className="flex flex-1 flex-col gap-4 overflow-y-auto p-4">
          <div className="flex items-center justify-between gap-3 rounded-md border p-3">
            <div className="space-y-0.5">
              <Label htmlFor="allow-all-ds">Tillad alle data sources</Label>
              <p className="text-xs text-muted-foreground">
                Disables the allowlist — the agent sees everything in the registry.
              </p>
            </div>
            <Switch
              id="allow-all-ds"
              checked={allowAll}
              onCheckedChange={setAllowAll}
            />
          </div>

          {!allowAll && (
            <div className="flex flex-1 flex-col gap-2">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <input
                  type="search"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search data sources…"
                  className="h-8 w-full rounded-md border bg-background pl-7 pr-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
                />
              </div>

              {dataSourcesQuery.isLoading || configQuery.isLoading ? (
                <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Henter data sources…
                </div>
              ) : dataSourcesQuery.isError ? (
                <p className="py-4 text-sm text-destructive">
                  Kunne ikke hente data sources fra registret. Tjek at
                  workspace har data registry konfigureret.
                </p>
              ) : filteredSources.length === 0 ? (
                <p className="py-4 text-sm text-muted-foreground">
                  No data sources match the search.
                </p>
              ) : (
                <ul className="flex-1 space-y-1 overflow-y-auto rounded-md border p-2">
                  {filteredSources.map((ds) => {
                    const checked = selected.has(ds.id);
                    return (
                      <li key={ds.id}>
                        <label className="flex cursor-pointer items-start gap-2 rounded-sm px-2 py-1.5 hover:bg-muted/50">
                          <Checkbox
                            checked={checked}
                            onCheckedChange={(v) =>
                              toggleId(ds.id, v === true)
                            }
                            className="mt-0.5"
                          />
                          <span className="min-w-0 flex-1">
                            <span className="block text-sm font-medium">
                              {ds.name}
                            </span>
                            {ds.description ? (
                              <span className="block text-xs text-muted-foreground">
                                {ds.description}
                              </span>
                            ) : null}
                            <span className="block font-mono text-[10px] text-muted-foreground/80">
                              {ds.id}
                            </span>
                          </span>
                        </label>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
          )}
        </div>

        <SheetFooter className="border-t">
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
            >
              Annuller
            </Button>
            <Button
              type="button"
              disabled={
                saveMutation.isPending ||
                (!allowAll && selected.size === 0) ||
                dataSourcesQuery.isLoading
              }
              onClick={() => saveMutation.mutate()}
            >
              {saveMutation.isPending ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Gemmer…
                </>
              ) : (
                "Gem"
              )}
            </Button>
          </div>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
