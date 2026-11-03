"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Delete } from "@multica/ui/icons";
import {
  knowledgeAccessOptions,
  usePutKnowledgeGrant,
  useRemoveKnowledgeGrant,
  type KnowledgeGrant,
  type KnowledgeSubjectType,
} from "@multica/core/knowledge";
import { officeFloorsOptions } from "@multica/core/office/queries";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { useT } from "../i18n";

const SUBJECT_TYPES: KnowledgeSubjectType[] = ["squad", "floor", "agent"];

/**
 * The grants on one restricted knowledge base. A squad, a floor or an agent is
 * granted read or write; the agents it reaches get the base's endpoint, and
 * everyone else cannot see the base. Owners and admins only.
 */
export function KnowledgeAccessEditor({ wsId, baseKey }: { wsId: string; baseKey: string }) {
  const { t } = useT("knowledge");
  const access = useQuery(knowledgeAccessOptions(wsId, baseKey));
  const squads = useQuery(squadListOptions(wsId));
  const floors = useQuery(officeFloorsOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const put = usePutKnowledgeGrant(wsId);
  const remove = useRemoveKnowledgeGrant(wsId);

  const [subjectType, setSubjectType] = useState<KnowledgeSubjectType>("squad");
  const [subjectId, setSubjectId] = useState("");
  const [level, setLevel] = useState<"read" | "write">("read");

  const options: { id: string; name: string }[] =
    subjectType === "squad"
      ? (squads.data ?? []).map((s) => ({ id: s.id, name: s.name }))
      : subjectType === "floor"
        ? (floors.data ?? []).map((f) => ({ id: f.id, name: f.name }))
        : (agents.data ?? []).map((a) => ({ id: a.id, name: a.name }));
  const nameOf = (type: string, id: string): string => {
    const list =
      type === "squad" ? squads.data : type === "floor" ? floors.data : type === "agent" ? agents.data : undefined;
    return list?.find((item) => item.id === id)?.name ?? id;
  };

  const subjectTypeItems = SUBJECT_TYPES.map((type) => ({ value: type, label: t(($) => $.access.subject[type]) }));
  const subjectItems = options.map((option) => ({ value: option.id, label: option.name }));
  const levelItems = [
    { value: "read", label: t(($) => $.access.level.read) },
    { value: "write", label: t(($) => $.access.level.write) },
  ];

  const subjectLabel = (type: KnowledgeGrant["subject_type"]) =>
    type === "unknown" ? t(($) => $.access.subject.unknown) : t(($) => $.access.subject[type]);

  const addGrant = () => {
    if (!subjectId) return;
    put.mutate(
      { key: baseKey, subjectType, subjectId, access: level },
      {
        onSuccess: () => setSubjectId(""),
        onError: (error) => toast.error(error.message || t(($) => $.access.toast.failed)),
      },
    );
  };

  if (access.isPending) {
    return <p className="text-sm text-muted-foreground">{t(($) => $.access.loading)}</p>;
  }
  if (access.isError || !access.data) {
    return <p className="text-sm text-destructive">{t(($) => $.access.load_error)}</p>;
  }
  const grants = access.data.grants;

  return (
    <div className="flex flex-col gap-3">
      {grants.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t(($) => $.access.empty)}</p>
      ) : (
        <ul className="flex flex-col divide-y rounded-md border">
          {grants.map((grant) => (
            <li key={`${grant.subject_type}:${grant.subject_id}`} className="flex items-center gap-3 px-3 py-2 text-sm">
              <span className="flex-1 truncate">
                {grant.subject_type === "unknown"
                  ? grant.subject_id
                  : nameOf(grant.subject_type, grant.subject_id)}
              </span>
              <Badge variant="outline">{subjectLabel(grant.subject_type)}</Badge>
              <Badge variant="secondary">
                {grant.access === "write" ? t(($) => $.access.level.write) : t(($) => $.access.level.read)}
              </Badge>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.access.remove)}
                disabled={grant.subject_type === "unknown" || remove.isPending}
                onClick={() =>
                  remove.mutate(
                    { key: baseKey, subjectType: grant.subject_type as KnowledgeSubjectType, subjectId: grant.subject_id },
                    { onError: (error) => toast.error(error.message || t(($) => $.access.toast.failed)) },
                  )
                }
              >
                <Delete className="size-4" />
              </Button>
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap items-end gap-2">
        <div className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">{t(($) => $.access.add.subject_type)}</span>
          <Select
            items={subjectTypeItems}
            value={subjectType}
            onValueChange={(value) => {
              if (!value) return;
              setSubjectType(value as KnowledgeSubjectType);
              setSubjectId("");
            }}
          >
            <SelectTrigger className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {SUBJECT_TYPES.map((type) => (
                <SelectItem key={type} value={type}>
                  {t(($) => $.access.subject[type])}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex min-w-48 flex-1 flex-col gap-1">
          <span className="text-xs text-muted-foreground">{t(($) => $.access.add.subject)}</span>
          <Select items={subjectItems} value={subjectId} onValueChange={(value) => value && setSubjectId(value)}>
            <SelectTrigger className="w-full">
              <SelectValue placeholder={t(($) => $.access.add.subject_placeholder)} />
            </SelectTrigger>
            <SelectContent>
              {options.map((option) => (
                <SelectItem key={option.id} value={option.id}>
                  {option.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">{t(($) => $.access.add.level)}</span>
          <Select items={levelItems} value={level} onValueChange={(value) => value && setLevel(value as "read" | "write")}>
            <SelectTrigger className="w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="read">{t(($) => $.access.level.read)}</SelectItem>
              <SelectItem value="write">{t(($) => $.access.level.write)}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <Button onClick={addGrant} disabled={!subjectId || put.isPending} aria-busy={put.isPending}>
          {t(($) => $.access.add.submit)}
        </Button>
      </div>
    </div>
  );
}
