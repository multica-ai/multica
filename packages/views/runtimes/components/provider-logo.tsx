import { useId } from "react";
import { Monitor } from "lucide-react";

// Claude (Anthropic) — official mark, sourced from Bootstrap Icons (bi-claude)
function ClaudeLogo({ className }: { className: string }) {
  return (
    <svg viewBox="0 0 16 16" fill="#D97757" className={className}>
      <path d="m3.127 10.604 3.135-1.76.053-.153-.053-.085H6.11l-.525-.032-1.791-.048-1.554-.065-1.505-.08-.38-.081L0 7.832l.036-.234.32-.214.455.04 1.009.069 1.513.105 1.097.064 1.626.17h.259l.036-.105-.089-.065-.068-.064-1.566-1.062-1.695-1.121-.887-.646-.48-.327-.243-.306-.104-.67.435-.48.585.04.15.04.593.456 1.267.981 1.654 1.218.242.202.097-.068.012-.049-.109-.181-.9-1.626-.96-1.655-.428-.686-.113-.411a2 2 0 0 1-.068-.484l.496-.674L4.446 0l.662.089.279.242.411.94.666 1.48 1.033 2.014.302.597.162.553.06.17h.105v-.097l.085-1.134.157-1.392.154-1.792.052-.504.25-.605.497-.327.387.186.319.456-.045.294-.19 1.23-.37 1.93-.243 1.29h.142l.161-.16.654-.868 1.097-1.372.484-.545.565-.601.363-.287h.686l.505.751-.226.775-.707.895-.585.759-.839 1.13-.524.904.048.072.125-.012 1.897-.403 1.024-.186 1.223-.21.553.258.06.263-.218.536-1.307.323-1.533.307-2.284.54-.028.02.032.04 1.029.098.44.024h1.077l2.005.15.525.346.315.424-.053.323-.807.411-3.631-.863-.872-.218h-.12v.073l.726.71 1.331 1.202 1.667 1.55.084.383-.214.302-.226-.032-1.464-1.101-.565-.497-1.28-1.077h-.084v.113l.295.432 1.557 2.34.08.718-.112.234-.404.141-.444-.08-.911-1.28-.94-1.44-.759-1.291-.093.053-.448 4.821-.21.246-.484.186-.403-.307-.214-.496.214-.98.258-1.28.21-1.016.19-1.263.112-.42-.008-.028-.092.012-.953 1.307-1.448 1.957-1.146 1.227-.274.109-.477-.247.045-.44.266-.39 1.586-2.018.956-1.25.617-.723-.004-.105h-.036l-4.212 2.736-.75.096-.324-.302.04-.496.154-.162 1.267-.871z" />
    </svg>
  );
}

