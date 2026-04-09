import { Monitor } from "lucide-react";

function ClaudeLogo({ className }: { className: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className}>
      <path
        d="M15.31 3.34L8.69 20.66"
        stroke="#D97757"
        strokeWidth="2.8"
        strokeLinecap="round"
      />
      <path
        d="M8.69 3.34L15.31 20.66"
        stroke="#D97757"
        strokeWidth="2.8"
        strokeLinecap="round"
      />
    </svg>
  );
}

function CodexLogo({ className }: { className: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className}>
      <path
        d="M12 2L3 7v10l9 5 9-5V7l-9-5z"
        stroke="#10A37F"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
      <path
        d="M12 22V12M3 7l9 5 9-5"
        stroke="#10A37F"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function OpenCodeLogo({ className }: { className: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className}>
      <path
        d="M9 6L3 12l6 6"
        stroke="#3B82F6"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M15 6l6 6-6 6"
        stroke="#3B82F6"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function OpenClawLogo({ className }: { className: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className}>
      <path
        d="M6 4c0 3 1 8 6 12"
        stroke="#8B5CF6"
        strokeWidth="2.2"
        strokeLinecap="round"
      />
      <path
        d="M12 4c0 3 0 8 0 12"
        stroke="#8B5CF6"
        strokeWidth="2.2"
        strokeLinecap="round"
      />
      <path
        d="M18 4c0 3-1 8-6 12"
        stroke="#8B5CF6"
        strokeWidth="2.2"
        strokeLinecap="round"
      />
      <path
        d="M6 20c2-1 4-2 6-2s4 1 6 2"
        stroke="#8B5CF6"
        strokeWidth="2.2"
        strokeLinecap="round"
      />
    </svg>
  );
}

export function ProviderLogo({
  provider,
  className = "h-4 w-4",
}: {
  provider: string;
  className?: string;
}) {
  switch (provider) {
    case "claude":
      return <ClaudeLogo className={className} />;
    case "codex":
      return <CodexLogo className={className} />;
    case "opencode":
      return <OpenCodeLogo className={className} />;
    case "openclaw":
      return <OpenClawLogo className={className} />;
    default:
      return <Monitor className={className} />;
  }
}
