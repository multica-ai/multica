import { forwardRef, useMemo } from "react";
import {
  Link as TanStackLink,
  useLocation,
  useNavigate,
} from "@tanstack/react-router";

function isExternalHref(href: string): boolean {
  return /^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(href);
}

type LinkProps = React.AnchorHTMLAttributes<HTMLAnchorElement> & {
  href: string;
};

export const Link = forwardRef<HTMLAnchorElement, LinkProps>(function Link(
  { href, ...props },
  ref,
) {
  if (isExternalHref(href)) {
    return <a ref={ref} href={href} {...props} />;
  }

  return <TanStackLink ref={ref} to={href} {...props} />;
});

export function useRouter() {
  const navigate = useNavigate();

  return useMemo(
    () => ({
      push: (href: string) => navigate({ to: href }),
      replace: (href: string) => navigate({ to: href, replace: true }),
    }),
    [navigate],
  );
}

export function usePathname(): string {
  return useLocation({
    select: (location) => location.pathname,
  });
}

export function useSearchParams(): URLSearchParams {
  const searchStr = useLocation({
    select: (location) => location.searchStr,
  });

  return useMemo(() => new URLSearchParams(searchStr), [searchStr]);
}
