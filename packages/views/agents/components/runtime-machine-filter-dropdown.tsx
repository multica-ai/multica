import { useMemo } from "react";
import { ChevronDown, Server } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import type { RuntimeMachine, RuntimeMachineSection } from "../../runtimes/components/runtime-machines";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// Runtime machine filter — dropdown next to the search input. The trigger
// shows the active machine's title (or "All runtimes"); the menu groups
// machines by section (Local / Remote / Cloud) the same way the
// Runtimes page sidebar does, so a user moving between the two pages
// sees consistent labels and counts.
// ---------------------------------------------------------------------------

const RUNTIME_MACHINE_SECTIONS: RuntimeMachineSection[] = [
  "local",
  "remote",
  "cloud",
];

export function RuntimeMachineFilterDropdown({
  machines,
  value,
  onChange,
  agentCountByMachine,
  // Sourced separately from the in-scope agent list (not derived from
  // `agentCountByMachine`) so the "All runtimes" badge stays accurate
  // even when an in-scope agent is bound to a runtime that's been GC'd
  // and no longer shows up under any current machine.
  totalAgentCount,
}: {
  machines: RuntimeMachine[];
  value: string | null;
  onChange: (id: string | null) => void;
  agentCountByMachine: Map<string, number>;
  totalAgentCount: number;
}) {
  const { t } = useT("agents");
  const selected =
    value === null
      ? null
      : machines.find((machine) => machine.id === value) ?? null;

  const triggerLabel = selected ? selected.title : t(($) => $.runtime_filter.all);
  // Always show a count, even when the trigger is "All runtimes" — keeps
  // the affordance scannable next to the other toolbar controls.
  const triggerCount = selected
    ? (agentCountByMachine.get(selected.id) ?? 0)
    : totalAgentCount;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            className="h-8 gap-1.5 px-2 text-xs"
            data-testid="agents-runtime-filter"
          />
        }
      >
        <Server className="h-3 w-3 text-muted-foreground" />
        <span className="max-w-[12rem] truncate">{triggerLabel}</span>
        <span className="font-mono tabular-nums text-muted-foreground/70">
          {triggerCount}
        </span>
        <ChevronDown className="h-3 w-3 text-muted-foreground/60" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72 p-0">
        <RuntimeMachineFilterMenu
          machines={machines}
          value={value}
          onChange={onChange}
          totalAgentCount={totalAgentCount}
          agentCountByMachine={agentCountByMachine}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function RuntimeMachineFilterMenu({
  machines,
  value,
  onChange,
  totalAgentCount,
  agentCountByMachine,
}: {
  machines: RuntimeMachine[];
  value: string | null;
  onChange: (id: string | null) => void;
  totalAgentCount: number;
  agentCountByMachine: Map<string, number>;
}) {
  const { t } = useT("agents");
  // Group machines by section while preserving the order
  // `buildRuntimeMachines` already sorts them by (section, online count,
  // title). We iterate the section list and slice to keep that order.
  const grouped = useMemo(() => {
    const result: Array<{
      section: RuntimeMachineSection;
      machines: RuntimeMachine[];
    }> = [];
    for (const section of RUNTIME_MACHINE_SECTIONS) {
      const inSection = machines.filter((machine) => machine.section === section);
      if (inSection.length > 0) result.push({ section, machines: inSection });
    }
    return result;
  }, [machines]);

  return (
    <div className="max-h-80 overflow-y-auto py-1">
      <RuntimeMachineFilterItem
        active={value === null}
        onClick={() => onChange(null)}
        label={t(($) => $.runtime_filter.all)}
        count={totalAgentCount}
      />
      {grouped.map((group) => (
        <div key={group.section}>
          <div className="flex items-center gap-2 px-3 pb-1 pt-3 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
            <span>{t(($) => $.runtime_filter[`section_${group.section}`])}</span>
            <span className="h-px flex-1 bg-border" />
          </div>
          {group.machines.map((machine) => (
            <RuntimeMachineFilterItem
              key={machine.id}
              active={value === machine.id}
              onClick={() => onChange(machine.id)}
              label={machine.title}
              subtitle={machine.subtitle}
              count={agentCountByMachine.get(machine.id) ?? 0}
            />
          ))}
        </div>
      ))}
      {machines.length === 0 && (
        <div className="px-3 py-2 text-xs text-muted-foreground">
          {t(($) => $.runtime_filter.empty)}
        </div>
      )}
    </div>
  );
}

function RuntimeMachineFilterItem({
  active,
  onClick,
  label,
  subtitle,
  count,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  subtitle?: string | null;
  count: number;
}) {
  const { t } = useT("agents");
  // DropdownMenuItem (Base UI Menu.Item) wires the row into the menu's
  // keyboard navigation, typeahead, and ARIA role="menuitem" semantics,
  // and auto-closes the menu on selection (closeOnClick: true). The
  // visual treatment — selected vs. idle — is layered on top via
  // `data-active` so it survives focus/hover styling from the base.
  return (
    <DropdownMenuItem
      onClick={onClick}
      data-active={active || undefined}
      data-testid={active ? "agents-runtime-filter-active" : undefined}
      className={`flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors ${
        active
          ? "bg-accent text-accent-foreground"
          : "text-foreground hover:bg-muted/60 data-highlighted:bg-muted/60"
      }`}
    >
      <span className="min-w-0 flex-1 truncate">
        <span className="block truncate font-medium">{label}</span>
        {subtitle && (
          <span className="block truncate text-[11px] font-normal text-muted-foreground">
            {subtitle}
          </span>
        )}
      </span>
      <span className="font-mono tabular-nums text-muted-foreground/70">
        {t(($) => $.runtime_filter.agent_count, { count })}
      </span>
    </DropdownMenuItem>
  );
}
