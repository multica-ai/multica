"use client";

import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, Ellipsis, ListTree, Plus, SlidersHorizontal } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  ADVANCED_FILTER_MAX_CONDITIONS,
  advancedFilterConditionSlots,
  createFilterCondition,
  createFilterGroup,
  editAdvancedFilter,
  isPropertyCondition,
  propertyFilterField,
  propertyIdOfField,
  pruneAdvancedFilter,
  simpleFromAdvancedFilter,
  type AdvancedFilterCondition,
  type AdvancedFilterField,
  type AdvancedFilterGroup,
  type AdvancedFilterNode,
  type DimensionFilterCondition,
  type NegatableFilterDimension,
  type PropertyFilterCondition,
} from "@multica/core/issues/stores/filter-tree";
import {
  conditionOperator,
  conditionText,
  filterFieldSpec,
  filterOperatorsFor,
  isEmptinessOperator,
  isTypedValueKind,
  withConditionOperator,
  withConditionText,
  type FilterFieldKind,
  type FilterOperator,
} from "@multica/core/issues/stores/filter-operators";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { useWorkspaceId } from "@multica/core/hooks";
import { propertyListOptions } from "@multica/core/properties";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  isActorPropertyType,
  isFilterablePropertyType,
  PROPERTY_FILTER_OP_SYMBOLS,
  propertyFilterValueKey,
  type IssueProperty,
  type PropertyFilterValue,
} from "@multica/core/types";
import { useT } from "../../i18n";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { PropertyIcon } from "../../common/property-icon";
import { buildChipActorNames, summarize } from "./filter-chips-bar";
import { PropertyFilterOptions, usePropertyValueName } from "./property-filter-options";
import {
  FILTER_FIELD_ICONS,
  matchesFilterQuery,
  useFilterFieldLabels,
  useFilterValueCatalog,
  type FilterValueOption,
} from "./filter-field-options";

/** Navigation keys move into the list; everything else stays in the input
 *  instead of feeding the menu's typeahead. */
export function stopMenuSearchKeys(event: React.KeyboardEvent) {
  if (["Escape", "Tab", "ArrowDown", "ArrowUp"].includes(event.key)) return;
  event.stopPropagation();
}

type Catalog = (field: NegatableFilterDimension) => FilterValueOption[];

const CONNECTOR_CLASS = "flex h-7 w-14 shrink-0 items-center text-caption text-muted-foreground";
const ROW_SELECT_CLASS = "h-7 rounded-md text-caption";
const VALUE_TRIGGER_CLASS =
  "flex h-7 min-w-0 flex-1 items-center gap-1.5 rounded-md border border-input px-2 text-left text-caption outline-none hover:bg-accent/50 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50 dark:bg-input/30";

interface FieldOption {
  field: AdvancedFilterField;
  label: string;
  icon: ReactNode;
}

/** What a condition can test: the surface's built-in fields, then every
 *  filterable custom property. */
interface ConditionFields {
  builtIn: FieldOption[];
  properties: FieldOption[];
  propertyById: ReadonlyMap<string, IssueProperty>;
  /** Label and icon for any stored field, including a hidden or deleted one. */
  describe: (field: AdvancedFilterField) => FieldOption;
}

function useConditionFields(fields: readonly NegatableFilterDimension[]): ConditionFields {
  const { t } = useT("issues");
  const labels = useFilterFieldLabels();
  const wsId = useWorkspaceId();
  const { data: workspaceProperties = [] } = useQuery(propertyListOptions(wsId));
  const unknownLabel = t(($) => $.filters.advanced_unknown_property);
  return useMemo(() => {
    const dimension = (field: NegatableFilterDimension): FieldOption => {
      const Icon = FILTER_FIELD_ICONS[field];
      return { field, label: labels[field], icon: <Icon className="size-3.5 text-muted-foreground" /> };
    };
    const property = (p: IssueProperty): FieldOption => ({
      field: propertyFilterField(p.id),
      label: p.name,
      icon: p.icon ? (
        <PropertyIcon property={p} className="size-3.5 text-caption" />
      ) : (
        <SlidersHorizontal className="size-3.5 text-muted-foreground" />
      ),
    });
    const filterable = workspaceProperties.filter((p) => isFilterablePropertyType(p.type));
    const propertyById = new Map(filterable.map((p) => [p.id, p]));
    return {
      builtIn: fields.map(dimension),
      properties: filterable.map(property),
      propertyById,
      describe: (field) => {
        const propertyId = propertyIdOfField(field);
        if (propertyId === null) return dimension(field as NegatableFilterDimension);
        const known = propertyById.get(propertyId);
        return known
          ? property(known)
          : { field, label: unknownLabel, icon: <SlidersHorizontal className="size-3.5 text-muted-foreground" /> };
      },
    };
  }, [fields, labels, workspaceProperties, unknownLabel]);
}

