"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ComponentProps,
  type ReactNode,
  type Ref,
} from "react";
import { useDefaultLayout, usePanelRef } from "react-resizable-panels";
import { ChevronRight, PanelRight, type IconComponent } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@multica/ui/components/ui/resizable";
import { Sheet, SheetContent } from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { cn } from "@multica/ui/lib/utils";
import { ActionShortcutKeycaps } from "../common/shortcut-keycaps";
import { useT } from "../i18n";
import {
  AnimatedRightSidebar,
  getAnimatedRightSidebarInitialOpen,
  rightSidebarPanelMotionProps,
  useAnimatedRightSidebarState,
  useRightSidebarShortcut,
} from "./animated-right-sidebar";
import { BreadcrumbHeader, type BreadcrumbSegment } from "./breadcrumb-header";

/** The reading column of an object page; the header above it stays full width. */
export const OBJECT_PAGE_BODY =
  "mx-auto w-full max-w-[var(--object-page-max-width,760px)] px-[var(--space-lg,1.5rem)] py-[var(--space-xl,2rem)]";

/** What an object page shows, as themes see it on `data-type`. */
export type ObjectPageType = "project" | "agent" | "squad" | "runtime" | "member" | "autopilot" | "skill";

interface ObjectPageProps {
  type: ObjectPageType;
  /** The lists this object was opened from, each one a link back. */
  segments: BreadcrumbSegment[];
  /** The object's own crumb. */
  leaf: ReactNode;
  /**
   * Header actions, before the panel toggle: icon actions, the page's one
   * solid action, then the ⋯ menu.
   */
  actions?: ReactNode;
  /** The properties panel. Omit and the page has no panel or toggle. */
  panel?: ReactNode;
  /** Persists the panel's size and open state per kind of object. */
  layoutId: string;
  /**
   * The body. Loading, error and not-found states go here too: the header
   * keeps its place while the body changes.
   */
  children: ReactNode;
  /** The body scroll owner, for sections that virtualize their rows. */
  scrollRef?: Ref<HTMLDivElement>;
}

/**
 * The page for one object opened from a list: a breadcrumb header, the body
 * as sections in a centred column, and the object's editable properties in a
 * right panel the header toggles — the layout the issue page already uses.
 * There are no tabs: what a tab would hold is a section of the body.
 */
export function ObjectPage({ type, segments, leaf, actions, panel, layoutId, children, scrollRef }: ObjectPageProps) {
  const { t } = useT("layout");
  const isMobile = useIsMobile();
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({ id: layoutId });
  const panelRef = usePanelRef();
  const shortcutTargetRef = useRef<HTMLDivElement | null>(null);
  // Desktop and mobile keep separate state: a mobile sheet that shared the
  // desktop's open default would mount open over the page.
  const {
    open: desktopOpen,
    visualOpen,
    motionEnabled,
    beginToggle,
    handleResize,
  } = useAnimatedRightSidebarState(getAnimatedRightSidebarInitialOpen(true, defaultLayout));
  const [mobileOpen, setMobileOpen] = useState(false);
  const open = isMobile ? mobileOpen : desktopOpen;

  useEffect(() => {
    if (isMobile) setMobileOpen(false);
  }, [isMobile]);

  const toggle = useCallback(() => {
    if (isMobile) {
      setMobileOpen((value) => !value);
      return;
    }
    const sidebar = panelRef.current;
    if (!sidebar) return;
    const nextOpen = sidebar.isCollapsed();
    beginToggle(nextOpen);
    window.requestAnimationFrame(() => {
      if (nextOpen) sidebar.expand();
      else sidebar.collapse();
    });
  }, [beginToggle, isMobile, panelRef]);

  useRightSidebarShortcut(shortcutTargetRef, toggle);

  const header = (
    <BreadcrumbHeader
      segments={segments}
      leaf={leaf}
      actions={
        actions || panel ? (
          <>
            {actions}
            {panel ? (
              <IconButton
                label={t(($) => $.object_page.toggle_panel)}
                shortcut={<ActionShortcutKeycaps action="toggleRightSidebar" />}
                tooltipSide="bottom"
                variant={open ? "secondary" : "ghost"}
                className={open ? undefined : "text-muted-foreground"}
                onClick={toggle}
              >
                <PanelRight />
              </IconButton>
            ) : null}
          </>
        ) : undefined
      }
    />
  );

  const body = <div ref={scrollRef} className="flex min-h-0 flex-1 flex-col overflow-y-auto">{children}</div>;

  if (!panel) {
    return (
      <div data-mc="object-page" data-type={type} className="flex min-h-0 flex-1 flex-col">
        {header}
        {body}
      </div>
    );
  }

  return (
    <div ref={shortcutTargetRef} data-mc="object-page" data-type={type} className="flex min-h-0 flex-1 flex-col">
      {header}
      <ResizablePanelGroup
        orientation="horizontal"
        className="min-h-0 flex-1"
        defaultLayout={defaultLayout}
        onLayoutChanged={onLayoutChanged}
      >
        <ResizablePanel id="content" minSize="50%">
          <div className="flex h-full flex-col">{body}</div>
        </ResizablePanel>
        {!isMobile && <ResizableHandle />}
        {!isMobile && (
          <ResizablePanel
            id="sidebar"
            {...rightSidebarPanelMotionProps}
            data-right-sidebar-motion={motionEnabled ? "enabled" : undefined}
            defaultSize={desktopOpen ? 300 : 0}
            minSize={260}
            maxSize={420}
            collapsible
            groupResizeBehavior="preserve-pixel-size"
            panelRef={panelRef}
            onResize={handleResize}
          >
            <AnimatedRightSidebar open={visualOpen} motionEnabled={motionEnabled}>
              {panel}
            </AnimatedRightSidebar>
          </ResizablePanel>
        )}
      </ResizablePanelGroup>
      {isMobile && (
        <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
          <SheetContent side="right" showCloseButton={false} className="w-[300px] overflow-y-auto p-4">
            {panel}
          </SheetContent>
        </Sheet>
      )}
    </div>
  );
}

