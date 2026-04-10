"use client";

import { useNavigation } from "./context";

interface AppLinkProps extends React.AnchorHTMLAttributes<HTMLAnchorElement> {
  href: string;
  children: React.ReactNode;
}

export function AppLink({
  href,
  children,
  onClick,
  ...props
}: AppLinkProps) {
  const { push, buildHref } = useNavigation();
  const resolvedHref = buildHref ? buildHref(href) : href;

  const handleClick = (e: React.MouseEvent<HTMLAnchorElement>) => {
    // Allow ctrl/cmd+click to open in new tab
    if (e.metaKey || e.ctrlKey || e.shiftKey) return;
    e.preventDefault();
    onClick?.(e);
    push(href);
  };

  return (
    <a href={resolvedHref} onClick={handleClick} {...props}>
      {children}
    </a>
  );
}
