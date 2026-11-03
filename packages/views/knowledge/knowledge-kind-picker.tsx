"use client";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import type { KnowledgeKind } from "@multica/core/knowledge";
import { useT } from "../i18n";

/**
 * Picks one knowledge kind to narrow a search. The empty value means every kind.
 */
export function KnowledgeKindPicker({
  kinds,
  value,
  onValueChange,
}: {
  kinds: KnowledgeKind[];
  value: string;
  onValueChange: (key: string) => void;
}) {
  const { t } = useT("knowledge");
  const items = [
    { value: "", label: t(($) => $.taxonomy.kind.all) },
    ...kinds.map((kind) => ({ value: kind.key, label: kind.name || kind.key })),
  ];
  return (
    <Select items={items} value={value} onValueChange={(next) => onValueChange(next ?? "")}>
      <SelectTrigger aria-label={t(($) => $.taxonomy.kind.label)} className="w-48">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {items.map((item) => (
          <SelectItem key={item.value || "all"} value={item.value}>
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
