import * as React from "react";
import { DropdownMenu as DropdownMenuPrimitive } from "radix-ui";

import { ArrowRightIcon, CheckIcon } from "#icons/system-icons";
import { cn } from "#lib/utils";
import {
  MenuSurface,
  menuItemClassName,
  menuItemIndicatorClassName,
  menuItemWithIndicatorClassName
} from "../menu-surface";
import { getPortalContainer } from "../../lib/portal-container";

function DropdownMenu({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Root>) {
  return <DropdownMenuPrimitive.Root data-slot="dropdown-menu" {...props} />;
}

function DropdownMenuPortal({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Portal>) {
  return (
    <DropdownMenuPrimitive.Portal container={getPortalContainer()} data-slot="dropdown-menu-portal" {...props} />
  );
}

function DropdownMenuTrigger({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Trigger>) {
  return (
    <DropdownMenuPrimitive.Trigger
      data-slot="dropdown-menu-trigger"
      {...props}
    />
  );
}

function DropdownMenuContent({
  className,
  children,
  align = "start",
  sideOffset = 4,
  style,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Content>) {
  return (
    <DropdownMenuPrimitive.Portal container={getPortalContainer()}>
      <DropdownMenuPrimitive.Content
        asChild
        data-slot="dropdown-menu-content"
        align={align}
        sideOffset={sideOffset}
        {...props}
      >
        <MenuSurface
          data-slot="dropdown-menu-content"
          className={cn(
            "z-50 max-h-(--radix-dropdown-menu-content-available-height) w-(--radix-dropdown-menu-trigger-width) min-w-32 origin-(--radix-dropdown-menu-content-transform-origin) overflow-x-hidden overflow-y-auto",
            className
          )}
          style={{ zIndex: "var(--z-popover)", ...style }}
        >
          {children}
        </MenuSurface>
      </DropdownMenuPrimitive.Content>
    </DropdownMenuPrimitive.Portal>
  );
}

function DropdownMenuGroup({
  className,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Group>) {
  return (
    <DropdownMenuPrimitive.Group
      data-slot="dropdown-menu-group"
      className={cn("flex flex-col gap-0.5", className)}
      {...props}
    />
  );
}

function DropdownMenuItem({
  className,
  inset,
  variant = "default",
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Item> & {
  inset?: boolean;
  variant?: "default" | "destructive";
}) {
  return (
    <DropdownMenuPrimitive.Item
      data-inset={inset}
      data-slot="dropdown-menu-item"
      data-variant={variant}
      className={cn(
        "group/dropdown-menu-item",
        menuItemClassName,
        "data-inset:pl-7 data-[variant=destructive]:text-[var(--state-danger)] data-[variant=destructive]:focus:bg-[var(--on-danger-hover)] data-[variant=destructive]:focus:text-[var(--state-danger)] data-[variant=destructive]:hover:bg-[var(--on-danger-hover)] data-[variant=destructive]:hover:text-[var(--state-danger)] data-[variant=destructive]:*:[svg]:text-[var(--state-danger)]",
        className
      )}
      {...props}
    />
  );
}

function DropdownMenuCheckboxItem({
  className,
  children,
  checked,
  inset,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.CheckboxItem> & {
  inset?: boolean;
}) {
  return (
    <DropdownMenuPrimitive.CheckboxItem
      checked={checked}
      data-inset={inset}
      data-slot="dropdown-menu-checkbox-item"
      className={cn(
        menuItemWithIndicatorClassName,
        "data-inset:pl-7 data-[state=checked]:[&_[data-slot=dropdown-menu-checkbox-item-indeterminate]]:hidden data-[state=indeterminate]:[&_[data-slot=dropdown-menu-checkbox-item-check]]:hidden",
        className
      )}
      {...props}
    >
      <span
        className={menuItemIndicatorClassName}
        data-slot="dropdown-menu-checkbox-item-indicator"
      >
        <DropdownMenuPrimitive.ItemIndicator className="grid place-content-center text-[var(--tutti-purple)]">
          <CheckIcon data-slot="dropdown-menu-checkbox-item-check" />
          <span
            aria-hidden="true"
            className="h-0.5 w-2.5 rounded-full bg-current"
            data-slot="dropdown-menu-checkbox-item-indeterminate"
          />
        </DropdownMenuPrimitive.ItemIndicator>
      </span>
      {children}
    </DropdownMenuPrimitive.CheckboxItem>
  );
}

function DropdownMenuRadioGroup({
  className,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.RadioGroup>) {
  return (
    <DropdownMenuPrimitive.RadioGroup
      data-slot="dropdown-menu-radio-group"
      className={cn("flex flex-col gap-0.5", className)}
      {...props}
    />
  );
}

function DropdownMenuRadioItem({
  className,
  children,
  inset,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.RadioItem> & {
  inset?: boolean;
}) {
  return (
    <DropdownMenuPrimitive.RadioItem
      data-inset={inset}
      data-slot="dropdown-menu-radio-item"
      className={cn(
        menuItemWithIndicatorClassName,
        "data-inset:pl-7",
        className
      )}
      {...props}
    >
      <span
        className={menuItemIndicatorClassName}
        data-slot="dropdown-menu-radio-item-indicator"
      >
        <DropdownMenuPrimitive.ItemIndicator>
          <CheckIcon className="text-[var(--tutti-purple)]" />
        </DropdownMenuPrimitive.ItemIndicator>
      </span>
      {children}
    </DropdownMenuPrimitive.RadioItem>
  );
}

function DropdownMenuLabel({
  className,
  inset,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Label> & {
  inset?: boolean;
}) {
  return (
    <DropdownMenuPrimitive.Label
      data-inset={inset}
      data-slot="dropdown-menu-label"
      className={cn(
        "px-1.5 py-1 text-[11px] font-medium text-[var(--text-secondary)] data-inset:pl-7",
        className
      )}
      {...props}
    />
  );
}

function DropdownMenuSeparator({
  className,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Separator>) {
  return (
    <DropdownMenuPrimitive.Separator
      data-slot="dropdown-menu-separator"
      className={cn("mx-2 my-0.5 h-px bg-[var(--border-1)]", className)}
      {...props}
    />
  );
}

function DropdownMenuShortcut({
  className,
  ...props
}: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="dropdown-menu-shortcut"
      className={cn(
        "ml-auto text-[11px] tracking-widest text-[var(--text-secondary)] group-focus/dropdown-menu-item:text-[var(--text-primary)]",
        className
      )}
      {...props}
    />
  );
}

function DropdownMenuSub({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Sub>) {
  return <DropdownMenuPrimitive.Sub data-slot="dropdown-menu-sub" {...props} />;
}

function DropdownMenuSubTrigger({
  className,
  inset,
  children,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.SubTrigger> & {
  inset?: boolean;
}) {
  return (
    <DropdownMenuPrimitive.SubTrigger
      data-inset={inset}
      data-slot="dropdown-menu-sub-trigger"
      className={cn(
        menuItemClassName,
        "data-inset:pl-7 data-open:bg-[var(--transparency-block)] data-open:text-[var(--text-primary)]",
        className
      )}
      {...props}
    >
      {children}
      <ArrowRightIcon className="ml-auto text-[var(--text-tertiary)]" />
    </DropdownMenuPrimitive.SubTrigger>
  );
}

function DropdownMenuSubContent({
  className,
  children,
  style,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.SubContent>) {
  return (
    <DropdownMenuPrimitive.Portal container={getPortalContainer()}>
      <DropdownMenuPrimitive.SubContent
        asChild
        data-slot="dropdown-menu-sub-content"
        {...props}
      >
        <MenuSurface
          data-slot="dropdown-menu-sub-content"
          className={cn(
            "z-50 min-w-[96px] origin-(--radix-dropdown-menu-content-transform-origin) overflow-hidden",
            className
          )}
          style={{ zIndex: "var(--z-popover)", ...style }}
        >
          {children}
        </MenuSurface>
      </DropdownMenuPrimitive.SubContent>
    </DropdownMenuPrimitive.Portal>
  );
}

export {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuPortal,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger
};