function FieldPickerMenu({
  fields,
  trigger,
  heading,
  defaultOpen,
  onOpenChange,
  onPick,
}: {
  fields: ConditionFields;
  trigger: React.ReactElement;
  heading?: string;
  /** Open as soon as it mounts: the menu of a group just created. */
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  onPick: (field: AdvancedFilterField) => void;
}) {
  const item = (option: FieldOption) => (
    <DropdownMenuItem key={option.field} onClick={() => onPick(option.field)}>
      {option.icon}
      <span className="truncate">{option.label}</span>
    </DropdownMenuItem>
  );
  return (
    <DropdownMenu defaultOpen={defaultOpen} onOpenChange={(open) => onOpenChange?.(open)}>
      <DropdownMenuTrigger render={trigger} />
      <DropdownMenuContent align="start" className="w-52">
        {heading && (
          <DropdownMenuGroup>
            <DropdownMenuLabel>{heading}</DropdownMenuLabel>
          </DropdownMenuGroup>
        )}
        {fields.builtIn.map(item)}
        {fields.properties.length > 0 && <DropdownMenuSeparator />}
        {fields.properties.map(item)}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function MatchSelect({
  value,
  onChange,
  className,
}: {
  value: "all" | "any";
  onChange: (value: "all" | "any") => void;
  className?: string;
}) {
  const { t } = useT("issues");
  const items = [
    { value: "all", label: t(($) => $.filters.match_all) },
    { value: "any", label: t(($) => $.filters.match_any) },
  ];
  return (
    <Select
      items={items}
      value={value}
      onValueChange={(next) => {
        if (next === "all" || next === "any") onChange(next);
      }}
    >
      <SelectTrigger size="sm" className={cn(ROW_SELECT_CLASS, "w-auto", className)}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false} align="start">
        {items.map((item) => (
          <SelectItem key={item.value} value={item.value} className="text-caption">
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function useJoinWord() {
  const { t } = useT("issues");
  return (match: "all" | "any") =>
    match === "any" ? t(($) => $.filters.advanced_or) : t(($) => $.filters.advanced_and);
}

/** "Where" before the first row, then the word the group's match joins rows
 *  with. The word is chosen in the group's header, not on a row. */
function RowConnector({ index, match }: { index: number; match: "all" | "any" }) {
  const { t } = useT("issues");
  const joinWord = useJoinWord();
  return (
    <span className={CONNECTOR_CLASS}>
      {index === 0 ? t(($) => $.filters.advanced_where) : joinWord(match)}
    </span>
  );
}

function useOperatorLabel() {
  const { t } = useT("issues");
  return (op: FilterOperator, kind: FilterFieldKind): string => {
    const set = kind === "multi" || kind === "list";
    switch (op) {
      case "is":
        if (kind === "number") return "=";
        return set ? t(($) => $.filters.operator_has_any) : t(($) => $.filters.operator_is);
      case "is_not":
        if (kind === "number") return "≠";
        return set ? t(($) => $.filters.operator_has_none) : t(($) => $.filters.operator_is_not);
      case "contains":
        return t(($) => $.filters.operator_contains);
      case "not_contains":
        return t(($) => $.filters.operator_not_contains);
      case "before":
        return t(($) => $.filters.operator_before);
      case "after":
        return t(($) => $.filters.operator_after);
      case "is_empty":
        return t(($) => $.filters.operator_empty);
      case "is_not_empty":
        return t(($) => $.filters.operator_not_empty);
      default:
        return PROPERTY_FILTER_OP_SYMBOLS[op] ?? op;
    }
  };
}

/** A typed value (text, number, date, list element), committed on Enter or
 *  blur so each keystroke doesn't refetch. */
function TypedValueInput({
  kind,
  value,
  onCommit,
}: {
  kind: FilterFieldKind;
  value: string;
  onCommit: (text: string) => void;
}) {
  const { t } = useT("issues");
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
  const commit = () => {
    if (draft.trim() !== value) onCommit(draft);
  };
  return (
    <Input
      type={kind === "number" ? "number" : kind === "date" ? "date" : "text"}
      step={kind === "number" ? "any" : undefined}
      inputMode={kind === "number" ? "decimal" : undefined}
      aria-label={t(($) => $.filters.advanced_values_label)}
      placeholder={
        kind === "number"
          ? t(($) => $.pickers.custom_property.number_placeholder)
          : t(($) => $.pickers.custom_property.value_placeholder)
      }
      value={draft}
      onChange={(event) => setDraft(event.target.value)}
      onBlur={commit}
      onKeyDown={(event) => {
        if (event.key === "Enter") commit();
      }}
      className="h-7 min-w-0 flex-1 rounded-md px-2 text-caption md:text-caption"
    />
  );
}

function ValuePicker({
  condition,
  catalog,
  onChange,
}: {
  condition: DimensionFilterCondition;
  catalog: Catalog;
  onChange: (values: string[]) => void;
}) {
  const { t } = useT("issues");
  const [search, setSearch] = useState("");
  const options = catalog(condition.field);
  const query = search.trim().toLowerCase();
  const visible = options.filter((option) => matchesFilterQuery(option.label, query));
  const selected = options.filter((option) => condition.values.includes(option.value));
  const toggle = (value: string) =>
    onChange(
      condition.values.includes(value)
        ? condition.values.filter((v) => v !== value)
        : [...condition.values, value],
    );
  const [first] = selected;
  const summary =
    condition.values.length === 0
      ? null
      : first
        ? condition.values.length > 1
          ? `${first.label} +${condition.values.length - 1}`
          : first.label
        : String(condition.values.length);

  let lastGroup: string | undefined;
  return (
    <DropdownMenu onOpenChange={(open) => !open && setSearch("")}>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label={t(($) => $.filters.advanced_values_label)}
            className={VALUE_TRIGGER_CLASS}
          />
        }
      >
        {selected.slice(0, 3).map((option) => (
          <span key={option.value} className="flex shrink-0 items-center">
            {option.icon}
          </span>
        ))}
        <span className={cn("min-w-0 flex-1 truncate", summary ? "font-medium" : "text-muted-foreground")}>
          {summary ?? t(($) => $.filters.advanced_values_placeholder)}
        </span>
        <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-64 p-0">
        <div className="border-b border-foreground/5 px-2 py-1.5">
          <input
            type="text"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            onKeyDown={stopMenuSearchKeys}
            placeholder={t(($) => $.filters.placeholder)}
            className="w-full bg-transparent text-body outline-none placeholder:text-muted-foreground"
            autoFocus
          />
        </div>
        <div className="max-h-64 overflow-y-auto p-1">
          {visible.map((option) => {
            const checked = condition.values.includes(option.value);
            const heading =
              option.group && option.group !== lastGroup ? option.group : undefined;
            lastGroup = option.group;
            return (
              <div key={option.value}>
                {heading && (
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>{heading}</DropdownMenuLabel>
                  </DropdownMenuGroup>
                )}
                <DropdownMenuCheckboxItem
                  checked={checked}
                  onCheckedChange={() => toggle(option.value)}
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={checked} />
                  {option.icon}
                  <span className="truncate">{option.label}</span>
                </DropdownMenuCheckboxItem>
              </div>
            );
          })}
          {visible.length === 0 && (
            <div className="px-2 py-3 text-center text-body text-muted-foreground">
              {t(($) => $.filters.no_results)}
            </div>
          )}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** A property condition edits its values with the property's own options:
 *  option checkboxes, operator + value for scalars, and "No value". */
function PropertyValuePicker({
  condition,
  property,
  onChange,
}: {
  condition: PropertyFilterCondition;
  property: IssueProperty | undefined;
  onChange: (values: PropertyFilterValue[]) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!property && isActorPropertyType(property.type),
  });
  const actorName = useMemo(() => buildChipActorNames(members, [], []), [members]);
  const valueName = usePropertyValueName(actorName);
  const summary =
    condition.values.length === 0
      ? null
      : summarize(condition.values.map((value) => (property ? valueName(property, value) : undefined)));
  const toggle = (value: string) =>
    onChange(
      condition.values.some((v) => propertyFilterValueKey(v) === value)
        ? condition.values.filter((v) => propertyFilterValueKey(v) !== value)
        : [...condition.values, value],
    );
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={!property}
        render={
          <button
            type="button"
            aria-label={t(($) => $.filters.advanced_values_label)}
            className={VALUE_TRIGGER_CLASS}
          />
        }
      >
        <span className={cn("min-w-0 flex-1 truncate", summary ? "font-medium" : "text-muted-foreground")}>
          {summary ?? t(($) => $.filters.advanced_values_placeholder)}
        </span>
        <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
      </DropdownMenuTrigger>
      {property && (
        <DropdownMenuContent align="start" className="max-h-80 w-auto min-w-52">
          <PropertyFilterOptions
            property={property}
            counts={undefined}
            selected={condition.values}
            onToggle={toggle}
            onSetValues={onChange}
          />
        </DropdownMenuContent>
      )}
    </DropdownMenu>
  );
}

function RowActions({ children }: { children: ReactNode }) {
  const { t } = useT("issues");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t(($) => $.filters.advanced_actions)}
            className="size-7 shrink-0 text-muted-foreground"
          />
        }
      >
        <Ellipsis className="size-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-40">
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ConditionRow({
  condition,
  fields,
  catalog,
  canAdd,
  onEdit,
  onTurnIntoGroup,
}: {
  condition: AdvancedFilterCondition;
  fields: ConditionFields;
  catalog: Catalog;
  canAdd: boolean;
  onEdit: (edit: (node: AdvancedFilterNode) => AdvancedFilterNode[]) => void;
  /** Only rows directly under the root can become a group. */
  onTurnIntoGroup?: () => void;
}) {
  const { t } = useT("issues");
  const operatorLabel = useOperatorLabel();
  const current = fields.describe(condition.field);
  // A stored condition on a field this surface hides (or a deleted property)
  // still renders, so it can be seen and removed.
  const known = [...fields.builtIn, ...fields.properties].some((o) => o.field === condition.field);
  const fieldItems = [...fields.builtIn, ...fields.properties, ...(known ? [] : [current])];
  const property = isPropertyCondition(condition)
    ? fields.propertyById.get(propertyIdOfField(condition.field) ?? "")
    : undefined;
  const spec = filterFieldSpec(condition, property?.type);
  // The operator the row last showed. An operator stored in the values
  // ("contains", "> 3") is lost while the value is empty, and picking only
  // "No value" under "is" reads back as "is empty"; either way the row keeps
  // what the user chose instead of swapping its editor mid-edit.
  const [pendingOp, setPendingOp] = useState<FilterOperator | null>(null);
  const derived = conditionOperator(condition, spec);
  const operator =
    pendingOp &&
    (condition.values.length === 0 ||
      (isEmptinessOperator(derived) && (pendingOp === "is" || pendingOp === "is_not")))
      ? pendingOp
      : derived;
  const operators = filterOperatorsFor(spec);
  if (!operators.includes(operator)) operators.push(operator);
  const operatorItems = operators.map((op) => ({ value: op, label: operatorLabel(op, spec.kind) }));
  const replace = (next: AdvancedFilterCondition) => onEdit(() => [next]);
  const setValues = (next: AdvancedFilterCondition) => {
    setPendingOp(operator);
    replace(next);
  };
  const fieldItem = (option: FieldOption) => (
    <SelectItem key={option.field} value={option.field} className="text-caption">
      {option.icon}
      <span className="truncate">{option.label}</span>
    </SelectItem>
  );
  return (
    <>
      <Select
        items={fieldItems.map((option) => ({ value: option.field, label: option.label }))}
        value={condition.field}
        onValueChange={(next) => {
          if (next && next !== condition.field) {
            setPendingOp(null);
            replace({ ...condition, field: next, values: [], negated: false } as AdvancedFilterCondition);
          }
        }}
      >
        <SelectTrigger
          size="sm"
          aria-label={t(($) => $.filters.advanced_field_label)}
          className={cn(ROW_SELECT_CLASS, "w-36 shrink-0")}
        >
          {current.icon}
          <SelectValue className="truncate" />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false} align="start" className="max-h-80">
          {fields.builtIn.map(fieldItem)}
          {fields.properties.length > 0 && <SelectSeparator />}
          {fields.properties.map(fieldItem)}
          {!known && fieldItem(current)}
        </SelectContent>
      </Select>
      <Select
        items={operatorItems}
        value={operator}
        onValueChange={(next) => {
          const op = next as FilterOperator;
          setPendingOp(op);
          replace(withConditionOperator(condition, spec, op));
        }}
      >
        <SelectTrigger
          size="sm"
          aria-label={t(($) => $.filters.operator_label)}
          className={cn(ROW_SELECT_CLASS, "w-32 shrink-0")}
        >
          <SelectValue className="truncate" />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false} align="start">
          {operatorItems.map((item) => (
            <SelectItem key={item.value} value={item.value} className="text-caption">
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {isEmptinessOperator(operator) ? (
        <span className="min-w-0 flex-1" />
      ) : isTypedValueKind(spec.kind) ? (
        <TypedValueInput
          kind={spec.kind}
          value={conditionText(condition)}
          onCommit={(text) => setValues(withConditionText(condition, operator, text))}
        />
      ) : isPropertyCondition(condition) ? (
        <PropertyValuePicker
          condition={condition}
          property={property}
          onChange={(values) => setValues({ ...condition, values })}
        />
      ) : (
        <ValuePicker condition={condition} catalog={catalog} onChange={(values) => setValues({ ...condition, values })} />
      )}
      <RowActions>
        {onTurnIntoGroup && (
          <DropdownMenuItem onClick={onTurnIntoGroup}>{t(($) => $.filters.advanced_turn_into_group)}</DropdownMenuItem>
        )}
        <DropdownMenuItem
          disabled={!canAdd}
          onClick={() =>
            onEdit((node) => [
              node,
              createFilterCondition(condition.field, [...condition.values], condition.negated),
            ])
          }
        >
          {t(($) => $.filters.advanced_duplicate)}
        </DropdownMenuItem>
        <DropdownMenuItem variant="destructive" onClick={() => onEdit(() => [])}>
          {t(($) => $.filters.advanced_delete)}
        </DropdownMenuItem>
      </RowActions>
    </>
  );
}

function AddButton({ children, ...props }: React.ComponentProps<typeof Button>) {
  return (
    <Button variant="ghost" size="sm" className="h-7 gap-1.5 px-2 text-caption text-muted-foreground" {...props}>
      <Plus className="size-3.5" />
      {children}
    </Button>
  );
}

/** A condition as a phrase: "Label includes any of Bug +1". */
function useConditionPhrase(fields: ConditionFields, catalog: Catalog) {
  const operatorLabel = useOperatorLabel();
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const actorName = useMemo(() => buildChipActorNames(members, [], []), [members]);
  const valueName = usePropertyValueName(actorName);
  return (condition: AdvancedFilterCondition): string => {
    const property = isPropertyCondition(condition)
      ? fields.propertyById.get(propertyIdOfField(condition.field) ?? "")
      : undefined;
    const spec = filterFieldSpec(condition, property?.type);
    const op = conditionOperator(condition, spec);
    const head = `${fields.describe(condition.field).label} ${operatorLabel(op, spec.kind)}`;
    if (isEmptinessOperator(op)) return head;
    let value: string;
    if (isTypedValueKind(spec.kind)) {
      value = conditionText(condition);
    } else if (isPropertyCondition(condition)) {
      value = summarize(condition.values.map((v) => (property ? valueName(property, v) : undefined)));
    } else {
      const options = catalog(condition.field);
      value = summarize(condition.values.map((v) => options.find((option) => option.value === v)?.label));
    }
    return `${head} ${value}`;
  };
}

/** A pruned tree as one sentence, a group in parentheses. */
function describeGroup(
  group: AdvancedFilterGroup,
  phrase: (condition: AdvancedFilterCondition) => string,
  joinWord: (match: "all" | "any") => string,
  nested = false,
): string {
  const parts = group.children.map((child) =>
    child.type === "group" ? describeGroup(child, phrase, joinWord, true) : phrase(child),
  );
  const text = parts.join(` ${joinWord(group.match)} `);
  return nested && parts.length > 1 ? `(${text})` : text;
}

/** A group under the root: its own match in the header, then its rows. An
 *  empty group is a slot to add the first condition into. */
function FilterGroupCard({
  group,
  fields,
  catalog,
  canAdd,
  openAddMenu,
  onAddMenuClosed,
  edit,
  onAppend,
  onMatchChange,
  onDelete,
}: {
  group: AdvancedFilterGroup;
  fields: ConditionFields;
  catalog: Catalog;
  canAdd: boolean;
  openAddMenu: boolean;
  onAddMenuClosed: () => void;
  edit: (id: string) => (fn: (node: AdvancedFilterNode) => AdvancedFilterNode[]) => void;
  onAppend: (node: AdvancedFilterNode) => void;
  onMatchChange: (match: "all" | "any") => void;
  onDelete: () => void;
}) {
  const { t } = useT("issues");
  const joinWord = useJoinWord();
  const addMenu = (
    <FieldPickerMenu
      fields={fields}
      heading={t(($) => $.filters.advanced_add_to_group)}
      defaultOpen={openAddMenu && canAdd}
      onOpenChange={(open) => {
        if (!open) onAddMenuClosed();
      }}
      trigger={<AddButton disabled={!canAdd}>{t(($) => $.filters.advanced_add_condition)}</AddButton>}
      onPick={(field) => {
        onAddMenuClosed();
        onAppend(createFilterCondition(field));
      }}
    />
  );
  return (
    <div
      className="flex min-w-0 flex-1 flex-col gap-1.5 rounded-lg border border-border bg-muted/40 p-2"
      data-testid="advanced-filter-group"
    >
      <div className="flex items-center gap-1.5">
        <span className="pl-1 text-caption text-muted-foreground">
          {t(($) => $.filters.advanced_group_match_leading)}
        </span>
        <MatchSelect value={group.match} onChange={onMatchChange} />
        <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">
          {t(($) => $.filters.advanced_group_match_trailing)}
        </span>
        <RowActions>
          <DropdownMenuItem variant="destructive" onClick={onDelete}>
            {t(($) => $.filters.advanced_delete_group)}
          </DropdownMenuItem>
        </RowActions>
      </div>
      {group.children.length === 0 ? (
        <div className="flex items-center gap-2 rounded-md border border-dashed border-border p-0.5">
          {addMenu}
          <span className="min-w-0 truncate text-caption text-muted-foreground">
            {t(($) => $.filters.advanced_group_hint, { word: joinWord(group.match) })}
          </span>
        </div>
      ) : (
        <>
          {group.children.map((child, index) =>
            child.type === "condition" ? (
              <div key={child.id} className="flex items-start gap-1.5">
                <RowConnector index={index} match={group.match} />
                <ConditionRow
                  condition={child}
                  fields={fields}
                  catalog={catalog}
                  canAdd={canAdd}
                  onEdit={edit(child.id)}
                />
              </div>
            ) : null,
          )}
          <div>{addMenu}</div>
        </>
      )}
    </div>
  );
}

/**
 * The advanced filter: a rule list with "all" / "any" and one level of
 * groups, edited in place on the view store. Edits apply live, like the
 * simple menu; rows without values filter nothing until they get one.
 */
export function AdvancedFilterPanel({
  fields: surfaceFields,
}: {
  /** Fields the surface offers; a scope that fixes one leaves it out. */
  fields: readonly NegatableFilterDimension[];
}) {
  const { t } = useT("issues");
  const stored = useViewStore((s) => s.advancedFilter);
  const setAdvancedFilter = useViewStore((s) => s.setAdvancedFilter);
  const exitAdvancedFilter = useViewStore((s) => s.exitAdvancedFilter);
  const emptyRoot = useMemo(() => createFilterGroup(), []);
  const root = stored ?? emptyRoot;
  const catalog = useFilterValueCatalog(true);
  const fields = useConditionFields(surfaceFields);
  const canAdd = advancedFilterConditionSlots(root) < ADVANCED_FILTER_MAX_CONDITIONS;
  const representable = simpleFromAdvancedFilter(root) !== null;

  const edit = (id: string) => (fn: (node: AdvancedFilterNode) => AdvancedFilterNode[]) =>
    setAdvancedFilter(editAdvancedFilter(root, id, fn));
  const append = (groupId: string, node: AdvancedFilterNode) =>
    edit(groupId)((group) => [
      { ...(group as AdvancedFilterGroup), children: [...(group as AdvancedFilterGroup).children, node] },
    ]);
  const setMatch = (groupId: string) => (match: "all" | "any") =>
    edit(groupId)((group) => [{ ...(group as AdvancedFilterGroup), match }]);
  // The group whose add menu opens as it mounts: one just created or just
  // made from a row. Cleared once a menu closes, so a remount stays shut.
  const [openGroupId, setOpenGroupId] = useState<string | null>(null);
  // A group is usually the other word: "and" rules holding an "or".
  const newGroup = (children: AdvancedFilterNode[] = []) =>
    createFilterGroup(root.match === "all" ? "any" : "all", children);
  const phrase = useConditionPhrase(fields, catalog);
  const joinWord = useJoinWord();
  const pruned = pruneAdvancedFilter(root);
  const summary = pruned?.children.some((child) => child.type === "group")
    ? describeGroup(pruned, phrase, joinWord)
    : null;

  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-1.5 px-3 pt-3 pb-2">
        <ListTree className="size-3.5 text-muted-foreground" />
        <span className="font-medium">{t(($) => $.filters.advanced_title)}</span>
        <span className="ml-2 text-caption text-muted-foreground">
          {t(($) => $.filters.advanced_match_leading)}
        </span>
        <MatchSelect value={root.match} onChange={setMatch(root.id)} />
        <span className="text-caption text-muted-foreground">
          {t(($) => $.filters.advanced_match_trailing)}
        </span>
      </div>

      <div className="flex max-h-[60vh] flex-col gap-1.5 overflow-y-auto px-3 py-1">
        {root.children.length === 0 && (
          <p className="py-3 text-caption text-muted-foreground">{t(($) => $.filters.advanced_empty)}</p>
        )}
        {root.children.map((child, index) => (
          <div key={child.id} className="flex items-start gap-1.5" data-testid="advanced-filter-row">
            <RowConnector index={index} match={root.match} />
            {child.type === "condition" ? (
              <ConditionRow
                condition={child}
                fields={fields}
                catalog={catalog}
                canAdd={canAdd}
                onEdit={edit(child.id)}
                onTurnIntoGroup={() => {
                  const group = newGroup([child]);
                  edit(child.id)(() => [group]);
                  setOpenGroupId(group.id);
                }}
              />
            ) : (
              <FilterGroupCard
                group={child}
                fields={fields}
                catalog={catalog}
                canAdd={canAdd}
                openAddMenu={openGroupId === child.id}
                onAddMenuClosed={() => setOpenGroupId(null)}
                edit={edit}
                onAppend={(node) => append(child.id, node)}
                onMatchChange={setMatch(child.id)}
                onDelete={() => edit(child.id)(() => [])}
              />
            )}
          </div>
        ))}
        {summary && (
          <p className="pt-1 text-caption text-pretty text-muted-foreground" data-testid="advanced-filter-summary">
            {t(($) => $.filters.advanced_summary, { summary })}
          </p>
        )}
      </div>

      <div className="flex items-center gap-1 px-2 py-1.5">
        <FieldPickerMenu
          fields={fields}
          trigger={<AddButton disabled={!canAdd}>{t(($) => $.filters.advanced_add_condition)}</AddButton>}
          onPick={(field) => append(root.id, createFilterCondition(field))}
        />
        <AddButton
          disabled={!canAdd}
          onClick={() => {
            const group = newGroup();
            append(root.id, group);
            setOpenGroupId(group.id);
          }}
        >
          {t(($) => $.filters.advanced_add_group)}
        </AddButton>
      </div>

      <div className="flex items-center gap-2 border-t border-border px-2 py-1.5">
        <Button
          variant="ghost"
          size="sm"
          className="h-7 shrink-0 px-2 text-caption"
          disabled={!representable}
          onClick={exitAdvancedFilter}
        >
          {t(($) => $.filters.advanced_to_simple)}
        </Button>
        {!representable && (
          <span className="min-w-0 truncate text-caption text-muted-foreground" title={t(($) => $.filters.advanced_to_simple_blocked)}>
            {t(($) => $.filters.advanced_to_simple_blocked)}
          </span>
        )}
        <Button
          variant="ghost"
          size="sm"
          className="ml-auto h-7 shrink-0 px-2 text-caption text-muted-foreground"
          disabled={root.children.length === 0}
          onClick={() => setAdvancedFilter(createFilterGroup(root.match))}
        >
          {t(($) => $.filters.advanced_clear)}
        </Button>
      </div>
    </div>
  );
}
