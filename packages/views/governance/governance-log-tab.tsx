"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  GOVERNANCE_ACTOR_TYPES,
  GOVERNANCE_EVENT_KINDS,
  GOVERNANCE_SUBJECT_TYPES,
  governanceEventsOptions,
  parseGovernanceEventFilters,
  useExportGovernanceEvents,
  type GovernanceEvent,
  type GovernanceSubjectType,
} from "@multica/core/governance";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { Download } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { PAGE_GUTTER } from "../layout/page-header";
import { useLocale, useT } from "../i18n";
import { useNavigation } from "../navigation";
import { saveBlob } from "./save-blob";

const ALL = "";

function knownSubjectType(type: string): GovernanceSubjectType | null {
  return (GOVERNANCE_SUBJECT_TYPES as readonly string[]).includes(type) ? (type as GovernanceSubjectType) : null;
}

/**
 * The governance log: every event that changed how the company is governed,
 * newest first. The filters live in the URL (`kind`, `actor_type`,
 * `subject_type`) so a filtered view can be linked. Pages load on demand, and
 * the CSV export downloads the same filtered log.
 */
export function GovernanceLogTab() {
  const { t } = useT("governance");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const { getActorName } = useActorName();
  const { pathname, searchParams, replace } = useNavigation();
  const filters = parseGovernanceEventFilters(searchParams);
  const log = useInfiniteQuery(governanceEventsOptions(wsId, filters));
  const exportCsv = useExportGovernanceEvents();

  const events = log.data?.pages.flatMap((page) => page.events) ?? [];

  const setFilter = (name: "kind" | "actor_type" | "subject_type", value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(name, value);
    else next.delete(name);
    const search = next.toString();
    replace(`${pathname}${search ? `?${search}` : ""}`);
  };

  const kindItems = [
    { value: ALL, label: t(($) => $.log.all) },
    ...GOVERNANCE_EVENT_KINDS.map((kind) => ({ value: kind, label: t(($) => $.log.kind[kind]) })),
  ];
  const actorItems = [
    { value: ALL, label: t(($) => $.log.all) },
    ...GOVERNANCE_ACTOR_TYPES.map((type) => ({ value: type, label: t(($) => $.log.actor_type[type]) })),
  ];
  const subjectItems = [
    { value: ALL, label: t(($) => $.log.all) },
    ...GOVERNANCE_SUBJECT_TYPES.map((type) => ({ value: type, label: t(($) => $.log.subject_type[type]) })),
  ];


  const formatTime = (value: string) => {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "";
    return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(date);
  };

  // A member or agent is named from the directory; anything else reads as its type.
  const actorText = (event: GovernanceEvent) => {
    if ((event.actor_type === "member" || event.actor_type === "agent") && event.actor_id) {
      return getActorName(event.actor_type, event.actor_id);
    }
    return t(($) => $.log.actor_type[event.actor_type]);
  };

  // A subject type this client does not list shows as the server wrote it.
  const subjectText = (type: string) => {
    const known = knownSubjectType(type);
    return known ? t(($) => $.log.subject_type[known]) : type;
  };

  const exportLog = () => {
    exportCsv.mutate(filters, {
      onSuccess: (blob) => saveBlob(blob, `governance-log-${new Date().toISOString().slice(0, 10)}.csv`),
      onError: () => toast.error(t(($) => $.log.export_failed)),
    });
  };

  const filterSelect = (
    label: string,
    value: string,
    items: { value: string; label: string }[],
    name: "kind" | "actor_type" | "subject_type",
    width: string,
  ) => (
    <Select items={items} value={value} onValueChange={(next) => setFilter(name, next ?? ALL)}>
      <SelectTrigger aria-label={label} className={width}>
        <SelectValue>{items.find((item) => item.value === value)?.label ?? label}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        {items.map((item) => (
          <SelectItem key={item.value} value={item.value}>
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );

  return (
    <div className={cn(PAGE_GUTTER, "flex flex-col gap-3 py-4")}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          {filterSelect(t(($) => $.log.filter_kind), filters.kind ?? ALL, kindItems, "kind", "w-56")}
          {filterSelect(t(($) => $.log.filter_actor), filters.actor_type ?? ALL, actorItems, "actor_type", "w-40")}
          {filterSelect(t(($) => $.log.filter_subject), filters.subject_type ?? ALL, subjectItems, "subject_type", "w-40")}
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={exportCsv.isPending}
          aria-busy={exportCsv.isPending}
          onClick={exportLog}
        >
          <Download />
          {t(($) => $.log.export)}
        </Button>
      </div>

      {log.isError ? (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.log.load_failed)}
        </p>
      ) : null}

      {log.isSuccess && events.length === 0 ? (
        <p className="py-8 text-center text-body text-muted-foreground">{t(($) => $.log.empty)}</p>
      ) : null}

      {events.length > 0 ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t(($) => $.log.column_kind)}</TableHead>
              <TableHead>{t(($) => $.log.column_actor)}</TableHead>
              <TableHead>{t(($) => $.log.column_subject)}</TableHead>
              <TableHead>{t(($) => $.log.column_summary)}</TableHead>
              <TableHead>{t(($) => $.log.column_time)}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {events.map((event) => (
              <TableRow key={event.id}>
                <TableCell className="whitespace-nowrap">{t(($) => $.log.kind[event.kind])}</TableCell>
                <TableCell className="whitespace-nowrap">{actorText(event)}</TableCell>
                <TableCell className="whitespace-nowrap">{subjectText(event.subject_type)}</TableCell>
                <TableCell className="min-w-64 whitespace-normal break-words">{event.summary}</TableCell>
                <TableCell className="whitespace-nowrap text-muted-foreground">{formatTime(event.created_at)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : null}

      {log.hasNextPage ? (
        <div className="flex justify-center">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={log.isFetchingNextPage}
            aria-busy={log.isFetchingNextPage}
            onClick={() => void log.fetchNextPage()}
          >
            {t(($) => $.log.load_more)}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