// Codex (OpenAI) — official mark, sourced from Bootstrap Icons (bi-openai)
function CodexLogo({ className }: { className: string }) {
  return (
    <svg viewBox="0 0 16 16" fill="currentColor" className={className}>
      <path d="M14.949 6.547a3.94 3.94 0 0 0-.348-3.273 4.11 4.11 0 0 0-4.4-1.934A4.1 4.1 0 0 0 8.423.2 4.15 4.15 0 0 0 6.305.086a4.1 4.1 0 0 0-1.891.948 4.04 4.04 0 0 0-1.158 1.753 4.1 4.1 0 0 0-1.563.679A4 4 0 0 0 .554 4.72a3.99 3.99 0 0 0 .502 4.731 3.94 3.94 0 0 0 .346 3.274 4.11 4.11 0 0 0 4.402 1.933c.382.425.852.764 1.377.995.526.231 1.095.35 1.67.346 1.78.002 3.358-1.132 3.901-2.804a4.1 4.1 0 0 0 1.563-.68 4 4 0 0 0 1.14-1.253 3.99 3.99 0 0 0-.506-4.716m-6.097 8.406a3.05 3.05 0 0 1-1.945-.694l.096-.054 3.23-1.838a.53.53 0 0 0 .265-.455v-4.49l1.366.778q.02.011.025.035v3.722c-.003 1.653-1.361 2.992-3.037 2.996m-6.53-2.75a2.95 2.95 0 0 1-.36-2.01l.095.057L5.29 12.09a.53.53 0 0 0 .527 0l3.949-2.246v1.555a.05.05 0 0 1-.022.041L6.473 13.3c-1.454.826-3.311.335-4.15-1.098m-.85-6.94A3.02 3.02 0 0 1 3.07 3.949v3.785a.51.51 0 0 0 .262.451l3.93 2.237-1.366.779a.05.05 0 0 1-.048 0L2.585 9.342a2.98 2.98 0 0 1-1.113-4.094zm11.216 2.571L8.747 5.576l1.362-.776a.05.05 0 0 1 .048 0l3.265 1.86a3 3 0 0 1 1.173 1.207 2.96 2.96 0 0 1-.27 3.2 3.05 3.05 0 0 1-1.36.997V8.279a.52.52 0 0 0-.276-.445m1.36-2.015-.097-.057-3.226-1.855a.53.53 0 0 0-.53 0L6.249 6.153V4.598a.04.04 0 0 1 .019-.04L9.533 2.7a3.07 3.07 0 0 1 3.257.139c.474.325.843.778 1.066 1.303.223.526.289 1.103.191 1.664zM5.503 8.575 4.139 7.8a.05.05 0 0 1-.026-.037V4.049c0-.57.166-1.127.476-1.607s.752-.864 1.275-1.105a3.08 3.08 0 0 1 3.234.41l-.096.054-3.23 1.838a.53.53 0 0 0-.265.455zm.742-1.577 1.758-1 1.762 1v2l-1.755 1-1.762-1z" />
    </svg>
  );
}

// OpenCode — official pixel-art "O" mark from anomalyco/opencode brand assets
function OpenCodeLogo({ className }: { className: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className}>
      <path d="M18 18H6V6H18V18Z" fill="#CFCECD" />
      <path d="M18 3H6V18H18V3ZM24 24H0V0H24V24Z" fill="#656363" />
    </svg>
  );
}

// OpenClaw — lobster mascot, vector version based on official branding
function OpenClawLogo({ className }: { className: string }) {
  return (
    <svg viewBox="0 0 16 16" fill="none" className={className}>
      {/* Body */}
      <path
        d="M8 2C5.5 2 3.5 4 3.5 6.5S5 10.5 6.5 11v1.5H8V11c.3.1.7.1 1 0v1.5h1.5V11c1.5-.5 3-2.5 3-4.5S10.5 2 8 2Z"
        fill="#E8453A"
      />
      {/* Left claw */}
      <path
        d="M3.5 5.5C2 5 1 6 1.5 7s2 .5 2.2-.7"
        fill="#FF6B5A"
      />
      {/* Right claw */}
      <path
        d="M12.5 5.5c1.5-.5 2.5.5 2 1.5s-2 .5-2.2-.7"
        fill="#FF6B5A"
      />
      {/* Antennae */}
      <path d="M6.5 3Q5 1.2 4.3 1.5" stroke="#FF6B5A" strokeWidth="0.8" strokeLinecap="round" />
      <path d="M9.5 3Q11 1.2 11.7 1.5" stroke="#FF6B5A" strokeWidth="0.8" strokeLinecap="round" />
      {/* Eyes */}
      <circle cx="6.2" cy="5.2" r="0.9" fill="#050810" />
      <circle cx="9.8" cy="5.2" r="0.9" fill="#050810" />
      <circle cx="6.4" cy="5" r="0.3" fill="#00E5CC" />
      <circle cx="10" cy="5" r="0.3" fill="#00E5CC" />
    </svg>
  );
}

