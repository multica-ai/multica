import * as React from "react";
import { Select as SelectPrimitive } from "radix-ui";

import { CheckIcon, ChevronDownIcon, ChevronUpIcon } from "#icons/system-icons";
import { cn } from "#lib/utils";
import { buttonVariants } from "../button";
import {
  MenuSurface,
  menuItemIndicatorClassName,
  menuItemWithIndicatorClassName
} from "../menu-surface";
import { getPortalContainer } from "../../lib/portal-container";

function Select({
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Root>) {
  return <SelectPrimitive.Root data-slot="select" {...props} />;
}

function SelectGroup({
  className,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Group>) {
  return (
    <SelectPrimitive.Group
      data-slot="select-group"
      className={cn("flex flex-col gap-0.5 scroll-my-1 p-1", className)}
      {...props}
    />
  );
}

function SelectValue({
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Value>) {
  return <SelectPrimitive.Value data-slot="select-value" {...props} />;
}

function SelectTrigger({
  className,
  size = "default",
  variant = "default",
  children,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Trigger> & {
  size?: "sm" | "default" | "dialog";
  variant?: "button" | "default";
}) {
  return (
    <SelectPrimitive.Trigger
      data-slot="select-trigger"
      data-size={size}
      data-variant={variant}
      className={cn(
        variant === "button"
          ? buttonVariants({ variant: "default", size })
          : "flex w-fit cursor-pointer items-center justify-between gap-1.5 rounded-lg border border-transparent bg-[var(--transparency-block)] py-2 pr-2 pl-2.5 text-[13px] text-[var(--text-primary)] whitespace-nowrap transition-colors outline-none select-none hover:bg-[var(--transparency-hover)] focus:bg-[var(--transparency-hover)] focus-visible:border-transparent focus-visible:bg-[var(--transparency-hover)] focus-visible:ring-0 disabled:cursor-not-allowed disabled:bg-[var(--transparency-block)] disabled:text-[var(--text-disabled)] disabled:opacity-100 aria-invalid:border-[var(--state-danger)] aria-invalid:bg-[var(--transparency-block)] aria-invalid:hover:bg-[var(--transparency-hover)] aria-invalid:focus:bg-[var(--transparency-hover)] aria-invalid:focus-visible:bg-[var(--transparency-hover)] aria-invalid:ring-0 aria-invalid:shadow-none data-placeholder:text-[var(--text-placeholder)] data-[size=default]:h-8 data-[size=sm]:h-7 data-[size=sm]:rounded-[min(var(--radius-md),10px)] *:data-[slot=select-value]:line-clamp-1 *:data-[slot=select-value]:flex *:data-[slot=select-value]:items-center *:data-[slot=select-value]:gap-1.5 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        "[&[data-state=open]>svg]:rotate-180 [&>svg]:transition-transform [&>svg]:duration-200",
        className
      )}
      {...props}
    >
      {children}
      <SelectPrimitive.Icon asChild>
        <ChevronDownIcon
          className={cn("pointer-events-none size-4", "text-current")}
        />
      </SelectPrimitive.Icon>
    </SelectPrimitive.Trigger>
  );
}

function SelectContent({
  className,
  children,
  position = "popper",
  align = "center",
  style,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Content>) {
  return (
    <SelectPrimitive.Portal container={getPortalContainer()}>
      <SelectPrimitive.Content
        asChild
        data-slot="select-content"
        data-align-trigger={position === "item-aligned"}
        position={position}
        align={align}
        {...props}
      >
        <MenuSurface
          data-slot="select-content"
          className={cn(
            "relative z-50 max-h-(--radix-select-content-available-height) min-w-36 origin-(--radix-select-content-transform-origin) overflow-x-hidden overflow-y-auto",
            position === "popper" &&
              "data-[side=bottom]:translate-y-1 data-[side=left]:-translate-x-1 data-[side=right]:translate-x-1 data-[side=top]:-translate-y-1",
            className
          )}
          style={{ zIndex: "var(--z-popover)", ...style }}
        >
          <SelectScrollUpButton />
          <SelectPrimitive.Viewport
            data-position={position}
            className={cn(
              "flex flex-col gap-0.5 data-[position=popper]:w-full data-[position=popper]:[min-width:var(--tutti-select-content-min-width,var(--radix-select-trigger-width))]"
            )}
          >
            {children}
          </SelectPrimitive.Viewport>
          <SelectScrollDownButton />
        </MenuSurface>
      </SelectPrimitive.Content>
    </SelectPrimitive.Portal>
  );
}

function SelectLabel({
  className,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Label>) {
  return (
    <SelectPrimitive.Label
      data-slot="select-label"
      className={cn(
        "px-1.5 py-1 text-[11px] font-normal text-[var(--text-secondary)]",
        className
      )}
      {...props}
    />
  );
}

function SelectSplitLayout({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="select-split-layout"
      className={cn(
        "grid h-full min-h-0 grid-cols-[minmax(0,1fr)_1px_minmax(104px,132px)] gap-1 overflow-hidden",
        className
      )}
      {...props}
    />
  );
}

function SelectSplitColumn({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="select-split-column"
      className={cn("flex h-full min-h-0 min-w-0 flex-col", className)}
      {...props}
    />
  );
}

function SelectSplitColumnLabel({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="select-split-column-label"
      className={cn(
        "shrink-0 px-2 pt-1 pb-2 text-[11px] font-normal leading-[18px] text-[var(--text-secondary)]",
        className
      )}
      {...props}
    />
  );
}

function SelectSplitColumnItems({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="select-split-column-items"
      className={cn(
        "flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto overscroll-contain",
        className
      )}
      {...props}
    />
  );
}

function SelectSplitDivider({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      aria-hidden="true"
      data-slot="select-split-divider"
      className={cn(
        "self-stretch bg-[var(--border-2,var(--border-1))]",
        className
      )}
      {...props}
    />
  );
}

function SelectItem({
  className,
  children,
  forceSelectedIndicator = false,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Item> & {
  forceSelectedIndicator?: boolean;
}) {
  return (
    <SelectPrimitive.Item
      data-slot="select-item"
      className={cn("w-full", menuItemWithIndicatorClassName, className)}
      {...props}
    >
      <span className={menuItemIndicatorClassName}>
        {forceSelectedIndicator ? (
          <CheckIcon
            className="pointer-events-none text-[var(--tutti-purple)]"
            data-slot="select-item-forced-indicator"
          />
        ) : (
          <SelectPrimitive.ItemIndicator>
            <CheckIcon className="pointer-events-none text-[var(--tutti-purple)]" />
          </SelectPrimitive.ItemIndicator>
        )}
      </span>
      <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
    </SelectPrimitive.Item>
  );
}

function SelectSeparator({
  className,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Separator>) {
  return (
    <SelectPrimitive.Separator
      data-slot="select-separator"
      className={cn(
        "pointer-events-none mx-2 my-0.5 h-px bg-[var(--border-1)]",
        className
      )}
      {...props}
    />
  );
}

function SelectScrollUpButton({
  className,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.ScrollUpButton>) {
  return (
    <SelectPrimitive.ScrollUpButton
      data-slot="select-scroll-up-button"
      className={cn(
        "z-10 flex cursor-pointer items-center justify-center bg-[var(--background-fronted)] py-1 [&_svg:not([class*='size-'])]:size-4",
        className
      )}
      {...props}
    >
      <ChevronUpIcon />
    </SelectPrimitive.ScrollUpButton>
  );
}

function SelectScrollDownButton({
  className,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.ScrollDownButton>) {
  return (
    <SelectPrimitive.ScrollDownButton
      data-slot="select-scroll-down-button"
      className={cn(
        "z-10 flex cursor-pointer items-center justify-center bg-[var(--background-fronted)] py-1 [&_svg:not([class*='size-'])]:size-4",
        className
      )}
      {...props}
    >
      <ChevronDownIcon />
    </SelectPrimitive.ScrollDownButton>
  );
}

export {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectScrollDownButton,
  SelectScrollUpButton,
  SelectSeparator,
  SelectSplitColumn,
  SelectSplitColumnItems,
  SelectSplitColumnLabel,
  SelectSplitDivider,
  SelectSplitLayout,
  SelectTrigger,
  SelectValue
};
