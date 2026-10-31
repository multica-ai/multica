"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Input } from "@multica/ui/components/ui/input";
import { DropdownMenuCheckboxItem } from "@multica/ui/components/ui/dropdown-menu";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  formatActorRef,
  isActorPropertyType,
  parseActorRef,
  isListPropertyType,
  isScalarPropertyType,
  MAX_ISSUE_PROPERTY_FILTER_VALUES,
  PROPERTY_FILTER_OP_SYMBOLS,
  PROPERTY_FILTER_OPS_BY_TYPE,
  type IssueProperty,
  type PropertyFilterOp,
  type PropertyFilterValue,
} from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { useT } from "../../i18n";
import type { ActorFilterValue } from "@multica/core/issues/stores/view-store";
import { NO_PROPERTY_VALUE } from "../utils/filter";

/**
 * Names one property filter value the way chips and advanced conditions show
 * it: the option's name, Yes/No, an operator phrase, or the actor's name.
 */
export function usePropertyValueName(
  actorName: (actor: ActorFilterValue) => string | undefined,
) {
  const { t } = useT("issues");
  return (definition: IssueProperty, member: PropertyFilterValue): string | undefined => {
    if (member === NO_PROPERTY_VALUE) {
      return t(($) => $.pickers.custom_property.none);
    }
    // Scalar operator members summarize with their operator; the comparison
    // symbols are locale-independent, the word ops get chip phrases.
    if (typeof member === "object") {
      if (member.op === "contains") {
        return t(($) => $.filters.chip_op_contains, { value: member.value });
      }
      if (member.op === "before") {
        return t(($) => $.filters.chip_op_before, { value: member.value });
      }
      if (member.op === "after") {
        return t(($) => $.filters.chip_op_after, { value: member.value });
      }
      const symbol = PROPERTY_FILTER_OP_SYMBOLS[member.op] ?? member.op;
      return `${symbol} ${member.value}`;
    }
    if (isActorPropertyType(definition.type)) {
      const ref = parseActorRef(member);
      return ref ? actorName({ type: ref.kind, id: ref.id }) : undefined;
    }
    if (definition.type === "checkbox") {
      return member === "true"
        ? t(($) => $.pickers.custom_property.true_label)
        : t(($) => $.pickers.custom_property.false_label);
    }
    // Scalar and list properties have no option list — the filter value IS
    // the label (for lists it is one matched element).
    if (isScalarPropertyType(definition.type) || isListPropertyType(definition.type)) {
      return member;
    }
    return definition.config.options?.find((o) => o.id === member)?.name;
  };
}

/**
 * Option checkboxes for one custom property inside the Filter dropdown.
 * Select/multi_select list the definition's options (dot + name); checkbox
 * definitions expose the "true"/"false" pseudo-options with Yes/No labels;
 * actor definitions have no config options at all, so their candidates come
 * from the member directory instead, with the signed-in member first so
 * "this property is me" stays one click away.
 */