/**
 * The object's name block at the top of the body: a 48px mark, the name, and
 * one line on what it is or does.
 */
export function ObjectIdentity({
  media,
  title,
  description,
  trailing,
}: {
  /** The avatar or icon, sized by the caller to 48px. */
  media: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  /** Inline status next to the name, e.g. an "Archived" badge. */
  trailing?: ReactNode;
}) {
  return (
    <div className="flex items-center gap-3">
      <div className="shrink-0">{media}</div>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <h1 className="min-w-0 truncate text-title font-semibold">{title}</h1>
          {trailing}
        </div>
        {description ? (
          <p className="mt-0.5 line-clamp-2 text-body text-muted-foreground">{description}</p>
        ) : null}
      </div>
    </div>
  );
}

/** The 48px square mark for objects that have an icon rather than an avatar. */
export function ObjectIconMark({ icon: Icon, className }: { icon: IconComponent; className?: string }) {
  return (
    <div
      className={cn(
        "flex size-12 items-center justify-center rounded-xl border bg-muted/50 text-muted-foreground",
        className,
      )}
    >
      <Icon className="size-5" />
    </div>
  );
}

/**
 * One part of an object's body. Its actions sit at the right of its title —
 * Edit, Add, View all — so the header never has to carry them.
 */
export function ObjectSection({
  title,
  count,
  action,
  children,
  className,
}: {
  title: ReactNode;
  count?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("space-y-2", className)}>
      <div className="flex h-7 items-center gap-2">
        <h2 className="text-body font-medium">{title}</h2>
        {count !== undefined && count !== null ? (
          <span className="text-caption tabular-nums text-muted-foreground">{count}</span>
        ) : null}
        {action ? <div className="ml-auto flex items-center gap-1">{action}</div> : null}
      </div>
      {children}
    </section>
  );
}

/** A quiet text action for a section title: icon plus a short verb. */
export function ObjectSectionAction({
  icon: Icon,
  children,
  className,
  ...props
}: Omit<ComponentProps<typeof Button>, "variant" | "size"> & { icon?: IconComponent }) {
  return (
    <Button
      variant="ghost"
      size="sm"
      className={cn("h-7 gap-1.5 px-2 text-muted-foreground", className)}
      {...props}
    >
      {Icon ? <Icon className="size-3.5" /> : null}
      {children}
    </Button>
  );
}

/** The bordered box a section's rows sit in; rows divide with hairlines. */
export function ObjectSectionCard({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("divide-y overflow-hidden rounded-lg border bg-card", className)}>
      {children}
    </div>
  );
}

/**
 * A section with nothing in it yet. The section's heading already names the
 * object, so this is a quiet row rather than a centered page state: what will
 * appear here, or what to do first, with the action at its end.
 */
export function ObjectSectionEmpty({
  icon: Icon,
  title,
  description,
  action,
  className,
}: {
  icon?: IconComponent;
  title?: ReactNode;
  description: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      data-mc="empty-state"
      data-variant="section"
      className={cn(
        "flex min-h-13 items-center gap-3 rounded-lg border border-dashed border-input py-2.5 pr-2.5 pl-3.5 text-muted-foreground",
        className,
      )}
    >
      {Icon ? <Icon aria-hidden="true" className="size-4.5 shrink-0 text-faint-foreground" /> : null}
      <div className="min-w-0 flex-1 text-label">
        {title ? <p className="text-body font-medium text-foreground">{title}</p> : null}
        <p>{description}</p>
      </div>
      {action ? <div className="flex shrink-0 items-center gap-1">{action}</div> : null}
    </div>
  );
}

/** A titled group in the properties panel, collapsible like the issue page's. */
export function ObjectPanelSection({
  title,
  children,
  defaultOpen = true,
}: {
  title: ReactNode;
  children: ReactNode;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        className={cn(
          "mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors hover:bg-accent/70",
          !open && "text-muted-foreground hover:text-foreground",
        )}
        onClick={() => setOpen(!open)}
      >
        {title}
        <ChevronRight
          className={cn(
            "!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
        />
      </button>
      {open ? <div className="space-y-0.5 pl-2">{children}</div> : null}
    </div>
  );
}

/**
 * One label and value in the properties panel; the value may be a picker.
 *
 * Top-aligned like `PropRow`: the label is one 32px line and the value centres
 * within at least that height, so a value that wraps (several label chips)
 * keeps the label beside its first line.
 */
export function ObjectProperty({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div className="-mx-2 flex min-h-8 items-start gap-2 rounded-md px-2 transition-colors hover:bg-accent/50">
      <TruncatedTooltip content={label} focusable>
        <span className="w-20 shrink-0 truncate text-caption leading-8 text-muted-foreground">{label}</span>
      </TruncatedTooltip>
      <div className="flex min-h-8 min-w-0 flex-1 items-center gap-1.5 truncate text-caption">{children}</div>
    </div>
  );
}
