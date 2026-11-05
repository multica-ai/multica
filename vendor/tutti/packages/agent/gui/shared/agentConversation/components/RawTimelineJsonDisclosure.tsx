import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { ChevronDown, ChevronRight } from "lucide-react";
import { observer } from "mobx-react-lite";
import { useState, type JSX } from "react";
import type { WorkspaceAgentActivityTimelineItem } from "../../workspaceAgentTimelineTypes";
import { CollapsibleReveal } from "./CollapsibleReveal";

interface RawTimelineJsonDisclosureProps {
  items: readonly WorkspaceAgentActivityTimelineItem[] | null | undefined;
  label: string;
}

export const RawTimelineJsonDisclosure = observer(
  function RawTimelineJsonDisclosure({
    items,
    label
  }: RawTimelineJsonDisclosureProps): JSX.Element | null {
    "use memo";
    const [expanded, setExpanded] = useState(false);
    const json = rawTimelineJson(items);
    if (!json) {
      return null;
    }

    return (
      <div className="mt-2 rounded-md border border-[color:color-mix(in_srgb,var(--state-danger)_18%,transparent)] bg-[color:color-mix(in_srgb,var(--background-fronted)_92%,var(--state-danger))] px-2 py-1.5 text-[11px] text-[var(--state-danger)]">
        <ButtonBinding1
          expanded={expanded}
          setExpanded={setExpanded}
          label={label}
        />
        <CollapsibleReveal expanded={expanded} preMountOnIdle>
          <pre className="mt-2 max-h-[320px] overflow-auto whitespace-pre-wrap break-words rounded-md border border-[var(--line-2)] bg-[var(--background-fronted)] px-3 py-2 font-mono text-[11px] leading-5 text-[var(--text-primary)]">
            <code>{json}</code>
          </pre>
        </CollapsibleReveal>
      </div>
    );
  }
);

function rawTimelineJson(
  items: readonly WorkspaceAgentActivityTimelineItem[] | null | undefined
): string | null {
  if (!items || items.length === 0) {
    return null;
  }
  try {
    return JSON.stringify(items.length === 1 ? items[0] : items, null, 2);
  } catch {
    return null;
  }
}

const ButtonBinding1 = observer(function ButtonBinding1({
  expanded,
  setExpanded,
  label,
  ...slotProps
}: {
  expanded: boolean;
  setExpanded: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
  label: NonNullable<RawTimelineJsonDisclosureProps>["label"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    expanded: boolean;
    setExpanded: import("react").Dispatch<
      import("react").SetStateAction<boolean>
    >;
    label: NonNullable<RawTimelineJsonDisclosureProps>["label"];
  }
>) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => setExpanded((value) => !value));
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        className="inline-flex items-center gap-1.5 border-0 bg-transparent p-0 text-left font-semibold text-[var(--state-danger)]"
        aria-expanded={expanded}
        onClick={onClickProp1}
      >
        {expanded ? (
          <ChevronDown size={13} strokeWidth={2.2} aria-hidden />
        ) : (
          <ChevronRight size={13} strokeWidth={2.2} aria-hidden />
        )}
        <span>{label}</span>
      </button>
    </Slot.Root>
  );
});