// Hermes (NousResearch) — official anime mascot, 48×48 webp embedded as data URI
const HERMES_ICON =
  "data:image/webp;base64,UklGRuYDAABXRUJQVlA4INoDAADQEwCdASowADAAPm0uk0ckIiGhKrqpWIANiWkAEyQea/fD8IewvGL5V7Nb3H8u/MA8G9rT+4flL+UfIe42/23GB8zPqF/o3E0UAP5h/h/+F6ZX+x5evnX/r+4T/K/67/u/zg7znogfrusMANZLkn1gvlY/vNsKubtj/9xLSzxTsLr7K9GLdFNs5rwtISRcPXvH4z57n2fg0XR3aQ2D+pPpycwyl7TwAAD+/2DbjivnePzfyHCsdOgJXKlUR/OgAkofD7K4AdmsPKyP5Ml4/4HBYmIm5/efn/H+X3IZtngyaUOvwbFuRS/1yODFYO3vf3qeXGgPdfgIROXd/EPT7K2jysfvY9N71+w6g2gBPs+P6lxYkPf6S9QfpvH/7Pp7i8xRh0nVDBTEQyczSz7V9hoqo4nDJuii+SfibZRR/d5zB+9jkcb1DNN7YnC5Y7+WfGrE3eseXt3hSm+NS5++m1MHbjsrd9z/Q4HPRP/C85Po41XObalGyIUcFUL2j2n3uI/Yh6U8r6trCUJFB4kT3fsv6+8ylX/d96y2hq869FCXLjq4YqEO8vs5BtT52sf7KyDxPAWkH/b06YbfVXf4/7y5THL6Sr/4mOrrY9P2LW81f05HHFN8n0jcyqKOH7AluMm0AHPgFyz8RVrfBdmnPiC2FLMQfNDte5yGFzGC3fMlDed/tS/PO3Q/hjsNLvAXUUjqHyCo3JeN69jyNgWjjf8iUqoBsXT+lJyp2r8p60ad1jxhNyTblyJwda8aWEw1hFDeGjpMGguDF66RL4c+ZO+PhculC6WxvCsZ7IPAsdD7/ywx3w3AowJ66hAAK7k+m6X2QV06OVOCwyIGERex/AUyuBbLUK93X58+M+Si7YfYjVYGpoJ7JvSgD8ExaA21z9OY+si+1wreacDanKnFDmhwBQC3t6MLeXCOGp3VURDKl10K7tdKHQcb4hr48ba+1x/MrMRwHfq3IQrDIXPYCg4b0OLnVN9JyXttKGM63B5imIdKuU0r6hhSslT10lGLjnIJuwO5WKR0RHs+BX5vs6H63y3K7IuuZ1eRN+Aczvbs4QuDs6ZRuzjJ/1DJ5R/3ZrFPrtxvMwT06vAXIgcbhLGNLhOQUYRPdUN5MgyCtL5NH71ArTPLRRkIjhGwoCYXKKqlqIKKT9NX3vwp/nlh4SX71dlYg/mPXbJ9bMeVugyjqFahjFTJ/rT3HtBCWG8h+OvvbOFDFKurCG9BOhO9B719OS7zsP0KPqoymnv7hVvoJyZp0iziCbBvaJpmF9Cvfs8/vWqWr7TUo616WfMW+X9nkgpuqtnfAAAAAA==";

function HermesLogo({ className }: { className: string }) {
  return (
    <img
      src={HERMES_ICON}
      alt="Hermes"
      className={`${className} rounded-sm`}
    />
  );
}

// Pi (pi.dev) — official pixel-art "pi" wordmark, sourced from pi.dev/logo.svg
function PiLogo({ className }: { className: string }) {
  return (
    <svg viewBox="0 0 800 800" fill="none" className={className}>
      <rect width="800" height="800" rx="150" fill="#09090b" />
      <path
        fill="#fff"
        fillRule="evenodd"
        d="M165.29 165.29H517.36V400H400V517.36H282.65V634.72H165.29ZM282.65 282.65V400H400V282.65Z"
      />
      <path fill="#fff" d="M517.36 400H634.72V634.72H517.36Z" />
    </svg>
  );
}

