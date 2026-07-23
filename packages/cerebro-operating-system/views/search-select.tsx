"use client";

import { useEffect, useMemo, useRef, useState } from "react";

export interface SearchSelectOption {
  value: string;
  label: string;
  hint?: string;
  color?: string;
  group?: string;
}

interface SearchSelectBaseProps {
  label: string;
  options: SearchSelectOption[];
  placeholder?: string;
  disabled?: boolean;
  compact?: boolean;
  className?: string;
  /** Static action rendered at the bottom of the list, e.g. "Plan next period". */
  actionLabel?: string;
  onAction?: (query: string) => void;
}

interface SingleSearchSelectProps extends SearchSelectBaseProps {
  multiple?: false;
  value: string;
  onChange: (value: string) => void;
  clearLabel?: string;
}

interface MultiSearchSelectProps extends SearchSelectBaseProps {
  multiple: true;
  values: string[];
  onValuesChange: (values: string[]) => void;
}

export type SearchSelectProps = SingleSearchSelectProps | MultiSearchSelectProps;

export function SearchSelect(props: SearchSelectProps) {
  const { label, options, placeholder = "Select…", disabled, compact, className = "", actionLabel, onAction } = props;
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const rootRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    inputRef.current?.focus();
    function onPointerDown(event: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) {
        setOpen(false);
        setQuery("");
      }
    }
    document.addEventListener("mousedown", onPointerDown);
    return () => document.removeEventListener("mousedown", onPointerDown);
  }, [open]);

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return options;
    return options.filter((option) => `${option.label} ${option.hint ?? ""}`.toLowerCase().includes(needle));
  }, [options, query]);

  const groups = useMemo(() => {
    const map = new Map<string, SearchSelectOption[]>();
    for (const option of filtered) {
      const key = option.group ?? "";
      const bucket = map.get(key);
      if (bucket) bucket.push(option);
      else map.set(key, [option]);
    }
    return [...map.entries()];
  }, [filtered]);

  const selectedValues = props.multiple ? props.values : props.value ? [props.value] : [];
  const summary = selectedValues.map((value) => options.find((option) => option.value === value)?.label).filter(Boolean).join(", ");
  const summaryColor = !props.multiple && props.value ? options.find((option) => option.value === props.value)?.color : undefined;

  function close() {
    setOpen(false);
    setQuery("");
  }

  function pick(value: string) {
    if (props.multiple) {
      props.onValuesChange(props.values.includes(value) ? props.values.filter((existing) => existing !== value) : [...props.values, value]);
    } else {
      props.onChange(value);
      close();
    }
  }

  return (
    <div ref={rootRef} className={`relative min-w-0 ${className}`} onKeyDown={(event) => { if (event.key === "Escape") close(); }}>
      <button
        type="button"
        disabled={disabled}
        aria-label={label}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={(event) => { event.stopPropagation(); if (open) close(); else setOpen(true); }}
        className={`flex w-full items-center justify-between gap-1.5 rounded-md border bg-background text-left disabled:opacity-50 ${compact ? "h-8 px-2 text-xs" : "h-10 px-3 text-sm"}`}
      >
        <span className={`flex min-w-0 items-center gap-1.5 ${summary ? "" : "text-muted-foreground"}`}>
          {summaryColor && <span aria-hidden className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: summaryColor }} />}
          <span className="truncate">{summary || placeholder}</span>
        </span>
        <span aria-hidden className="shrink-0 text-muted-foreground">▾</span>
      </button>
      {open && (
        <div className="absolute left-0 top-full z-30 mt-1 w-full min-w-56 rounded-md border bg-popover p-1 shadow-md" onClick={(event) => event.stopPropagation()}>
          <input
            ref={inputRef}
            aria-label={`Search ${label}`}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Type to search…"
            className="mb-1 h-8 w-full rounded border bg-background px-2 text-sm"
          />
          <div role="listbox" aria-label={`${label} options`} aria-multiselectable={props.multiple || undefined} className="max-h-56 overflow-y-auto">
            {!props.multiple && props.clearLabel && (
              <button type="button" role="option" aria-selected={!props.value} onClick={() => pick("")} className={`flex w-full items-center rounded px-2 py-1.5 text-left text-sm text-muted-foreground hover:bg-muted ${!props.value ? "bg-muted/60" : ""}`}>
                {props.clearLabel}
              </button>
            )}
            {groups.map(([group, groupOptions]) => (
              <div key={group || "default"}>
                {group && <p className="px-2 pb-1 pt-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{group}</p>}
                {groupOptions.map((option) => {
                  const selected = selectedValues.includes(option.value);
                  return (
                    <button key={option.value} type="button" role="option" aria-selected={selected} onClick={() => pick(option.value)} className={`flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-muted ${selected ? "bg-muted/60 font-medium" : ""}`}>
                      {option.color && <span aria-hidden className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: option.color }} />}
                      <span className="min-w-0 flex-1 truncate">{option.label}</span>
                      {option.hint && <span className="shrink-0 text-xs text-muted-foreground">{option.hint}</span>}
                      {selected && <span aria-hidden className="shrink-0 text-primary">✓</span>}
                    </button>
                  );
                })}
              </div>
            ))}
            {filtered.length === 0 && <p className="px-2 py-3 text-sm text-muted-foreground">No matches.</p>}
          </div>
          {onAction && (
            <button type="button" onClick={() => { onAction(query.trim()); close(); }} className="mt-1 flex w-full items-center gap-1.5 rounded border-t px-2 py-1.5 text-left text-sm font-medium text-primary hover:bg-muted">
              ＋ {actionLabel ?? `Create "${query.trim()}"`}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
