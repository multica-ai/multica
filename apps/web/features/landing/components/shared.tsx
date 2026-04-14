import { cn } from "@multica/ui/lib/utils";

export const githubUrl = "https://github.com/multica-ai/multica";
export const twitterUrl = "https://x.com/MulticaAI";

export function GitHubMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      aria-hidden="true"
      className={className}
      fill="currentColor"
    >
      <path d="M8 0C3.58 0 0 3.58 0 8a8 8 0 0 0 5.47 7.59c.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2 .37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82A7.65 7.65 0 0 1 8 4.84c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  );
}

export function XMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      className={className}
      fill="currentColor"
    >
      <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
    </svg>
  );
}

export function ImageIcon({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      className={className}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <rect x="3.5" y="5" width="17" height="14" rx="2.5" />
      <circle cx="9" cy="10" r="1.6" />
      <path d="m20.5 16-4.8-4.8a1 1 0 0 0-1.4 0L8 17.5" />
      <path d="m11.5 14.5 1.8-1.8a1 1 0 0 1 1.4 0l2.8 2.8" />
    </svg>
  );
}

export function ClaudeCodeLogo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 248 248"
      aria-hidden="true"
      className={className}
      fill="currentColor"
    >
      <path d="M52.4285 162.873L98.7844 136.879L99.5485 134.602L98.7844 133.334H96.4921L88.7237 132.862L62.2346 132.153L39.3113 131.207L17.0249 130.026L11.4214 128.844L6.2 121.873L6.7094 118.447L11.4214 115.257L18.171 115.847L33.0711 116.911L55.485 118.447L71.6586 119.392L95.728 121.873H99.5485L100.058 120.337L98.7844 119.392L97.7656 118.447L74.5877 102.732L49.4995 86.1905L36.3823 76.62L29.3779 71.7757L25.8121 67.2858L24.2839 57.3608L30.6515 50.2716L39.3113 50.8623L41.4763 51.4531L50.2636 58.1879L68.9842 72.7209L93.4357 90.6804L97.0015 93.6343L98.4374 92.6652L98.6571 91.9801L97.0015 89.2625L83.757 65.2772L69.621 40.8192L63.2534 30.6579L61.5978 24.632C60.9565 22.1032 60.579 20.0111 60.579 17.4246L67.8381 7.49965L71.9133 6.19995L81.7193 7.49965L85.7946 11.0443L91.9074 24.9865L101.714 46.8451L116.996 76.62L121.453 85.4816L123.873 93.6343L124.764 96.1155H126.292V94.6976L127.566 77.9197L129.858 57.3608L132.15 30.8942L132.915 23.4505L136.608 14.4708L143.994 9.62643L149.725 12.344L154.437 19.0788L153.8 23.4505L150.998 41.6463L145.522 70.1215L141.957 89.2625H143.994L146.414 86.7813L156.093 74.0206L172.266 53.698L179.398 45.6635L187.803 36.802L193.152 32.5484H203.34L210.726 43.6549L207.415 55.1159L196.972 68.3492L188.312 79.5739L175.896 96.2095L168.191 109.585L168.882 110.689L170.738 110.53L198.755 104.504L213.91 101.787L231.994 98.7149L240.144 102.496L241.036 106.395L237.852 114.311L218.495 119.037L195.826 123.645L162.07 131.592L161.696 131.893L162.137 132.547L177.36 133.925L183.855 134.279H199.774L229.447 136.524L237.215 141.605L241.8 147.867L241.036 152.711L229.065 158.737L213.019 154.956L175.45 145.977L162.587 142.787H160.805V143.85L171.502 154.366L191.242 172.089L215.82 195.011L217.094 200.682L213.91 205.172L210.599 204.699L188.949 188.394L180.544 181.069L161.696 165.118H160.422V166.772L164.752 173.152L187.803 207.771L188.949 218.405L187.294 221.832L181.308 223.959L174.813 222.777L161.187 203.754L147.305 182.486L136.098 163.345L134.745 164.2L128.075 235.42L125.019 239.082L117.887 241.8L111.902 237.31L108.718 229.984L111.902 215.452L115.722 196.547L118.779 181.541L121.58 162.873L123.291 156.636L123.14 156.219L121.773 156.449L107.699 175.752L86.304 204.699L69.3663 222.777L65.291 224.431L58.2867 220.768L58.9235 214.27L62.8713 208.48L86.304 178.705L100.44 160.155L109.551 149.507L109.462 147.967L108.959 147.924L46.6977 188.512L35.6182 189.93L30.7788 185.44L31.4156 178.115L33.7079 175.752L52.4285 162.873Z" />
    </svg>
  );
}

