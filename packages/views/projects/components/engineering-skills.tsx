"use client";

import { Info, LoaderCircle } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { useT } from "../../i18n";
import type { StrixnovaSkillBinding } from "./engineering-utils";

export function EngineeringSkills({ bindings, loading, error }: {
  bindings: readonly StrixnovaSkillBinding[];
  loading: boolean;
  error: boolean;
}) {
  const { t } = useT("engineering");
  return (
    <section className="rounded-lg border bg-muted/20 p-3" aria-label={t(($) => $.skills.title)} data-engineering-skills>
      <div className="flex items-start gap-2">
        {loading ? <LoaderCircle className="mt-0.5 size-4 shrink-0 animate-spin text-muted-foreground" aria-hidden="true" /> : <Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden="true" />}
        <div className="min-w-0 flex-1">
          <h2 className="text-caption font-medium">{t(($) => $.skills.title)}</h2>
          {loading ? <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.skills.loading)}</p> : null}
          {!loading && error ? <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.skills.unavailable)}</p> : null}
          {!loading && !error && bindings.length === 0 ? <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.skills.empty)}</p> : null}
          {!loading && !error && bindings.length ? (
            <ul className="mt-2 space-y-2">
              {bindings.map(({ agent, skills }) => (
                <li key={agent.id} className="flex flex-wrap items-center gap-1.5" data-skill-agent={agent.id}>
                  <span className="mr-1 text-caption font-medium">{agent.name}</span>
                  {skills.map((skill) => <Badge key={skill} variant="secondary" className="font-mono text-micro">{skill}</Badge>)}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      </div>
    </section>
  );
}
