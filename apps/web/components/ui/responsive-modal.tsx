"use client";

import * as React from "react";
import { useIsMobile } from "@/hooks/use-mobile";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogFooter,
  DialogTitle,
  DialogDescription,
  DialogClose,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Drawer,
  DrawerContent,
  DrawerHeader,
  DrawerFooter,
  DrawerTitle,
  DrawerDescription,
  DrawerClose,
  DrawerTrigger,
} from "@/components/ui/drawer";
import { cn } from "@/lib/utils";

// ---------------------------------------------------------------------------
// ResponsiveModal — renders Dialog on desktop, bottom Drawer on mobile
// ---------------------------------------------------------------------------

interface ResponsiveModalProps {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  children: React.ReactNode;
}

function ResponsiveModal({ open, onOpenChange, children }: ResponsiveModalProps) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return (
      <Drawer open={open} onOpenChange={onOpenChange}>
        {children}
      </Drawer>
    );
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {children}
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Trigger
// ---------------------------------------------------------------------------

function ResponsiveModalTrigger({
  children,
  ...props
}: React.ComponentProps<typeof DialogTrigger>) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return <DrawerTrigger {...(props as React.ComponentProps<typeof DrawerTrigger>)}>{children}</DrawerTrigger>;
  }

  return <DialogTrigger {...props}>{children}</DialogTrigger>;
}

// ---------------------------------------------------------------------------
// Content
// ---------------------------------------------------------------------------

interface ResponsiveModalContentProps {
  className?: string;
  children: React.ReactNode;
  showCloseButton?: boolean;
}

function ResponsiveModalContent({
  className,
  children,
  showCloseButton = true,
}: ResponsiveModalContentProps) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return (
      <DrawerContent className={cn("max-h-[90vh]", className)}>
        {children}
      </DrawerContent>
    );
  }

  return (
    <DialogContent className={className} showCloseButton={showCloseButton}>
      {children}
    </DialogContent>
  );
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

function ResponsiveModalHeader({
  className,
  ...props
}: React.ComponentProps<"div">) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return <DrawerHeader className={className} {...props} />;
  }

  return <DialogHeader className={className} {...props} />;
}

// ---------------------------------------------------------------------------
// Footer
// ---------------------------------------------------------------------------

function ResponsiveModalFooter({
  className,
  ...props
}: React.ComponentProps<"div">) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return <DrawerFooter className={className} {...props} />;
  }

  return <DialogFooter className={className} {...props} />;
}

// ---------------------------------------------------------------------------
// Title
// ---------------------------------------------------------------------------

function ResponsiveModalTitle({
  className,
  children,
  ...props
}: React.ComponentProps<"h2"> & { className?: string }) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return (
      <DrawerTitle className={className} {...(props as React.ComponentProps<typeof DrawerTitle>)}>
        {children}
      </DrawerTitle>
    );
  }

  return (
    <DialogTitle className={className} {...(props as React.ComponentProps<typeof DialogTitle>)}>
      {children}
    </DialogTitle>
  );
}

// ---------------------------------------------------------------------------
// Description
// ---------------------------------------------------------------------------

function ResponsiveModalDescription({
  className,
  children,
  ...props
}: React.ComponentProps<"p"> & { className?: string }) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return (
      <DrawerDescription className={className} {...(props as React.ComponentProps<typeof DrawerDescription>)}>
        {children}
      </DrawerDescription>
    );
  }

  return (
    <DialogDescription className={className} {...(props as React.ComponentProps<typeof DialogDescription>)}>
      {children}
    </DialogDescription>
  );
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

function ResponsiveModalClose({
  children,
  ...props
}: React.ComponentProps<typeof DialogClose>) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return <DrawerClose {...(props as React.ComponentProps<typeof DrawerClose>)}>{children}</DrawerClose>;
  }

  return <DialogClose {...props}>{children}</DialogClose>;
}

export {
  ResponsiveModal,
  ResponsiveModalTrigger,
  ResponsiveModalContent,
  ResponsiveModalHeader,
  ResponsiveModalFooter,
  ResponsiveModalTitle,
  ResponsiveModalDescription,
  ResponsiveModalClose,
};
