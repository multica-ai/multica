"use client";

export function SidebarSection({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="mt-3">
      <div className="px-2 pb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground/60">
        {title}
      </div>
      {children}
    </div>
  );
}