// GitHub Copilot — GitHub mark (Invertocat)
function CopilotLogo({ className }: { className: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" className={className}>
      <path d="M12 1C5.9225 1 1 5.9225 1 12C1 16.8675 4.14875 20.9787 8.52125 22.4362C9.07125 22.5325 9.2775 22.2025 9.2775 21.9137C9.2775 21.6525 9.26375 20.7862 9.26375 19.865C6.5 20.3737 5.785 19.1912 5.565 18.5725C5.44125 18.2562 4.905 17.28 4.4375 17.0187C4.0525 16.8125 3.5025 16.3037 4.42375 16.29C5.29 16.2762 5.90875 17.0875 6.115 17.4175C7.105 19.0812 8.68625 18.6137 9.31875 18.325C9.415 17.61 9.70375 17.1287 10.02 16.8537C7.5725 16.5787 5.015 15.63 5.015 11.4225C5.015 10.2262 5.44125 9.23625 6.1425 8.46625C6.0325 8.19125 5.6475 7.06375 6.2525 5.55125C6.2525 5.55125 7.17375 5.2625 9.2775 6.67875C10.1575 6.43125 11.0925 6.3075 12.0275 6.3075C12.9625 6.3075 13.8975 6.43125 14.7775 6.67875C16.8813 5.24875 17.8025 5.55125 17.8025 5.55125C18.4075 7.06375 18.0225 8.19125 17.9125 8.46625C18.6138 9.23625 19.04 10.2125 19.04 11.4225C19.04 15.6437 16.4688 16.5787 14.0213 16.8537C14.42 17.1975 14.7638 17.8575 14.7638 18.8887C14.7638 20.36 14.75 21.5425 14.75 21.9137C14.75 22.2025 14.9563 22.5462 15.5063 22.4362C19.8513 20.9787 23 16.8537 23 12C23 5.9225 18.0775 1 12 1Z" />
    </svg>
  );
}

