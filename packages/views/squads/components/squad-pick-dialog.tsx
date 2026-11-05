"use client";

import { useMemo, useState } from "react";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Search } from "@multica/ui/icons";

export interface PickItem {
  key: string;
  name: string;
  description?: string;
  /** Install source, for plugins. */
  hint?: string;
}

export interface ManualEntryCopy {
  nameLabel: string;
  namePlaceholder: string;
  hintLabel: string;
  hintPlaceholder: string;
  addLabel: string;
  duplicate: string;
}

/**
 * Pick several skills or plugins from a list. A search box narrows the list,
 * and the select-all box then acts on what is shown: the filtered rows when a
 * search is active, every row when it is not. `manual` adds a free-typed entry
 * for anything the list does not offer.
 */
export function SquadPickDialog({
  title,
  items,
  searchPlaceholder,
  emptyMessage,
  noMatchMessage,
  selectAllLabel,
  confirmLabel,
  cancelLabel,
  saving,
  manual,
  onClose,
  onConfirm,
}: {
  title: string;
  items: PickItem[];
  searchPlaceholder: string;
  emptyMessage: string;
  noMatchMessage: string;
  selectAllLabel: (count: number) => string;
  confirmLabel: (count: number) => string;
  cancelLabel: string;
  saving: boolean;
  manual?: ManualEntryCopy;
  onClose: () => void;
  onConfirm: (picked: PickItem[]) => Promise<void>;
}) {
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [manualItems, setManualItems] = useState<PickItem[]>([]);
  const [manualName, setManualName] = useState("");
  const [manualHint, setManualHint] = useState("");
  const [manualError, setManualError] = useState<string | null>(null);

  const allItems = useMemo(() => [...items, ...manualItems], [items, manualItems]);
  const trimmed = query.trim().toLowerCase();
  const visible = useMemo(
    () =>
      trimmed
        ? allItems.filter((item) =>
            [item.name, item.description, item.hint]
              .some((text) => text?.toLowerCase().includes(trimmed)),
          )
        : allItems,
    [allItems, trimmed],
  );

  const visibleSelected = visible.filter((item) => selected.has(item.key)).length;
  const allVisibleSelected = visible.length > 0 && visibleSelected === visible.length;
  const someVisibleSelected = visibleSelected > 0 && !allVisibleSelected;

  const toggle = (key: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  // Acts only on the rows shown, so a search followed by select-all picks the
  // matches and nothing else.
  const toggleAllVisible = (checked: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev);
      for (const item of visible) {
        if (checked) next.add(item.key);
        else next.delete(item.key);
      }
      return next;
    });

  const addManual = () => {
    const name = manualName.trim();
    if (!name) return;
    if (allItems.some((item) => item.key === name)) {
      setManualError(manual?.duplicate ?? "");
      return;
    }
    const hint = manualHint.trim();
    setManualItems((prev) => [...prev, { key: name, name, hint: hint || undefined }]);
    setSelected((prev) => new Set(prev).add(name));
    setManualName("");
    setManualHint("");
    setManualError(null);
  };

  const picked = allItems.filter((item) => selected.has(item.key));

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !saving) onClose(); }}>
      {/* The grid column must be bounded: an auto track sizes to the longest
          unbroken skill or plugin name and pushes the dialog into a horizontal scroll. */}
      <DialogContent className="max-w-md grid-cols-[minmax(0,1fr)]">
        <DialogHeader>
          <DialogTitle className="text-body">{title}</DialogTitle>
        </DialogHeader>

        <div className="min-w-0 space-y-3">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              placeholder={searchPlaceholder}
              onChange={(e) => setQuery(e.target.value)}
              className="pl-8"
              aria-label={searchPlaceholder}
            />
          </div>

          {allItems.length === 0 ? (
            <p className="px-1 text-caption text-muted-foreground">{emptyMessage}</p>
          ) : visible.length === 0 ? (
            <p className="px-1 text-caption text-muted-foreground">{noMatchMessage}</p>
          ) : (
            <>
              <label className="flex min-w-0 cursor-pointer items-center gap-2 px-1 text-caption font-medium">
                <Checkbox
                  checked={allVisibleSelected}
                  indeterminate={someVisibleSelected}
                  onCheckedChange={(checked) => toggleAllVisible(checked)}
                />
                {selectAllLabel(visible.length)}
              </label>
              <ul className="max-h-72 divide-y overflow-y-auto rounded-lg border">
                {visible.map((item) => (
                  <li key={item.key}>
                    <label className="flex min-w-0 cursor-pointer items-center gap-3 px-3 py-2">
                      <Checkbox
                        checked={selected.has(item.key)}
                        onCheckedChange={() => toggle(item.key)}
                      />
                      <span className="flex min-w-0 flex-1 flex-col">
                        <span className="truncate text-body font-medium">{item.name}</span>
                        {(item.description || item.hint) && (
                          <span className="truncate text-caption text-muted-foreground">
                            {item.description || item.hint}
                          </span>
                        )}
                      </span>
                    </label>
                  </li>
                ))}
              </ul>
            </>
          )}

          {manual && (
            <div className="space-y-2 border-t pt-3">
              <div className="grid min-w-0 grid-cols-2 gap-2">
                <div className="space-y-1">
                  <Label htmlFor="squad-pick-manual-name" className="text-caption">{manual.nameLabel}</Label>
                  <Input
                    id="squad-pick-manual-name"
                    value={manualName}
                    placeholder={manual.namePlaceholder}
                    onChange={(e) => { setManualName(e.target.value); setManualError(null); }}
                  />
                </div>
                <div className="space-y-1">
                  <Label htmlFor="squad-pick-manual-hint" className="text-caption">{manual.hintLabel}</Label>
                  <Input
                    id="squad-pick-manual-hint"
                    value={manualHint}
                    placeholder={manual.hintPlaceholder}
                    onChange={(e) => setManualHint(e.target.value)}
                  />
                </div>
              </div>
              {manualError && <p className="text-caption text-destructive">{manualError}</p>}
              <div className="flex justify-end">
                <Button variant="outline" size="sm" onClick={addManual} disabled={!manualName.trim() || saving}>
                  {manual.addLabel}
                </Button>
              </div>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={saving}>
            {cancelLabel}
          </Button>
          <Button
            disabled={picked.length === 0 || saving}
            onClick={() => void onConfirm(picked).catch(() => {})}
          >
            {confirmLabel(picked.length)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
