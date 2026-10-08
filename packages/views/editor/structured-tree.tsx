"use client";

/**
 * StructuredTree — parsed JSON / YAML as a collapsible tree, for the viewer's
 * `structured` kind.
 *
 * Every object and array is a disclosure: its row toggles it, and a collapsed
 * one previews its first entries inline (`{ id: 1, name: "a", … }`) so a list
 * of records can be scanned without opening each. The root is always open.
 * One more level opens by default under an object root — a config reads
 * top-down — but not under an array root, where the previews are the view.
 *
 * Large collections render their children a page at a time, so a 50 000-item
 * array costs what its first page costs.
 */

import { useState, type ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import "./styles/code.css";

const PAGE_SIZE = 100;
// Children count up to which a second-level object opens on its own.
const AUTO_OPEN_LIMIT = 50;
const PREVIEW_BUDGET = 80;

type Container = Record<string, unknown> | unknown[];

function isContainer(value: unknown): value is Container {
  return typeof value === "object" && value !== null;
}

function entriesOf(value: Container): Array<[string, unknown]> {
  return Array.isArray(value)
    ? value.map((item, index) => [String(index), item])
    : Object.entries(value);
}

// A key that would read ambiguously bare (empty, spaces, punctuation) is
// quoted, the way a devtools console shows it.
function formatKey(key: string): string {
  return /^[\p{L}\p{N}_$-]+$/u.test(key) ? key : JSON.stringify(key);
}

function primitiveText(value: unknown): string {
  if (typeof value === "string") return JSON.stringify(value);
  if (value instanceof Date) return value.toISOString();
  return String(value);
}

function primitiveClass(value: unknown): string {
  if (typeof value === "string" || value instanceof Date) return "hljs-string";
  if (typeof value === "number" || typeof value === "bigint") return "hljs-number";
  return "hljs-literal";
}

// `{ id: 1, name: "a", … }` — nested containers shrink to `{…}` / `[…]`.
export function previewOf(value: Container): string {
  const isArray = Array.isArray(value);
  const parts: string[] = [];
  let used = 0;
  let truncated = false;
  for (const [key, item] of entriesOf(value)) {
    const shown = isContainer(item)
      ? Array.isArray(item)
        ? "[…]"
        : "{…}"
      : primitiveText(item);
    const part = isArray ? shown : `${formatKey(key)}: ${shown}`;
    if (parts.length > 0 && used + part.length > PREVIEW_BUDGET) {
      truncated = true;
      break;
    }
    parts.push(part);
    used += part.length + 2;
  }
  const body = parts.join(", ") + (truncated ? ", …" : "");
  return isArray ? `[${body}]` : `{ ${body} }`;
}

export function StructuredTree({ value }: { value: unknown }) {
  return (
    <div className="structured-tree font-mono text-label leading-6">
      {isContainer(value) ? (
        <Children value={value} depth={1} parentIsArray={Array.isArray(value)} />
      ) : (
        <Primitive value={value} />
      )}
    </div>
  );
}

function Children({
  value,
  depth,
  parentIsArray,
}: {
  value: Container;
  depth: number;
  parentIsArray: boolean;
}) {
  const { t } = useT("editor");
  const entries = entriesOf(value);
  const [shown, setShown] = useState(PAGE_SIZE);
  const remaining = entries.length - shown;

  return (
    <ul className={cn(depth > 1 && "ml-[7px] border-l border-border pl-3")}>
      {entries.slice(0, shown).map(([key, item]) => (
        <Node
          key={key}
          name={key}
          value={item}
          depth={depth}
          parentIsArray={parentIsArray}
        />
      ))}
      {remaining > 0 && (
        <li>
          <button
            type="button"
            className="ml-5 rounded-sm px-1 text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
            onClick={() => setShown((count) => count + PAGE_SIZE)}
          >
            {t(($) => $.attachment.tree_show_more, { count: remaining })}
          </button>
        </li>
      )}
    </ul>
  );
}

function Node({
  name,
  value,
  depth,
  parentIsArray,
}: {
  name: string;
  value: unknown;
  depth: number;
  parentIsArray: boolean;
}) {
  const label = (
    <span className={parentIsArray ? "text-muted-foreground" : "hljs-attr"}>
      {parentIsArray ? name : formatKey(name)}
    </span>
  );

  if (!isContainer(value)) {
    return (
      <li className="flex min-w-0 gap-1 pl-5">
        <span className="shrink-0">
          {label}
          <span className="text-muted-foreground">:</span>
        </span>
        <Primitive value={value} />
      </li>
    );
  }

  return (
    <ContainerNode
      label={label}
      value={value}
      depth={depth}
      defaultOpen={
        depth === 1 && !parentIsArray && entriesOf(value).length <= AUTO_OPEN_LIMIT
      }
    />
  );
}

function ContainerNode({
  label,
  value,
  depth,
  defaultOpen,
}: {
  label: ReactNode;
  value: Container;
  depth: number;
  defaultOpen: boolean;
}) {
  const { t } = useT("editor");
  const isArray = Array.isArray(value);
  const count = isArray ? value.length : Object.keys(value).length;
  const [open, setOpen] = useState(defaultOpen);

  if (count === 0) {
    return (
      <li className="pl-5">
        {label}
        <span className="text-muted-foreground">: {isArray ? "[]" : "{}"}</span>
      </li>
    );
  }

  return (
    <li>
      <button
        type="button"
        aria-expanded={open}
        className="flex w-full min-w-0 items-center gap-1 rounded-sm text-left hover:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
        onClick={() => setOpen((next) => !next)}
      >
        <ChevronRight
          aria-hidden
          className={cn(
            "size-4 shrink-0 text-muted-foreground transition-transform duration-100",
            open && "rotate-90",
          )}
        />
        <span className="shrink-0">
          {label}
          <span className="text-muted-foreground">:</span>
        </span>
        {open ? (
          <span className="shrink-0 text-muted-foreground">
            {isArray
              ? t(($) => $.attachment.tree_items, { count })
              : t(($) => $.attachment.tree_keys, { count })}
          </span>
        ) : (
          <span className="min-w-0 truncate text-muted-foreground">
            {previewOf(value)}
          </span>
        )}
      </button>
      {open && <Children value={value} depth={depth + 1} parentIsArray={isArray} />}
    </li>
  );
}

function Primitive({ value }: { value: unknown }) {
  return (
    <span className={cn("min-w-0 [overflow-wrap:anywhere]", primitiveClass(value))}>
      {primitiveText(value)}
    </span>
  );
}