// Cursor — official brand logo from Cursor brand assets
function CursorLogo({ className }: { className: string }) {
  return (
    <svg viewBox="600 300 400 400" fill="none" className={className}>
      <path fill="#14120B" d="M999.994 554.294C999.994 559.859 999.994 565.419 999.962 570.984C999.935 575.67 999.882 580.357 999.753 585.038C999.475 595.247 998.875 605.542 997.059 615.639C995.217 625.88 992.212 635.409 987.477 644.718C982.822 653.861 976.738 662.233 969.485 669.491C962.227 676.748 953.861 682.828 944.712 687.482C935.409 692.217 925.875 695.222 915.633 697.065C905.537 698.88 895.242 699.48 885.033 699.759C880.346 699.887 875.665 699.941 870.978 699.968C865.413 700.005 859.853 700 854.288 700H745.695C740.13 700 734.571 700 729.005 699.968C724.319 699.941 719.632 699.887 714.951 699.759C704.742 699.48 694.447 698.88 684.35 697.065C674.109 695.222 664.58 692.217 655.271 687.482C646.128 682.828 637.756 676.743 630.499 669.491C623.241 662.233 617.161 653.866 612.507 644.718C607.772 635.414 604.767 625.88 602.925 615.639C601.109 605.542 600.509 595.247 600.23 585.038C600.102 580.352 600.048 575.67 600.021 570.984C600 565.419 600 559.859 600 554.294V445.701C600 440.136 600 434.576 600.032 429.011C600.059 424.324 600.112 419.637 600.241 414.956C600.52 404.747 601.119 394.452 602.935 384.356C604.778 374.115 607.783 364.586 612.518 355.277C617.172 346.133 623.257 337.762 630.509 330.504C637.767 323.246 646.133 317.167 655.282 312.512C664.586 307.777 674.12 304.772 684.361 302.93C694.458 301.114 704.752 300.514 714.961 300.236C719.648 300.107 724.329 300.054 729.016 300.027C734.576 300 740.136 300 745.701 300H854.294C859.859 300 865.419 300 870.984 300.032C875.67 300.059 880.357 300.112 885.038 300.241C895.247 300.52 905.542 301.119 915.639 302.935C925.88 304.778 935.409 307.783 944.718 312.518C953.861 317.172 962.233 323.257 969.491 330.509C976.748 337.767 982.828 346.133 987.482 355.282C992.217 364.586 995.222 374.12 997.065 384.361C998.88 394.458 999.48 404.752 999.759 414.961C999.887 419.648 999.941 424.329 999.968 429.016C1000.01 434.581 1000 440.141 1000 445.706V554.299L999.994 554.294Z"/>
      <path fill="#72716D" d="M800.004 500L923.821 571.486C923.061 572.804 921.957 573.929 920.591 574.716L804.863 641.531C801.858 643.266 798.151 643.266 795.146 641.531L679.417 574.716C678.052 573.929 676.948 572.804 676.188 571.486L800.004 500Z"/>
      <path fill="#55544F" d="M800.005 357.168V500L676.188 571.486C675.427 570.168 675.004 568.647 675.004 567.072V432.928C675.004 429.774 676.686 426.865 679.418 425.285L795.141 358.47C796.646 357.602 798.323 357.168 799.999 357.168H800.005Z"/>
      <path fill="#43413C" d="M923.815 428.515C923.055 427.197 921.951 426.072 920.586 425.285L804.857 358.47C803.357 357.602 801.68 357.168 800.004 357.168V500L923.821 571.486C924.581 570.168 925.005 568.647 925.005 567.072V432.928C925.005 431.348 924.587 429.838 923.821 428.515H923.815Z"/>
      <path fill="#D6D5D2" d="M915.156 433.518C915.857 434.728 915.954 436.281 915.156 437.663L802.764 632.323C802.008 633.641 800 633.1 800 631.584V503.311C800 502.287 799.727 501.302 799.229 500.44L915.15 433.512H915.156V433.518Z"/>
      <path fill="white" d="M915.155 433.518L799.233 500.445C798.741 499.588 798.023 498.86 797.134 498.345L686.049 434.209C684.731 433.453 685.272 431.445 686.788 431.445H911.566C913.162 431.445 914.459 432.307 915.155 433.518Z"/>
    </svg>
  );
}

// Kimi (Moonshot AI) — wordmark "K" mark in Moonshot brand purple, simple
// rounded-square logotype suitable for small icon sizes.
function KimiLogo({ className }: { className: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className}>
      <rect width="24" height="24" rx="5" fill="#1F1147" />
      <path
        d="M7.2 6h2.4v5.1l4.3-5.1h2.9l-4.4 5.1L17 18h-2.9l-3.2-5.2-1.3 1.5V18H7.2V6z"
        fill="#FFFFFF"
      />
    </svg>
  );
}

