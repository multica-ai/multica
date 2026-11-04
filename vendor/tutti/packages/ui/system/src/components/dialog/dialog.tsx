import * as React from "react";
import { Dialog as DialogPrimitive } from "radix-ui";

import { Button } from "#components/button";
import { CloseIcon } from "#icons/system-icons";
import { cn } from "#lib/utils";
import { getPortalContainer } from "../../lib/portal-container";

const dialogCloseDurationMs = 150;

const DialogMotionContext = React.createContext<{ open: boolean }>({
  open: false
});

function Dialog({
  defaultOpen,
  onOpenChange,
  open,
  ...props
}: Omit<React.ComponentProps<typeof DialogPrimitive.Root>, "onOpenChange"> & {
  onOpenChange?: (open: boolean) => void;
}) {
  const [uncontrolledOpen, setUncontrolledOpen] = React.useState(
    defaultOpen ?? false
  );
  const currentOpen = open ?? uncontrolledOpen;

  const handleOpenChange = React.useCallback(
    (nextOpen: boolean) => {
      if (open === undefined) {
        setUncontrolledOpen(nextOpen);
      }
      onOpenChange?.(nextOpen);
    },
    [onOpenChange, open]
  );

  return (
    <DialogMotionContext.Provider value={{ open: currentOpen }}>
      <DialogPrimitive.Root
        data-slot="dialog"
        open={currentOpen}
        onOpenChange={handleOpenChange}
        {...props}
      />
    </DialogMotionContext.Provider>
  );
}

function DialogTrigger({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Trigger>) {
  return <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />;
}

function DialogPortal({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Portal>) {
  return <DialogPrimitive.Portal container={getPortalContainer()} data-slot="dialog-portal" {...props} />;
}

function DialogClose({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Close>) {
  return <DialogPrimitive.Close data-slot="dialog-close" {...props} />;
}

function DialogOverlay({
  className,
  style,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Overlay>) {
  return (
    <DialogPrimitive.Overlay
      data-slot="dialog-overlay"
      className={cn(
        "fixed inset-0 isolate bg-[var(--backdrop)] duration-150 supports-backdrop-filter:backdrop-blur-sm data-open:animate-in data-open:fade-in-0 data-closed:animate-out data-closed:fade-out-0",
        className
      )}
      style={{ zIndex: "var(--z-dialog-overlay)", ...style }}
      {...props}
    />
  );
}

function DialogContent({
  className,
  children,
  overlayClassName,
  portaled = true,
  showCloseButton = true,
  style,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Content> & {
  overlayClassName?: string;
  portaled?: boolean;
  showCloseButton?: boolean;
}) {
  const { open } = React.useContext(DialogMotionContext);
  const [mounted, setMounted] = React.useState(open);

  React.useEffect(() => {
    if (open) {
      setMounted(true);
      return undefined;
    }

    const timeout = window.setTimeout(() => {
      setMounted(false);
    }, dialogCloseDurationMs);

    return () => {
      window.clearTimeout(timeout);
    };
  }, [open]);

  if (!mounted) {
    return null;
  }

  const content = (
    <>
      <DialogOverlay className={overlayClassName} forceMount />
      <DialogPrimitive.Content
        data-slot="dialog-content"
        forceMount
        className={cn(
          portaled ? "fixed" : "absolute",
          "nodrag pointer-events-none top-1/2 left-1/2 grid w-full max-w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 origin-center gap-3 rounded-[16px] border border-border-1 bg-[var(--background-panel)] p-[18px] text-[13px] text-foreground shadow-panel outline-none ease-[cubic-bezier(0.22,1,0.36,1)] will-change-[transform,opacity] [-webkit-app-region:no-drag] sm:max-w-[360px] data-[state=closed]:!pointer-events-none data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-[0.96] data-[state=closed]:duration-[150ms] data-[state=open]:pointer-events-auto data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-[0.96] data-[state=open]:duration-[250ms] motion-reduce:animate-none",
          className
        )}
        style={{ zIndex: "var(--z-dialog)", ...style }}
        {...props}
      >
        {children}
        {showCloseButton && (
          <DialogPrimitive.Close data-slot="dialog-close" asChild>
            <Button
              variant="ghost"
              className="absolute top-3 right-3"
              size="icon-sm"
            >
              <CloseIcon />
              <span className="sr-only">Close</span>
            </Button>
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Content>
    </>
  );

  if (!portaled) {
    return content;
  }

  return <DialogPortal forceMount>{content}</DialogPortal>;
}

function DialogHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-header"
      className={cn("flex flex-col gap-2 pr-8", className)}
      {...props}
    />
  );
}

function DialogFooter({
  className,
  showCloseButton = false,
  children,
  ...props
}: React.ComponentProps<"div"> & {
  showCloseButton?: boolean;
}) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn(
        "flex flex-col-reverse gap-2 pt-1 sm:flex-row sm:justify-end sm:gap-2.5",
        className
      )}
      {...props}
    >
      {children}
      {showCloseButton && (
        <DialogPrimitive.Close asChild>
          <Button variant="ghost" size="dialog">
            Close
          </Button>
        </DialogPrimitive.Close>
      )}
    </div>
  );
}

function DialogTitle({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      data-slot="dialog-title"
      className={cn(
        "text-[15px] leading-[1.35] font-semibold tracking-normal text-foreground",
        className
      )}
      {...props}
    />
  );
}

function DialogDescription({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      data-slot="dialog-description"
      className={cn(
        "text-[13px] font-[400] leading-[1.3] text-text-secondary *:[a]:underline *:[a]:underline-offset-3 *:[a]:hover:text-foreground",
        className
      )}
      {...props}
    />
  );
}

export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
  DialogTrigger
};
