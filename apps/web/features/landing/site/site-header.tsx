"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { ChevronDown, Menu, X } from "lucide-react";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { Sheet, SheetContent, SheetTitle } from "@multica/ui/components/ui/sheet";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { docsHrefForLocale, useLocale } from "../i18n";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import { formatStarCount, useGithubStars } from "../utils/use-github-stars";
import { GitHubMark, githubUrl } from "../components/shared";
import { ActionLink, SiteTextLink, focusRing } from "./primitives";
import { TOP_LINKS, buildMenus, type SiteMenu, type SiteMenuId } from "./sitemap";

const HOVER_OPEN_DELAY = 60;
const HOVER_CLOSE_DELAY = 140;

export function SiteHeader() {
  const { locale } = useLocale();
  const menus = useMemo(() => buildMenus(docsHrefForLocale(locale)), [locale]);
  const user = useAuthStore((s) => s.user);
  const ctaHref = useDashboardCtaHref();
  const stars = useGithubStars();

  const [openId, setOpenId] = useState<SiteMenuId | null>(null);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [scrolled, setScrolled] = useState(false);
  const hoverTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const triggerRefs = useRef(new Map<SiteMenuId, HTMLButtonElement>());

  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (!sentinel) return;
    const observer = new IntersectionObserver(([entry]) =>
      setScrolled(!entry?.isIntersecting),
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const timer = hoverTimer;
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, []);

  function clearHoverTimer() {
    if (hoverTimer.current) clearTimeout(hoverTimer.current);
    hoverTimer.current = null;
  }

  function scheduleOpen(id: SiteMenuId | null, delay: number) {
    clearHoverTimer();
    hoverTimer.current = setTimeout(() => setOpenId(id), delay);
  }

  function close(returnFocusTo?: SiteMenuId) {
    clearHoverTimer();
    setOpenId(null);
    if (returnFocusTo) triggerRefs.current.get(returnFocusTo)?.focus();
  }

  const openMenu = menus.find((menu) => menu.id === openId) ?? null;
  const ctaLabel = user ? "Dashboard" : "Get started";

  return (
    <>
      <div ref={sentinelRef} aria-hidden className="h-0" />
      <header
        className={cn(
          "sticky top-0 z-40 border-b bg-site-canvas transition-colors",
          scrolled || openMenu ? "border-site-line" : "border-transparent",
        )}
        onPointerLeave={(event) => {
          if (event.pointerType === "mouse") scheduleOpen(null, HOVER_CLOSE_DELAY);
        }}
        onPointerEnter={(event) => {
          if (event.pointerType === "mouse" && openId) clearHoverTimer();
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape" && openId) close(openId);
        }}
        onBlur={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) close();
        }}
      >
        <div className="mx-auto flex h-site-header w-full max-w-site items-center justify-between gap-6 px-site-gutter">
          <div className="flex min-w-0 items-center gap-8">
            <Link
              href="/"
              aria-label="Multica home"
              className={cn("flex shrink-0 items-center gap-2.5 rounded-sm text-site-ink", focusRing)}
              onClick={() => close()}
            >
              <MulticaIcon className="size-[18px]" noSpin />
              <span className="text-site-logo font-medium lowercase">
                multica
              </span>
            </Link>

            <nav aria-label="Primary" className="hidden items-center gap-0.5 lg:flex">
              {menus.map((menu) => (
                <button
                  key={menu.id}
                  ref={(node) => {
                    if (node) triggerRefs.current.set(menu.id, node);
                    else triggerRefs.current.delete(menu.id);
                  }}
                  type="button"
                  aria-expanded={openId === menu.id}
                  aria-controls={`site-menu-${menu.id}`}
                  onClick={() => {
                    clearHoverTimer();
                    setOpenId((current) => (current === menu.id ? null : menu.id));
                  }}
                  onPointerEnter={(event) => {
                    if (event.pointerType === "mouse") {
                      scheduleOpen(menu.id, openId ? 0 : HOVER_OPEN_DELAY);
                    }
                  }}
                  className={cn(navItemClassName, openId === menu.id && "bg-site-hover")}
                >
                  {menu.label}
                  <ChevronDown
                    aria-hidden
                    className={cn(
                      "size-3.5 text-site-ink-secondary transition-transform duration-200",
                      openId === menu.id && "rotate-180",
                    )}
                  />
                </button>
              ))}
              {TOP_LINKS.map((link) => (
                <Link
                  key={link.href}
                  href={link.href}
                  className={navItemClassName}
                  onPointerEnter={(event) => {
                    if (event.pointerType === "mouse") scheduleOpen(null, HOVER_CLOSE_DELAY);
                  }}
                >
                  {link.label}
                </Link>
              ))}
            </nav>
          </div>

          <div className="flex shrink-0 items-center gap-1.5">
            <Link
              href={githubUrl}
              target="_blank"
              rel="noreferrer"
              aria-label={stars != null ? `GitHub, ${formatStarCount(stars)} stars` : "GitHub"}
              className={cn(navItemClassName, "hidden md:inline-flex")}
            >
              <GitHubMark className="size-4" />
              {stars != null ? (
                <span className="text-site-muted tabular-nums">{formatStarCount(stars)}</span>
              ) : null}
            </Link>
            <Link href="/contact-sales" className={cn(navItemClassName, "hidden xl:inline-flex")}>
              Contact sales
            </Link>
            {user ? null : (
              <ActionLink href="/login" tone="secondary" className="hidden sm:inline-flex">
                Log in
              </ActionLink>
            )}
            <ActionLink href={ctaHref}>{ctaLabel}</ActionLink>
            <button
              type="button"
              aria-label="Open menu"
              aria-expanded={mobileOpen}
              onClick={() => setMobileOpen(true)}
              className={cn(navItemClassName, "px-2 lg:hidden")}
            >
              <Menu className="size-5" aria-hidden />
            </button>
          </div>
        </div>

        {openMenu ? <MegaPanel menu={openMenu} onNavigate={() => close()} /> : null}
      </header>

      {openMenu ? (
        <div
          aria-hidden
          onClick={() => close()}
          className="fixed inset-x-0 top-site-header bottom-0 z-30 hidden bg-site-canvas/70 backdrop-blur-md lg:block"
        />
      ) : null}

      <MobileMenu
        open={mobileOpen}
        onOpenChange={setMobileOpen}
        menus={menus}
        ctaHref={ctaHref}
        ctaLabel={ctaLabel}
        showLogin={!user}
      />
    </>
  );
}