// Kiro CLI — official icon sourced from kiro.dev/icon.svg.
function KiroLogo({ className }: { className: string }) {
  const maskId = `kiro-logo-mask-${useId().replace(/:/g, "")}`;

  return (
    <svg viewBox="0 0 1200 1200" fill="none" className={className}>
      <rect width="1200" height="1200" rx="260" fill="#9046FF" />
      <mask
        id={maskId}
        style={{ maskType: "luminance" }}
        maskUnits="userSpaceOnUse"
        x="272"
        y="202"
        width="655"
        height="796"
      >
        <path
          d="M926.578 202.793H272.637V997.857H926.578V202.793Z"
          fill="white"
        />
      </mask>
      <g mask={`url(#${maskId})`}>
        <path
          d="M398.554 818.914C316.315 1001.03 491.477 1046.74 620.672 940.156C658.687 1059.66 801.052 970.473 852.234 877.795C964.787 673.567 919.318 465.357 907.64 422.374C827.637 129.443 427.623 128.946 358.8 423.865C342.651 475.544 342.402 534.18 333.458 595.051C328.986 625.86 325.507 645.488 313.83 677.785C306.873 696.424 297.68 712.819 282.773 740.645C259.915 783.881 269.604 867.113 387.87 823.883L399.051 818.914H398.554Z"
          fill="white"
        />
        <path
          d="M636.123 549.353C603.328 549.353 598.359 510.097 598.359 486.742C598.359 465.623 602.086 448.977 609.293 438.293C615.504 428.852 624.697 424.131 636.123 424.131C647.555 424.131 657.492 428.852 664.447 438.541C672.398 449.474 676.623 466.12 676.623 486.742C676.623 525.998 661.471 549.353 636.375 549.353H636.123Z"
          fill="black"
        />
        <path
          d="M771.24 549.353C738.445 549.353 733.477 510.097 733.477 486.742C733.477 465.623 737.203 448.977 744.41 438.293C750.621 428.852 759.814 424.131 771.24 424.131C782.672 424.131 792.609 428.852 799.564 438.541C807.516 449.474 811.74 466.12 811.74 486.742C811.74 525.998 796.588 549.353 771.492 549.353H771.24Z"
          fill="black"
        />
      </g>
    </svg>
  );
}

