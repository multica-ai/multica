"use client";

import { useEffect, useMemo, useState } from "react";
import { GitPullRequest, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";
import { useNavigation } from "@multica/views/navigation";
import {
  listRegisteredObjects,
  parseGithubPr,
  type ReferenceObject,
} from "@multica/cerebro-references/core";

const OBJECT_LABELS: Record<string, string> = {
  github_pr: "GitHub PR",
};

function objectLabel(object: ReferenceObject): string {
  return OBJECT_LABELS[object] ?? object;
}

function splitReferenceParam(raw: string | null): {
  object: ReferenceObject;
  refId: string;
} | null {
  if (!raw) return null;
  const [object, ...rest] = raw.split(":");
  const refId = rest.join(":");
  if (!object || !refId) return null;
  return { object, refId };
}

function normalizeReference(object: ReferenceObject, rawRefId: string): string | null {
  const refId = rawRefId.trim();
  if (!refId) return null;
  if (object !== "github_pr") return refId;
  const parsed = parseGithubPr(refId);
  return parsed ? `${parsed.org}/${parsed.repo}#${parsed.number}` : refId;
}

// Compact issue-list filter backed by ?reference=<object>:<ref_id>.
// It deliberately lives in cerebro-references so new object renderers can be
// added without touching the upstream issue toolbar again.
export function IssueListReferenceFilter({ className }: { className?: string }) {
  const navigation = useNavigation();
  const active = splitReferenceParam(navigation.searchParams.get("reference"));
  const objects = useMemo(() => {
    const registered = listRegisteredObjects();
    return registered.includes("github_pr")
      ? registered
      : ["github_pr", ...registered];
  }, []);

  const [object, setObject] = useState<ReferenceObject>(
    active?.object ?? objects[0] ?? "github_pr",
  );
  const [refId, setRefId] = useState(active?.refId ?? "");
  const [open, setOpen] = useState(false);

  useEffect(() => {
    setObject(active?.object ?? objects[0] ?? "github_pr");
    setRefId(active?.refId ?? "");
  }, [active?.object, active?.refId, objects]);

  const commit = (nextReference: string | null) => {
    const params = new URLSearchParams(navigation.searchParams);
    if (nextReference) {
      params.set("reference", nextReference);
    } else {
      params.delete("reference");
    }
    const query = params.toString();
    navigation.push(query ? `${navigation.pathname}?${query}` : navigation.pathname);
  };

  const apply = () => {
    const normalized = normalizeReference(object, refId);
    if (!normalized) return;
    commit(`${object}:${normalized}`);
    setOpen(false);
  };

  const clear = () => {
    commit(null);
    setOpen(false);
  };

  const activeLabel = active
    ? `${objectLabel(active.object)} ${active.refId}`
    : "Reference";

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            className={cn(
              "h-7 gap-1.5 text-muted-foreground",
              active && "border-primary/40 bg-primary/5 text-primary",
              className,
            )}
          >
            <GitPullRequest className="size-3.5" />
            <span className="max-w-48 truncate text-xs">{activeLabel}</span>
          </Button>
        }
      />
      <PopoverContent align="end" className="w-72 p-3">
        <form
          className="flex flex-col gap-3"
          onSubmit={(event) => {
            event.preventDefault();
            apply();
          }}
        >
          <div className="grid grid-cols-[104px_1fr] gap-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="issue-reference-object" className="text-xs">
                Type
              </Label>
              <NativeSelect
                id="issue-reference-object"
                value={object}
                onChange={(event) => setObject(event.target.value)}
              >
                {objects.map((item) => (
                  <NativeSelectOption key={item} value={item}>
                    {objectLabel(item)}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>
            <div className="flex min-w-0 flex-col gap-1.5">
              <Label htmlFor="issue-reference-id" className="text-xs">
                Reference
              </Label>
              <Input
                id="issue-reference-id"
                value={refId}
                onChange={(event) => setRefId(event.target.value)}
                placeholder="org/repo#123"
              />
            </div>
          </div>
          <div className="flex justify-between gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={!active}
              onClick={clear}
            >
              <X className="size-3.5" />
              Clear
            </Button>
            <Button type="submit" size="sm" disabled={!refId.trim()}>
              Apply
            </Button>
          </div>
        </form>
      </PopoverContent>
    </Popover>
  );
}
