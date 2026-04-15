import { cn } from "@multica/ui/lib/utils";

function FileDropOverlay({ className }: { className?: string }) {
  return (
    <div 
      className={cn(
        "absolute inset-0 z-50 rounded-xl border-2 border-dashed border-brand/30 bg-brand/[0.03] pointer-events-none transition-all",
        className
      )} 
    />
  );
}

export { FileDropOverlay };
