"use client";

import { useId } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { qoderAgentsOptions } from "@multica/core/runtimes/qoder";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";

export function QoderAgentSelect({
  value,
  onChange,
  disabled = false,
}: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const wsId = useWorkspaceId();
  const { t } = useT("runtimes");
  const labelId = useId();
  const query = useQuery(qoderAgentsOptions(wsId));
  const items = query.data ?? [];
  const missing =
    !!value && query.isSuccess && !items.some((item) => item.id === value);
  return (
    <div className="space-y-2">
      <p id={labelId} className="text-caption font-medium">
        {t(($) => $.qoder.agent)}
      </p>
      <Select
        items={items.map((item) => ({ value: item.id, label: item.name }))}
        value={value || null}
        onValueChange={(next) => onChange(next ?? "")}
        disabled={disabled || query.isPending || !items.length}
      >
        <SelectTrigger className="w-full" aria-labelledby={labelId}>
          <SelectValue placeholder={t(($) => $.qoder.select_agent)} />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          {items.map((item) => (
            <SelectItem key={item.id} value={item.id}>
              {item.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.qoder.agent_hint)}
      </p>
      {query.isPending && (
        <p role="status" className="text-caption text-muted-foreground">
          {t(($) => $.qoder.loading)}
        </p>
      )}
      {query.isError && (
        <p role="alert" className="text-caption text-destructive">
          {query.error.message}
        </p>
      )}
      {query.isSuccess && !items.length && (
        <p role="status" className="text-caption text-muted-foreground">
          {t(($) => $.qoder.no_agents)}
        </p>
      )}
      {missing && (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.qoder.agent_unavailable)}
        </p>
      )}
      <Button
        type="button"
        variant="ghost"
        size="sm"
        disabled={disabled || query.isFetching}
        onClick={() => void query.refetch()}
      >
        {t(($) => $.qoder.refresh_catalog)}
      </Button>
    </div>
  );
}