const navItemClassName = cn(
  "inline-flex h-site-control-sm items-center gap-1 rounded-site-control px-3 text-site-small text-site-ink transition-colors hover:bg-site-hover",
  focusRing,
);

function MegaPanel({ menu, onNavigate }: { menu: SiteMenu; onNavigate: () => void }) {
  return (
    <div
      id={`site-menu-${menu.id}`}
      className="absolute inset-x-0 top-full hidden border-b border-site-line bg-site-canvas lg:block"
    >
      <div className="mx-auto grid w-full max-w-site grid-cols-5 gap-8 px-site-gutter pt-6 pb-10">
        {menu.groups.map((group) => (
          <div key={group.title}>
            <p className="text-site-small text-site-muted">{group.title}</p>
            <ul className="mt-3 flex flex-col">
              {group.links.map((link) => (
                <li key={`${group.title}-${link.label}`}>
                  <SiteTextLink
                    href={link.href}
                    external={link.external}
                    onClick={onNavigate}
                    className="py-1.5 text-site-body text-site-ink hover:text-site-ink-secondary"
                  >
                    {link.label}
                  </SiteTextLink>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </div>
  );
}

function MobileMenu({
  open,
  onOpenChange,
  menus,
  ctaHref,
  ctaLabel,
  showLogin,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  menus: SiteMenu[];
  ctaHref: string;
  ctaLabel: string;
  showLogin: boolean;
}) {
  const close = () => onOpenChange(false);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="top"
        showCloseButton={false}
        className="landing-light inset-0 gap-0 border-0 bg-site-canvas text-site-ink shadow-none data-[side=top]:h-dvh lg:hidden"
      >
        <SheetTitle className="sr-only">Menu</SheetTitle>
        <div className="flex h-site-header shrink-0 items-center justify-between px-site-gutter">
          <Link href="/" onClick={close} className={cn("flex items-center gap-2.5 rounded-sm", focusRing)}>
            <MulticaIcon className="size-[18px]" noSpin />
            <span className="text-site-logo font-medium lowercase">
              multica
            </span>
          </Link>
          <button type="button" aria-label="Close menu" onClick={close} className={cn(navItemClassName, "px-2")}>
            <X className="size-5" aria-hidden />
          </button>
        </div>

        <nav aria-label="Primary" className="min-h-0 flex-1 overflow-y-auto px-site-gutter">
          {menus.map((menu) => (
            <details key={menu.id} className="group border-b border-site-line">
              <summary
                className={cn(
                  "flex h-14 cursor-pointer list-none items-center justify-between text-site-card [&::-webkit-details-marker]:hidden",
                  focusRing,
                )}
              >
                {menu.label}
                <ChevronDown aria-hidden className="size-4 text-site-ink-secondary transition-transform group-open:rotate-180" />
              </summary>
              <div className="flex flex-col gap-4 pb-5">
                {menu.groups.map((group) => (
                  <div key={group.title}>
                    <p className="text-site-small text-site-muted">{group.title}</p>
                    <ul className="mt-1 grid grid-cols-2 gap-x-4">
                      {group.links.map((link) => (
                        <li key={`${group.title}-${link.label}`}>
                          <SiteTextLink
                            href={link.href}
                            external={link.external}
                            onClick={close}
                            className="min-h-10 text-site-body"
                          >
                            {link.label}
                          </SiteTextLink>
                        </li>
                      ))}
                    </ul>
                  </div>
                ))}
              </div>
            </details>
          ))}
          {[...TOP_LINKS, { label: "Contact sales", href: "/contact-sales" }].map((link) => (
            <Link
              key={link.href}
              href={link.href}
              onClick={close}
              className={cn("flex h-14 items-center border-b border-site-line text-site-card", focusRing)}
            >
              {link.label}
            </Link>
          ))}
        </nav>

        <div className="grid shrink-0 grid-cols-2 gap-2.5 px-site-gutter pt-4 pb-8">
          {showLogin ? (
            <ActionLink href="/login" tone="secondary" size="lg" onClick={close}>
              Log in
            </ActionLink>
          ) : null}
          <ActionLink href={ctaHref} size="lg" onClick={close} className={showLogin ? undefined : "col-span-2"}>
            {ctaLabel}
          </ActionLink>
        </div>
      </SheetContent>
    </Sheet>
  );
}
