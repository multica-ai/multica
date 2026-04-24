/**
 * Inline SVG marks for macOS / Windows / Linux.
 * Lucide lacks real Apple / Tux marks, and the download page needs
 * the recognizable brand glyphs next to platform rows. Kept as
 * minimal monochrome outlines so they inherit currentColor.
 */

type IconProps = React.SVGProps<SVGSVGElement> & { size?: number };

export function AppleIcon({ size = 18, ...props }: IconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="currentColor"
      aria-hidden
      {...props}
    >
      <path d="M16.37 12.8c.02-1.9 1.56-2.83 1.63-2.87-.89-1.3-2.28-1.48-2.77-1.5-1.18-.12-2.3.69-2.9.69-.6 0-1.52-.68-2.5-.66-1.28.02-2.47.74-3.13 1.88-1.33 2.3-.34 5.7.96 7.57.63.92 1.38 1.94 2.36 1.9.95-.04 1.31-.61 2.45-.61 1.14 0 1.47.61 2.47.59 1.02-.02 1.66-.93 2.29-1.84.72-1.06 1.02-2.1 1.04-2.15-.02-.01-2-.77-2.02-3.05-.02-1.9 1.55-2.81 1.63-2.87zm-2.05-5.24c.52-.63.88-1.52.78-2.4-.75.03-1.66.5-2.2 1.12-.48.55-.9 1.44-.79 2.32.84.06 1.69-.42 2.21-1.04z" />
    </svg>
  );
}

export function WindowsIcon({ size = 18, ...props }: IconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="currentColor"
      aria-hidden
      {...props}
    >
      <path d="M3 5.5 10.5 4.5v6.75H3V5.5Zm0 7.25h7.5v6.75L3 18.5v-5.75Zm8.75-8.4L21 3v9H11.75V4.35ZM11.75 12h9.25v9L11.75 19.65V12Z" />
    </svg>
  );
}

export function LinuxIcon({ size = 18, ...props }: IconProps) {
  // Simplified Tux silhouette — round head + body.
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="currentColor"
      aria-hidden
      {...props}
    >
      <path d="M12 2c-2.4 0-4 1.9-4 4.6 0 1.2.3 2.3.8 3.2-.7.7-1.3 1.8-1.6 3-.4 1.4-.7 3.3-1.8 4.4-.6.6-1 .9-1 1.6 0 .9.8 1.3 2 1.6 1.5.3 2.6.1 3.6-.3.6-.2 1.3-.4 2-.4s1.4.2 2 .4c1 .4 2.1.6 3.6.3 1.2-.3 2-.7 2-1.6 0-.7-.4-1-1-1.6-1.1-1.1-1.4-3-1.8-4.4-.3-1.2-.9-2.3-1.6-3 .5-.9.8-2 .8-3.2 0-2.7-1.6-4.6-4-4.6Zm-1.5 5.2c.3 0 .5.3.5.8s-.2.8-.5.8-.5-.3-.5-.8.2-.8.5-.8Zm3 0c.3 0 .5.3.5.8s-.2.8-.5.8-.5-.3-.5-.8.2-.8.5-.8Zm-3 2.6c.7.5 1.5.8 1.5.8s.8-.3 1.5-.8c0 .6-.7 1-1.5 1s-1.5-.4-1.5-1Z" />
    </svg>
  );
}