// CodeBuddy (Tencent Cloud) — official logo from codebuddy.cn
function CodeBuddyLogo({ className }: { className: string }) {
  const id = useId().replace(/:/g, "");
  const clipId = `cb-clip-${id}`;
  const f0 = `cb-f0-${id}`;
  const f1 = `cb-f1-${id}`;
  const g0 = `cb-g0-${id}`;
  const g1 = `cb-g1-${id}`;

  return (
    <svg viewBox="0 0 40 40" fill="none" className={className}>
      <g clipPath={`url(#${clipId})`}>
        <rect width="40" height="40" rx="20" fill={`url(#${g0})`} />
        <g filter={`url(#${f0})`}>
          <circle cx="12.607" cy="34.979" r="14.081" fill="#32E6B9" fillOpacity="0.4" />
        </g>
        <g filter={`url(#${f1})`}>
          <circle cx="33.34" cy="43.619" r="12.87" fill="#32E6B9" />
        </g>
        <path
          d="M27.6897 3.3426C28.057 3.01322 28.0795 2.99983 28.3485 2.98366C28.7851 2.95177 29.1858 3.16107 29.8664 3.78072C31.4566 5.22574 33.6703 8.19724 35.047 10.7343L35.5796 11.7191L36.3309 12.0927C37.0562 12.4593 38.2459 13.2111 38.7428 13.614C38.9673 13.7997 38.9992 13.8032 39.2326 13.7124C40.2872 13.3017 41.7982 13.846 43.1307 15.1244C44.3302 16.274 45.479 18.2384 45.9191 19.8778C45.9833 20.1417 46.0683 20.709 46.0994 21.1314C46.1997 22.6144 45.7243 23.7986 44.8085 24.3349C44.6213 24.443 44.6085 24.4725 44.6137 24.939C44.6559 27.1604 44.0571 29.3777 42.8543 31.54C41.4965 33.9678 39.0794 36.4798 35.8073 38.846C34.0502 40.1247 29.8918 42.5469 28.0122 43.3973C23.5096 45.4244 19.8999 46.2022 16.7645 45.8181C14.8943 45.5915 12.7771 44.8611 11.5246 44.0137C11.1951 43.786 11.1428 43.7719 10.8909 43.8439C9.55017 44.2289 7.79442 43.4374 6.30272 41.7817C5.70771 41.1197 4.74709 39.4939 4.43575 38.6241C3.71569 36.5887 3.85859 34.752 4.81783 33.6551C5.0657 33.3725 5.07387 33.3607 5.01972 32.8856C4.93033 32.1079 4.88982 30.9566 4.93066 30.2138L4.96343 29.5205L3.92091 27.6777C2.3078 24.8074 1.28326 22.3968 0.887982 20.5554C0.679402 19.546 0.692781 19.0981 0.949061 18.7667C1.10529 18.5666 1.61622 18.3594 2.23252 18.2455C3.78518 17.9729 7.17203 18.2198 10.9394 18.885L11.3303 18.9525L12.1903 18.1921C13.6179 16.9275 14.5665 16.2176 16.315 15.1274C18.1373 13.9872 20.1943 13.0498 22.5103 12.3071L23.2533 12.0687L23.6615 10.9964C25.1239 7.13587 26.6218 4.28969 27.6897 3.3426ZM15.4393 23.1261C13.7864 24.0804 12.9602 24.5582 12.3529 25.093C9.89364 27.2585 8.9738 30.6888 10.0208 33.7938C10.2794 34.5606 10.7568 35.387 11.7111 37.0398C12.6653 38.6926 13.1424 39.5194 13.6771 40.1267C15.8426 42.5859 19.2732 43.5045 22.3783 42.4574C23.145 42.1989 23.972 41.7223 25.6248 40.768L35.1333 35.2783C36.7862 34.324 37.6124 33.8462 38.2196 33.3114C40.6789 31.1459 41.5988 27.7156 40.5518 24.6105C40.2932 23.8438 39.8157 23.0174 38.8615 21.3646C37.9072 19.7117 37.4302 18.885 36.8955 18.2777C34.7299 15.8185 31.2993 14.8999 28.1943 15.947C27.4275 16.2055 26.6006 16.6821 24.9478 17.6364L15.4393 23.1261Z"
          fill={`url(#${g1})`}
        />
        <rect x="15.882" y="30.027" width="4.009" height="8.326" rx="2.005" transform="rotate(-30 15.882 30.027)" fill="white" />
        <rect x="26.698" y="23.781" width="4.009" height="8.326" rx="2.005" transform="rotate(-30 26.698 23.781)" fill="white" />
      </g>
      <defs>
        <filter id={f0} x="-14.916" y="7.456" width="55.047" height="55.047" filterUnits="userSpaceOnUse" colorInterpolationFilters="sRGB">
          <feFlood floodOpacity="0" result="BackgroundImageFix" />
          <feBlend mode="normal" in="SourceGraphic" in2="BackgroundImageFix" result="shape" />
          <feGaussianBlur stdDeviation="6.721" result="effect1_foregroundBlur" />
        </filter>
        <filter id={f1} x="9.47" y="19.748" width="47.742" height="47.741" filterUnits="userSpaceOnUse" colorInterpolationFilters="sRGB">
          <feFlood floodOpacity="0" result="BackgroundImageFix" />
          <feBlend mode="normal" in="SourceGraphic" in2="BackgroundImageFix" result="shape" />
          <feGaussianBlur stdDeviation="5.5" result="effect1_foregroundBlur" />
        </filter>
        <linearGradient id={g0} x1="20" y1="0" x2="20" y2="40" gradientUnits="userSpaceOnUse">
          <stop stopColor="#6C4DFF" />
          <stop offset="1" stopColor="#583ED3" />
        </linearGradient>
        <linearGradient id={g1} x1="14.639" y1="10.792" x2="32.171" y2="41.159" gradientUnits="userSpaceOnUse">
          <stop stopColor="white" stopOpacity="0.8" />
          <stop offset="0.438" stopColor="white" />
        </linearGradient>
        <clipPath id={clipId}>
          <rect width="40" height="40" rx="20" fill="white" />
        </clipPath>
      </defs>
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
    case "hermes":
      return <HermesLogo className={className} />;
    case "pi":
      return <PiLogo className={className} />;
    case "copilot":
      return <CopilotLogo className={className} />;
    case "cursor":
      return <CursorLogo className={className} />;
    case "kimi":
      return <KimiLogo className={className} />;
    case "kiro":
      return <KiroLogo className={className} />;
    case "codebuddy":
      return <CodeBuddyLogo className={className} />;
    default:
      return <Monitor className={className} />;
  }
}