// Keyboard guard for the inline operator radios — same contract as the
// scalar input below: Escape/Tab belong to the menu (close / move focus);
// every other navigation or selection key must not bubble into the popup's
// typeahead / list-navigation handlers.
function stopScalarMenuKeys(event: React.KeyboardEvent) {
  if (event.key === "Escape" || event.key === "Tab") return;
  event.stopPropagation();
}
export function PropertyFilterOptions({
  property,
  counts,
  selected,
  onToggle,
  onSetValues,
  fixedIds,
  fixedTitle,
}: {
  property: IssueProperty;
  counts: Map<string, number> | undefined;
  selected: PropertyFilterValue[];
  onToggle: (optionId: string) => void;
  /** Replace the property's full filter value set (scalar types only). */
  onSetValues: (optionIds: PropertyFilterValue[]) => void;
  fixedIds?: Set<string>;
  fixedTitle?: string;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const currentUserId = useAuthStore((s) => s.user?.id);
  const actorProperty = isActorPropertyType(property.type);
  // Scalar properties (text / number / date / url) have no option list — the
  // filter menu shows a value input plus "No value".
  const { data: actorMembers = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: actorProperty,
  });

  const actorOptions = useMemo(() => {
    if (!actorProperty) return [];
    return actorMembers
      .slice()
      .sort((a, b) => {
        if (a.user_id === currentUserId) return -1;
        if (b.user_id === currentUserId) return 1;
        return 0;
      })
      .map((m) => ({
        id: formatActorRef("member", m.user_id),
        name: m.name,
        actorType: "member" as const,
        actorId: m.user_id,
      }));
  }, [actorProperty, actorMembers, currentUserId]);

  // "No value" is offered for every property type: it selects issues where the
  // property is unset, the inverse of picking one of the options below.
  const noValueOption = {
    id: NO_PROPERTY_VALUE,
    name: t(($) => $.pickers.custom_property.none),
    color: undefined as string | undefined,
    actorType: undefined as string | undefined,
    actorId: undefined as string | undefined,
  };
  // Scalar value state lives at the top level so the hooks stay unconditional
  // (Rules of Hooks): it is only rendered for text / number / date / url and
  // the list types (equality-only input), but must be declared regardless of
  // which branch runs. The draft syncs to the committed scalar member whenever
  // that changes, so a filter cleared or rewritten elsewhere cannot be written
  // back from a stale input.
  const committedMember = selected.find((member) => member !== NO_PROPERTY_VALUE);
  const committedScalar =
    typeof committedMember === "object" ? committedMember.value : (committedMember ?? "");
  const committedOp: PropertyFilterOp | "is" =
    typeof committedMember === "object" ? committedMember.op : "is";
  const hasNoValue = selected.includes(NO_PROPERTY_VALUE);
  const [draft, setDraft] = useState(committedScalar);
  useEffect(() => setDraft(committedScalar), [committedScalar]);
  // Operator picked in the open menu but not yet committed; null defers to the
  // committed member (equality when there is none). Resets whenever the
  // committed member changes — including right after this menu commits — so a
  // value rewritten elsewhere can't inherit a stale operator.
  const [pendingOp, setPendingOp] = useState<PropertyFilterOp | "is" | null>(null);
  const committedKey =
    committedMember === undefined
      ? ""
      : typeof committedMember === "object"
        ? `op:${committedMember.op}:${committedMember.value}`
        : `eq:${committedMember}`;
  useEffect(() => setPendingOp(null), [committedKey]);
  const options = [
    ...(actorProperty
      ? actorOptions.map((option) => ({
          id: option.id,
          name: option.name,
          color: undefined as string | undefined,
          actorType: option.actorType as string | undefined,
          actorId: option.actorId as string | undefined,
        }))
      : property.type === "checkbox"
        ? [
            { id: "true", name: t(($) => $.pickers.custom_property.true_label), color: undefined, actorType: undefined, actorId: undefined },
            { id: "false", name: t(($) => $.pickers.custom_property.false_label), color: undefined, actorType: undefined, actorId: undefined },
          ]
        : (property.config.options ?? []).map((option) => ({
            id: option.id,
            name: option.name,
            color: option.color as string | undefined,
            actorType: undefined as string | undefined,
            actorId: undefined as string | undefined,
          }))),
    noValueOption,
  ];

  if (isScalarPropertyType(property.type) || isListPropertyType(property.type)) {
    const placeholder =
      property.type === "url" || property.type === "multi_url"
        ? t(($) => $.pickers.custom_property.url_placeholder)
        : property.type === "number"
          ? t(($) => $.pickers.custom_property.number_placeholder)
          : t(($) => $.pickers.custom_property.value_placeholder);
    const noneCount = counts?.get(NO_PROPERTY_VALUE) ?? 0;
    // A saved view locks this dimension: the value (with its operator) AND
    // "No value" are both part of the view's identity, so neither can be
    // edited in place.
    const locked = fixedIds !== undefined && fixedIds.size > 0;
    // "is" is equality and commits a bare string — the pre-operator shape.
    // Every other op commits an operator object; the server and matcher both
    // treat unknown shapes conservatively.
    const effectiveOp: PropertyFilterOp | "is" = pendingOp ?? committedOp;
    const scalarOperatorLabel = (op: PropertyFilterOp): string => {
      if (op === "contains") return t(($) => $.pickers.custom_property.op_contains);
      if (op === "before") return t(($) => $.pickers.custom_property.op_before);
      if (op === "after") return t(($) => $.pickers.custom_property.op_after);
      return PROPERTY_FILTER_OP_SYMBOLS[op] ?? op;
    };
    // List types are equality-only: their PROPERTY_FILTER_OPS_BY_TYPE entry is
    // empty by design, and looking it up needs the scalar narrowing anyway
    // (IssueProperty.type is a lenient string).
    const scalarOps = isScalarPropertyType(property.type)
      ? (PROPERTY_FILTER_OPS_BY_TYPE[property.type] ?? [])
      : [];
    const opButtons: { op: PropertyFilterOp | "is"; label: string }[] = [
      {
        op: "is",
        label:
          property.type === "number"
            ? "="
            : t(($) => $.pickers.custom_property.op_is),
      },
      ...scalarOps.map((op) => ({
        op,
        label: scalarOperatorLabel(op),
      })),
    ];
    const commitValue = (raw: string, op: PropertyFilterOp | "is" = effectiveOp) => {
      const value = raw.trim();
      // NO_PROPERTY_VALUE is the reserved "no value" sentinel — it cannot be
      // filtered as a literal value. The value and "No value" compose like
      // every other property type: committing a value replaces only the value
      // member and preserves "No value" membership.
      if (value === NO_PROPERTY_VALUE) return;
      const member: PropertyFilterValue | undefined = value
        ? op === "is"
          ? value
          : { op, value }
        : undefined;
      onSetValues([
        ...(member ? [member] : []),
        ...(hasNoValue ? [NO_PROPERTY_VALUE] : []),
      ]);
    };
    const applyOp = (op: PropertyFilterOp | "is") => {
      // An explicit click commits the current draft with the chosen op, unlike
      // the input which waits for Enter/blur. With an empty draft nothing
      // commits, so the choice rides on pendingOp and the next Enter/blur
      // commit carries it — killing pendingOp here would silently downgrade
      // that later commit back to equality.
      setPendingOp(op);
      commitValue(draft, op);
    };
    return (
      <>
        {opButtons.length > 1 && (
          <div
            role="radiogroup"
            aria-label={t(($) => $.pickers.custom_property.operator_label)}
            className="flex flex-wrap gap-1 px-2 pt-1.5"
          >
            {opButtons.map(({ op, label }) => {
              const active = effectiveOp === op;
              return (
                <label
                  key={op}
                  className={locked ? "cursor-not-allowed" : "cursor-pointer"}
                >
                  <input
                    type="radio"
                    name={`property-filter-op-${property.id}`}
                    value={op}
                    checked={active}
                    disabled={locked}
                    onChange={() => applyOp(op)}
                    onKeyDown={stopScalarMenuKeys}
                    className="peer sr-only"
                  />
                  <span
                    className={`inline-flex h-6 items-center rounded-md px-1.5 text-caption transition-colors peer-focus-visible:outline-none peer-focus-visible:ring-2 peer-focus-visible:ring-ring ${
                      active
                        ? "bg-accent font-medium text-foreground"
                        : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
                    }`}
                  >
                    {label}
                  </span>
                </label>
              );
            })}
          </div>
        )}
        <div className="px-2 py-1.5">
          <Input
            type={property.type === "number" ? "number" : property.type === "date" ? "date" : "text"}
            step={property.type === "number" ? "any" : undefined}
            inputMode={property.type === "number" ? "decimal" : undefined}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onBlur={(event) => {
              // Clicking the "No value" menu item moves focus off the input,
              // firing blur first. That premature commit would flip the
              // checkbox's controlled state before its click handler reads it
              // — so when focus moves into a menu item in this popup, skip the
              // blur commit and let onCheckedChange decide from the pre-blur
              // state.
              const next = event.relatedTarget;
              const movedToMenuItem =
                next instanceof HTMLElement &&
                next.closest('[role="menuitemcheckbox"]') !== null;
              if (!movedToMenuItem) commitValue(draft);
            }}
            onKeyDown={(event) => {
              // Base UI's menu popup merges typeahead + list-navigation handlers
              // onto the role="menu" element and stopEvent()s every printable
              // key — without stopping propagation, the input would swallow no
              // characters at all (and arrow keys on number/date inputs would
              // be hijacked). Escape/Tab are left for the menu to close/move
              // focus; Enter commits and still needs stopPropagation so it does
              // not bubble up and activate the highlighted "No value" item.
              if (event.key === "Escape" || event.key === "Tab") return;
              if (event.key === "Enter") commitValue(draft);
              event.stopPropagation();
            }}
            disabled={locked}
            placeholder={placeholder}
            className="h-8"
          />
        </div>
        <DropdownMenuCheckboxItem
          checked={hasNoValue}
          disabled={locked}
          onCheckedChange={(checked) => {
            // "No value" toggles membership in the same OR-set as the value —
            // checking/unchecking never touches the committed value member, so
            // unchecking restores it without any draft round-trip.
            const valueMembers = selected.filter((id) => id !== NO_PROPERTY_VALUE);
            onSetValues(
              checked
                ? [...valueMembers, NO_PROPERTY_VALUE]
                : valueMembers,
            );
          }}
          className={FILTER_ITEM_CLASS}
        >
          <HoverCheck checked={hasNoValue} />
          <span className="truncate">{noValueOption.name}</span>
          {noneCount > 0 && (
            <span className="ml-auto text-caption text-muted-foreground">{noneCount}</span>
          )}
        </DropdownMenuCheckboxItem>
      </>
    );
  }

  const atCapacity = selected.length >= MAX_ISSUE_PROPERTY_FILTER_VALUES;
  return (
    <>
      {options.map((option) => {
        const checked = selected.includes(option.id);
        const count = counts?.get(option.id) ?? 0;
        return (
          <DropdownMenuCheckboxItem
            key={option.id}
            checked={checked}
            disabled={fixedIds?.has(option.id) === true || (atCapacity && !checked)}
            title={fixedIds?.has(option.id) === true ? fixedTitle : undefined}
            onCheckedChange={() => onToggle(option.id)}
            className={FILTER_ITEM_CLASS}
          >
            <HoverCheck checked={checked} />
            {option.actorType && option.actorId && (
              <ActorAvatar actorType={option.actorType} actorId={option.actorId} size="sm" />
            )}
            {option.color && (
              <span
                className="size-2.5 shrink-0 rounded-full"
                style={{ backgroundColor: option.color }}
              />
            )}
            <span className="truncate">{option.name}</span>
            {count > 0 && (
              <span className="ml-auto text-caption text-muted-foreground">
                {count}
              </span>
            )}
          </DropdownMenuCheckboxItem>
        );
      })}
    </>
  );
}