export function CodexLogo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      className={className}
      fill="currentColor"
    >
      <path d="M22.282 9.821a5.985 5.985 0 0 0-.516-4.91 6.046 6.046 0 0 0-6.51-2.9A6.065 6.065 0 0 0 4.981 4.18a5.985 5.985 0 0 0-3.998 2.9 6.046 6.046 0 0 0 .743 7.097 5.98 5.98 0 0 0 .51 4.911 6.051 6.051 0 0 0 6.515 2.9A5.985 5.985 0 0 0 13.26 24a6.056 6.056 0 0 0 5.772-4.206 5.99 5.99 0 0 0 3.997-2.9 6.056 6.056 0 0 0-.747-7.073ZM13.26 22.43a4.476 4.476 0 0 1-2.876-1.04l.141-.081 4.779-2.758a.795.795 0 0 0 .392-.681v-6.737l2.02 1.168a.071.071 0 0 1 .038.052v5.583a4.504 4.504 0 0 1-4.494 4.494ZM3.6 18.304a4.47 4.47 0 0 1-.535-3.014l.142.085 4.783 2.759a.771.771 0 0 0 .78 0l5.843-3.369v2.332a.08.08 0 0 1-.033.062L9.74 19.95a4.5 4.5 0 0 1-6.14-1.646ZM2.34 7.896a4.485 4.485 0 0 1 2.366-1.973V11.6a.766.766 0 0 0 .388.676l5.815 3.355-2.02 1.168a.076.076 0 0 1-.071 0l-4.83-2.786A4.504 4.504 0 0 1 2.34 7.872v.024Zm16.597 3.855-5.833-3.387L15.119 7.2a.076.076 0 0 1 .071 0l4.83 2.791a4.494 4.494 0 0 1-.676 8.105v-5.678a.79.79 0 0 0-.407-.667Zm2.01-3.023-.141-.085-4.774-2.782a.776.776 0 0 0-.785 0L9.409 9.23V6.897a.066.066 0 0 1 .028-.061l4.83-2.787a4.5 4.5 0 0 1 6.68 4.66v.018ZM8.318 12.898l-2.024-1.168a.074.074 0 0 1-.038-.052V6.095a4.494 4.494 0 0 1 7.37-3.456l-.14.081-4.78 2.758a.795.795 0 0 0-.392.681l-.014 6.739h.018Zm1.1-2.36 2.602-1.5 2.595 1.5v2.999l-2.595 1.5-2.602-1.5v-3Z" />
    </svg>
  );
}

export function OpenClawLogo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      aria-hidden="true"
      className={className}
      fill="none"
    >
      <g fill="#3a0a0d">
        <rect x="1" y="5" width="1" height="3" />
        <rect x="2" y="4" width="1" height="1" />
        <rect x="2" y="8" width="1" height="1" />
        <rect x="3" y="3" width="1" height="1" />
        <rect x="3" y="9" width="1" height="1" />
        <rect x="4" y="2" width="1" height="1" />
        <rect x="4" y="10" width="1" height="1" />
        <rect x="5" y="2" width="6" height="1" />
        <rect x="11" y="2" width="1" height="1" />
        <rect x="12" y="3" width="1" height="1" />
        <rect x="12" y="9" width="1" height="1" />
        <rect x="13" y="4" width="1" height="1" />
        <rect x="13" y="8" width="1" height="1" />
        <rect x="14" y="5" width="1" height="3" />
        <rect x="5" y="11" width="6" height="1" />
        <rect x="4" y="12" width="1" height="1" />
        <rect x="11" y="12" width="1" height="1" />
        <rect x="3" y="13" width="1" height="1" />
        <rect x="12" y="13" width="1" height="1" />
        <rect x="5" y="14" width="6" height="1" />
      </g>
      <g fill="#ff4f40">
        <rect x="5" y="3" width="6" height="1" />
        <rect x="4" y="4" width="8" height="1" />
        <rect x="3" y="5" width="10" height="1" />
        <rect x="3" y="6" width="10" height="1" />
        <rect x="3" y="7" width="10" height="1" />
        <rect x="4" y="8" width="8" height="1" />
        <rect x="5" y="9" width="6" height="1" />
        <rect x="5" y="12" width="6" height="1" />
        <rect x="6" y="13" width="4" height="1" />
      </g>
      <g fill="#ff775f">
        <rect x="1" y="6" width="2" height="1" />
        <rect x="2" y="5" width="1" height="1" />
        <rect x="2" y="7" width="1" height="1" />
        <rect x="13" y="6" width="2" height="1" />
        <rect x="13" y="5" width="1" height="1" />
        <rect x="13" y="7" width="1" height="1" />
      </g>
      <g fill="#081016">
        <rect x="6" y="5" width="1" height="1" />
        <rect x="9" y="5" width="1" height="1" />
      </g>
      <g fill="#f5fbff">
        <rect x="6" y="4" width="1" height="1" />
        <rect x="9" y="4" width="1" height="1" />
      </g>
    </svg>
  );
}

export function OpenCodeLogo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      className={className}
      fill="none"
    >
      <path d="M18 18H6V6H18V18Z" fill="#CFCECD" />
      <path d="M18 3H6V18H18V3ZM24 24H0V0H24V24Z" fill="#656363" />
    </svg>
  );
}

export function headerButtonClassName(
  tone: "ghost" | "solid",
  variant: "dark" | "light" = "dark",
) {
  return cn(
    "inline-flex items-center justify-center gap-2 rounded-[11px] px-4 py-2.5 text-[13px] font-semibold transition-colors",
    variant === "dark"
      ? tone === "solid"
        ? "bg-white text-[#0a0d12] hover:bg-white/92"
        : "border border-white/18 bg-black/16 text-white backdrop-blur-sm hover:bg-black/24"
      : tone === "solid"
        ? "bg-[#0a0d12] text-white hover:bg-[#0a0d12]/88"
        : "border border-[#0a0d12]/12 bg-white text-[#0a0d12] hover:bg-[#0a0d12]/5",
  );
}

export function heroButtonClassName(tone: "ghost" | "solid") {
  return cn(
    "inline-flex items-center justify-center gap-2 rounded-[12px] px-5 py-3 text-[14px] font-semibold transition-colors",
    tone === "solid"
      ? "bg-white text-[#0a0d12] hover:bg-white/92"
      : "border border-white/18 bg-black/16 text-white backdrop-blur-sm hover:bg-black/24",
  );
}
