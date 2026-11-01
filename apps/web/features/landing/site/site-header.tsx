"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from "react";
import Link from "next/link";
import { ArrowUpRight, ChevronDown, Menu, X } from "@multica/ui/icons";
import { Sheet, SheetContent, SheetTitle } from "@multica/ui/components/ui/sheet";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { docsHrefForLocale, useLocale } from "../i18n";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import { formatStarCount, useGithubStars } from "../utils/use-github-stars";
import { GitHubMark, githubUrl } from "../components/shared";
import { HERO_PROVIDERS } from "../components/provider-marks";
import { BrandWordmark } from "./brand-logo";
import { ActionLink, SiteTextLink, focusRing } from "./primitives";
import { TOP_LINKS, buildMenus, type SiteLink, type SiteLinkGroup, type SiteMenu, type SiteMenuId } from "./sitemap";

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
  // The panel stays mounted so it can animate out; it keeps showing the last
  // menu while it fades. Moving straight from one menu to another slides the
  // open panel over instead of reopening it.
  const [lastId, setLastId] = useState<SiteMenuId>(menus[0]!.id);
  const [prevOpenId, setPrevOpenId] = useState<SiteMenuId | null>(null);
  const [switchDir, setSwitchDir] = useState(0);
  if (openId !== prevOpenId) {
    setPrevOpenId(openId);
    if (openId) {
      const index = (id: SiteMenuId) => menus.findIndex((menu) => menu.id === id);
      setSwitchDir(prevOpenId ? Math.sign(index(openId) - index(prevOpenId)) : 0);
      setLastId(openId);
    }
  }
  const panelMenu = openMenu ?? menus.find((menu) => menu.id === lastId) ?? menus[0]!;
  const barRef = useRef<HTMLDivElement>(null);
  const ctaLabel = user ? "Dashboard" : "Get started";

  return (
    <>
      <div ref={sentinelRef} aria-hidden className="h-0" />
      <header
        className={cn(
          "sticky top-0 z-40 border-b bg-site-canvas transition-colors",
          scrolled ? "border-site-line" : "border-transparent",
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
        <div
          ref={barRef}
          className="relative mx-auto flex h-site-header w-full max-w-site-wide items-center justify-between gap-6 px-site-gutter"
        >
          <div className="flex min-w-0 items-center gap-8">
            <Link
              href="/"
              aria-label="Multica home"
              className={cn("group flex shrink-0 items-center rounded-sm", focusRing)}
              onClick={() => close()}
            >
              <BrandWordmark />
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

          <MenuPanel
            menu={panelMenu}
            open={openMenu != null}
            switchDir={switchDir}
            barRef={barRef}
            triggerRefs={triggerRefs}
            onNavigate={() => close()}
          />
        </div>
      </header>

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

const EASE_OUT = "ease-[cubic-bezier(0.32,0.72,0,1)]";

// A dropdown under its trigger: it opens with a short fade and scale from the
// trigger and closes faster. Moving to another menu slides and resizes the
// open panel while the new content fades in from that side.
function MenuPanel({
  menu,
  open,
  switchDir,
  barRef,
  triggerRefs,
  onNavigate,
}: {
  menu: SiteMenu;
  open: boolean;
  switchDir: number;
  barRef: RefObject<HTMLDivElement | null>;
  triggerRefs: RefObject<Map<SiteMenuId, HTMLButtonElement>>;
  onNavigate: () => void;
}) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const cardRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const sliding = open && switchDir !== 0;

  useLayoutEffect(() => {
    const place = () => {
      const bar = barRef.current;
      const wrap = wrapRef.current;
      const card = cardRef.current;
      const content = contentRef.current;
      const trigger = triggerRefs.current.get(menu.id);
      if (!bar || !wrap || !card || !content || !trigger) return;
      const barRect = bar.getBoundingClientRect();
      const triggerRect = trigger.getBoundingClientRect();
      const inset = parseFloat(getComputedStyle(bar).paddingLeft) || 0;
      const width = content.offsetWidth;
      const center = triggerRect.left + triggerRect.width / 2 - barRect.left;
      const left = Math.max(inset, Math.min(center - width / 2, barRect.width - Math.min(inset, 16) - width));
      wrap.style.left = `${left}px`;
      card.style.width = `${width}px`;
      card.style.height = `${content.offsetHeight}px`;
      card.style.transformOrigin = `${center - left}px top`;
    };
    place();
    const observer = new ResizeObserver(place);
    if (contentRef.current) observer.observe(contentRef.current);
    window.addEventListener("resize", place);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", place);
    };
  }, [menu.id, barRef, triggerRefs]);

  return (
    <div
      ref={wrapRef}
      id={`site-menu-${menu.id}`}
      inert={!open}
      className={cn(
        "absolute top-full hidden pt-2 lg:block",
        !open && "pointer-events-none",
        sliding && `transition-[left] duration-300 ${EASE_OUT} motion-reduce:transition-none`,
      )}
    >
      <div
        ref={cardRef}
        className={cn(
          "overflow-hidden rounded-site-card border border-site-line bg-site-surface shadow-site-menu",
          "transition-[opacity,scale,translate] motion-reduce:transition-none",
          open
            ? `translate-y-0 scale-100 opacity-100 duration-200 ${EASE_OUT}`
            : "-translate-y-1 scale-[0.97] opacity-0 duration-150 ease-in",
          sliding && "transition-[opacity,scale,translate,width,height] duration-300",
        )}
      >
        <div
          ref={contentRef}
          key={menu.id}
          className={cn(
            "flex w-max gap-4 p-3",
            sliding && "animate-in fade-in duration-300 motion-reduce:animate-none",
            switchDir > 0 && "slide-in-from-right-6",
            switchDir < 0 && "slide-in-from-left-6",
          )}
        >
          {menuColumns(menu.groups).map((column) => (
            <div key={column[0]!.title} className="flex min-w-36 flex-col gap-4">
              {column.map((group) => (
                <div key={group.title}>
                  <p className="px-2 pt-1 pb-1.5 text-[0.8125rem] text-site-muted">{group.title}</p>
                  <ul className="flex flex-col">
                    {group.links.map((link) => (
                      <li key={`${group.title}-${link.label}`}>
                        <MenuLink link={link} onNavigate={onNavigate} />
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

/** Groups of two links or fewer share a column with the next short group. */
function menuColumns(groups: SiteLinkGroup[]): SiteLinkGroup[][] {
  const columns: SiteLinkGroup[][] = [];
  for (const group of groups) {
    const last = columns.at(-1);
    if (last?.length === 1 && last[0]!.links.length <= 2 && group.links.length <= 2) last.push(group);
    else columns.push([group]);
  }
  return columns;
}

function MenuLink({ link, onNavigate }: { link: SiteLink; onNavigate: () => void }) {
  const Mark = PROVIDER_MARKS.get(link.label);
  return (
    <Link
      href={link.href}
      onClick={onNavigate}
      {...(link.external ? { target: "_blank", rel: "noreferrer" } : {})}
      className={cn(
        "group/link flex items-center gap-2 rounded-site-inner px-2 py-1.5 text-site-small whitespace-nowrap text-site-ink transition-colors hover:bg-site-hover",
        focusRing,
      )}
    >
      {Mark ? <Mark className="size-4 shrink-0" /> : null}
      {link.label}
      {link.external ? (
        <ArrowUpRight
          aria-hidden
          className="-ml-1 size-3.5 text-site-muted opacity-0 transition-opacity group-hover/link:opacity-100 group-focus-visible/link:opacity-100"
        />
      ) : null}
    </Link>
  );
}

const PROVIDER_MARKS = new Map(HERO_PROVIDERS.map((provider) => [provider.name, provider.Mark]));

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
          <Link href="/" aria-label="Multica home" onClick={close} className={cn("flex items-center rounded-sm", focusRing)}>
              <BrandWordmark />
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
