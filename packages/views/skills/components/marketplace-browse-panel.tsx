"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Download, Loader2, Search } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { MarketplaceSkill, Skill } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  skillDetailOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useT } from "../../i18n";

function seedAfterCreate(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  skill: Skill,
) {
  qc.setQueryData(skillDetailOptions(wsId, skill.id).queryKey, skill);
  qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
  qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
}

function MarketplaceSkillCard({
  skill,
  importing,
  onImport,
}: {
  skill: MarketplaceSkill;
  importing: boolean;
  onImport: () => void;
}) {
  const { t } = useT("skills");

  return (
    <div className="flex min-h-[132px] flex-col rounded-lg border bg-card p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="truncate text-sm font-medium">{skill.name}</h3>
          <p className="mt-1 line-clamp-3 text-xs text-muted-foreground">
            {skill.description}
          </p>
        </div>
        <Badge variant="secondary" className="shrink-0">
          {t(($) => $.create.marketplace.source_badge)}
        </Badge>
      </div>
      <div className="mt-auto flex items-end justify-between gap-3 pt-3">
        <p className="min-w-0 truncate font-mono text-xs text-muted-foreground">
          {skill.source_url}
        </p>
        <Button
          type="button"
          size="sm"
          onClick={onImport}
          disabled={importing}
          className="shrink-0"
        >
          {importing ? (
            <>
              <Loader2 className="h-3 w-3 animate-spin" />
              {t(($) => $.create.marketplace.importing)}
            </>
          ) : (
            <>
              <Download className="h-3 w-3" />
              {t(($) => $.create.marketplace.import)}
            </>
          )}
        </Button>
      </div>
    </div>
  );
}

export function MarketplaceBrowsePanel({
  onImported,
}: {
  onImported: (skill: Skill) => void;
}) {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [importingURL, setImportingURL] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      setDebouncedQuery(query.trim());
    }, 300);
    return () => window.clearTimeout(timeout);
  }, [query]);

  const marketplaceQuery = useQuery({
    queryKey: ["marketplace-skills", wsId, debouncedQuery],
    queryFn: () => api.searchMarketplace({ q: debouncedQuery, limit: 20 }),
    enabled: debouncedQuery.length > 0,
  });

  const handleImport = async (skill: MarketplaceSkill) => {
    setImportingURL(skill.source_url);
    try {
      const imported = await api.importSkill({ url: skill.source_url });
      seedAfterCreate(qc, wsId, imported);
      toast.success(t(($) => $.create.marketplace.toast_imported));
      onImported(imported);
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t(($) => $.create.url.fallback_error),
      );
    } finally {
      setImportingURL("");
    }
  };

  const skills = marketplaceQuery.data?.skills ?? [];

  const middle = (() => {
    if (!debouncedQuery) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">
            {t(($) => $.create.marketplace.empty_query)}
          </p>
        </div>
      );
    }
    if (marketplaceQuery.isLoading) {
      return (
        <div className="grid gap-3 sm:grid-cols-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="rounded-lg border p-4">
              <Skeleton className="h-4 w-36" />
              <Skeleton className="mt-3 h-3 w-full" />
              <Skeleton className="mt-2 h-3 w-2/3" />
              <Skeleton className="mt-6 h-8 w-24" />
            </div>
          ))}
        </div>
      );
    }
    if (marketplaceQuery.error) {
      return (
        <div className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          {t(($) => $.create.marketplace.error)}
        </div>
      );
    }
    if (skills.length === 0) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">
            {t(($) => $.create.marketplace.no_results, { query: debouncedQuery })}
          </p>
        </div>
      );
    }
    return (
      <div className="grid gap-3 sm:grid-cols-2">
        {skills.map((skill) => (
          <MarketplaceSkillCard
            key={skill.source_url}
            skill={skill}
            importing={importingURL === skill.source_url}
            onImport={() => handleImport(skill)}
          />
        ))}
      </div>
    );
  })();

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 border-b px-5 py-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t(($) => $.create.marketplace.search_placeholder)}
            className="pl-9"
          />
        </div>
      </div>
      <div
        ref={scrollRef}
        style={fadeStyle}
        aria-disabled={!!importingURL || undefined}
        className={`flex-1 min-h-0 overflow-y-auto px-5 py-3 ${
          importingURL ? "pointer-events-none opacity-60" : ""
        }`}
      >
        {middle}
      </div>
    </div>
  );
}
