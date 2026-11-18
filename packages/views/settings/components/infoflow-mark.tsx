// The InfoFlow (如流) brand mark. Unlike the WeCom / Telegram marks next door
// this is NOT the vendor's own artwork: InfoFlow does not ship an icon we can
// vendor under a permissive licence the way TDesign and Simple Icons do. It is a
// neutral monogram — a rounded chat bubble carrying the platform's initial —
// chosen so the connect button reads as InfoFlow rather than as a generic
// lucide speech bubble (the same glyph Slack used, see WecomMark). Replace this
// with the official mark if a licensed copy is ever added. Same 24x24 box and
// currentColor paint as the other marks so it drops into a Button beside lucide
// icons without restyling.
export function InfoFlowMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      className={className}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinejoin="round"
      strokeLinecap="round"
    >
      <path d="M4 3.75h16A1.25 1.25 0 0 1 21.25 5v10A1.25 1.25 0 0 1 20 16.25h-7.4l-4.6 4v-4H4A1.25 1.25 0 0 1 2.75 15V5A1.25 1.25 0 0 1 4 3.75Z" />
      <path d="M12 7.25v5.5" />
      <circle cx="12" cy="14.9" r="0.9" fill="currentColor" stroke="none" />
    </svg>
  );
}