import type { ReactNode } from "react";

/**
 * "Current issue · MUL-12 Fix login". The object a group acts on is named in
 * its heading, not in the input: the input always searches everything.
 */
export function ContextHeading({
  label,
  subject,
  detail,
  icon,
}: {
  label: string;
  subject?: string;
  detail?: string;
  icon?: ReactNode;
}) {
  return (
    <>
      <span className="shrink-0">{label}</span>
      {(subject || detail) && <span aria-hidden="true">·</span>}
      {icon}
      {subject && <span className="shrink-0 font-semibold text-foreground">{subject}</span>}
      {detail && <span className="min-w-0 truncate font-normal">{detail}</span>}
    </>
  );
}
