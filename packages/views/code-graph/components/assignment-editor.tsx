"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  codeGraphAssignmentChanges,
  type AssignmentOption,
} from "@multica/core/workspace/codegraph";
import {
  useCodeGraphAssignmentChanges,
  type CodeGraphAssignmentChange,
} from "@multica/core/workspace/codegraph";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

export function useGraphAssignmentWriter(wsId: string) {
  const user = useAuthStore((state) => state.user?.id);
  const members = useQuery(memberListOptions(wsId));
  const role = members.data?.find((member) => member.user_id === user)?.role;
  return role === "owner" || role === "admin";
}

export function AssignmentEditor({
  wsId,
  options,
  toChange,
  onDone,
  onBusyChange,
}: {
  wsId: string;
  options: AssignmentOption[];
  toChange: (id: string, assigned: boolean) => CodeGraphAssignmentChange;
  onDone: () => void;
  onBusyChange?: (busy: boolean) => void;
}) {
  const { t } = useT("layout");
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState(
    () =>
      new Set(
        options.filter((option) => option.assigned).map((option) => option.id),
      ),
  );
  const [touched, setTouched] = useState(() => new Set<string>());
  const [errors, setErrors] = useState<string[]>([]);
  const mutation = useCodeGraphAssignmentChanges(wsId);
  const changes = codeGraphAssignmentChanges(options, selected, touched);
  const visible = options
    .filter((option) => option.assigned || option.supported)
    .filter((option) =>
      option.label.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
    );
  async function save() {
    setErrors([]);
    onBusyChange?.(true);
    try {
      const failures = await mutation.mutateAsync(
        changes.map((change) => toChange(change.id, change.assigned)),
      );
      if (!failures.length) onDone();
      else
        setErrors(
          failures.map(
            (failure) =>
              `${
                options.find((option) => {
                  const candidate = toChange(
                    option.id,
                    failure.change.assigned,
                  );
                  return (
                    candidate.agentId === failure.change.agentId &&
                    candidate.repositoryId === failure.change.repositoryId
                  );
                })?.label ?? ""
              }: ${failure.error}`,
          ),
        );
    } catch (error) {
      setErrors([error instanceof Error ? error.message : String(error)]);
    } finally {
      onBusyChange?.(false);
    }
  }
  return (
    <div className="space-y-4">
      <Input
        aria-label={t(($) => $.code_graph.search)}
        placeholder={t(($) => $.code_graph.search)}
        value={search}
        disabled={mutation.isPending}
        onChange={(event) => setSearch(event.target.value)}
      />
      <div className="max-h-80 overflow-y-auto rounded-lg border divide-y">
        {visible.map((option) => (
          <label key={option.id} className="flex items-start gap-3 p-3">
            <input
              type="checkbox"
              className="mt-1"
              checked={
                touched.has(option.id)
                  ? selected.has(option.id)
                  : option.assigned
              }
              disabled={
                mutation.isPending ||
                !option.editable ||
                (!option.supported && !option.assigned)
              }
              onChange={(event) => {
                setTouched((previous) => new Set(previous).add(option.id));
                setSelected((previous) => {
                  const next = new Set(previous);
                  if (event.target.checked) next.add(option.id);
                  else next.delete(option.id);
                  return next;
                });
              }}
            />
            <span className="min-w-0 flex-1 break-words text-body">
              {option.label}
              {!option.supported && (
                <span className="block text-caption text-muted-foreground">
                  {t(($) => $.code_graph.assignment_external)}
                </span>
              )}
              {!option.editable && (
                <span className="block text-caption text-muted-foreground">
                  {t(($) => $.code_graph.assignment_readonly)}
                </span>
              )}
            </span>
          </label>
        ))}
        {!visible.length && (
          <p className="p-3 text-body text-muted-foreground">
            {t(($) => $.code_graph.assignment_empty)}
          </p>
        )}
      </div>
      {errors.length > 0 && (
        <div role="alert" className="space-y-1 text-caption text-destructive">
          {errors.map((error) => (
            <p key={error}>{error}</p>
          ))}
        </div>
      )}
      <div className="flex justify-end gap-2">
        <Button
          variant="outline"
          disabled={mutation.isPending}
          onClick={onDone}
        >
          {t(($) => $.code_graph.cancel)}
        </Button>
        <Button
          disabled={!changes.length || mutation.isPending}
          aria-busy={mutation.isPending}
          onClick={() => void save()}
        >
          {t(($) => $.code_graph.assignment_save)}
        </Button>
      </div>
    </div>
  );
}
