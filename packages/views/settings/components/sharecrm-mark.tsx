// The ShareCRM (纷享销客) channel mark. lucide-react carries no brand icons, so
// the Settings / Agent Integrations row used the same MessagesSquare speech
// bubble Slack and WeCom used to share — nothing on the page said which
// platform it was (#6585). Official ShareCRM artwork is not published under a
// license we can vendor the way Semi Design / TDesign marks are, so this is an
// original geometric identifier (a conversation card with a folded CRM corner)
// rather than a copy of the vendor wordmark. Same 24x24 box and currentColor
// fill as WecomMark next door.

export function ShareCRMMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      data-testid="sharecrm-mark"
      className={className}
      fill="currentColor"
    >
      <path d="M7.5 3h6.85c.4 0 .78.16 1.06.44l4.15 4.2c.28.28.44.66.44 1.06v6.8A3.5 3.5 0 0 1 16.5 19h-3.05l-2.9 2.42c-.5.42-1.25.07-1.25-.58V19H7.5A3.5 3.5 0 0 1 4 15.5v-9A3.5 3.5 0 0 1 7.5 3zm7.15 2H7.5A1.5 1.5 0 0 0 6 6.5v9A1.5 1.5 0 0 0 7.5 17h2.2v2.05L12.2 17H16.5A1.5 1.5 0 0 0 18 15.5V10h-2.35A1.5 1.5 0 0 1 14.15 8.5V5zM16.15 5v3.5h3.4L16.15 5z" />
    </svg>
  );
}
