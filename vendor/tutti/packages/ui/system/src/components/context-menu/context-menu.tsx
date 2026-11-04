import * as React from "react";
import { ContextMenu as ContextMenuPrimitive } from "radix-ui";

import { ArrowRightIcon } from "#icons/system-icons";
import { cn } from "#lib/utils";
import { MenuSurface, menuItemClassName } from "../menu-surface";
import { getPortalContainer } from "../../lib/portal-container";

function ContextMenu({
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Root>) {
  return <ContextMenuPrimitive.Root data-slot="context-menu" {...props} />;
}

function ContextMenuTrigger({
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Trigger>) {
  return (
    <ContextMenuPrimitive.Trigger data-slot="context-menu-trigger" {...props} />
  );
}

function ContextMenuContent({
  className,
  children,
  style,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Content>) {
  return (
    <ContextMenuPrimitive.Portal container={getPortalContainer()}>
      <ContextMenuPrimitive.Content
        asChild
        data-slot="context-menu-content"
        {...props}
      >
        <MenuSurface
          data-slot="context-menu-content"
          className={cn(
            "z-50 min-w-32 overflow-x-hidden overflow-y-auto",
            className
          )}
          style={{ zIndex: "var(--z-popover)", ...style }}
        >
          {children}
        </MenuSurface>
      </ContextMenuPrimitive.Content>
    </ContextMenuPrimitive.Portal>
  );
}

function ContextMenuItem({
  className,
  inset,
  variant = "default",
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Item> & {
  inset?: boolean;
  variant?: "default" | "destructive";
}) {
  return (
    <ContextMenuPrimitive.Item
      data-inset={inset}
      data-slot="context-menu-item"
      data-variant={variant}
      className={cn(
        "group/context-menu-item",
        menuItemClassName,
        "data-inset:pl-7",
        className
      )}
      {...props}
    />
  );
}

function ContextMenuSeparator({
  className,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Separator>) {
  return (
    <ContextMenuPrimitive.Separator
      data-slot="context-menu-separator"
      className={cn("mx-2 my-0.5 h-px bg-[var(--border-1)]", className)}
      {...props}
    />
  );
}

function ContextMenuSub({
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.Sub>) {
  return <ContextMenuPrimitive.Sub data-slot="context-menu-sub" {...props} />;
}

function ContextMenuSubTrigger({
  className,
  children,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.SubTrigger>) {
  return (
    <ContextMenuPrimitive.SubTrigger
      data-slot="context-menu-sub-trigger"
      className={cn(
        menuItemClassName,
        "data-open:bg-[var(--transparency-block)] data-open:text-[var(--text-primary)]",
        className
      )}
      {...props}
    >
      {children}
      <ArrowRightIcon className="ml-auto text-[var(--text-tertiary)]" />
    </ContextMenuPrimitive.SubTrigger>
  );
}

function ContextMenuSubContent({
  className,
  children,
  style,
  ...props
}: React.ComponentProps<typeof ContextMenuPrimitive.SubContent>) {
  return (
    <ContextMenuPrimitive.Portal container={getPortalContainer()}>
      <ContextMenuPrimitive.SubContent
        asChild
        data-slot="context-menu-sub-content"
        {...props}
      >
        <MenuSurface
          data-slot="context-menu-sub-content"
          className={cn("z-50 min-w-[96px] overflow-hidden", className)}
          style={{ zIndex: "var(--z-popover)", ...style }}
        >
          {children}
        </MenuSurface>
      </ContextMenuPrimitive.SubContent>
    </ContextMenuPrimitive.Portal>
  );
}

export {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger
};
