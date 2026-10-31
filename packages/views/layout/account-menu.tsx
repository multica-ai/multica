"use client";

import {
  ArrowUpRight,
  BookOpen,
  Download,
  History,
  Keyboard,
  Globe,
  LogOut,
  Monitor,
  Moon,
  Palette,
  Sun,
  User,
} from "@multica/ui/icons";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspacePaths } from "@multica/core/paths";
import type { SupportedLocale } from "@multica/core/i18n";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useLogout } from "../auth";
import { docsLocalePrefix } from "../common/docs-locale";
import { useLanguageSwitch } from "../common/use-language-switch";
import { useT } from "../i18n";
import { AppLink } from "../navigation";
import { isDesktopShell } from "../platform/local-directory";
import { DISCORD_URL, DiscordIcon } from "./discord";
import { CHANGELOG_URL, DOCS_URL, DOWNLOAD_URL } from "./help-links";
import { RAIL_ICON_BUTTON_CLASS } from "./rail-styles";

const THEMES = [
  { value: "light", icon: Sun },
  { value: "dark", icon: Moon },
  { value: "system", icon: Monitor },
] as const;

function SubmenuLabel({ label, value }: { label: string; value?: string }) {
  return (
    <>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {value && <span className="shrink-0 text-caption text-muted-foreground">{value}</span>}
    </>
  );
}

function ExternalItem({ href, icon, label }: { href: string; icon: React.ReactNode; label: string }) {
  return (
    <DropdownMenuItem render={<a href={href} target="_blank" rel="noopener noreferrer" />}>
      {icon}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <ArrowUpRight className="size-3 text-faint-foreground" />
    </DropdownMenuItem>
  );
}

/** The signed-in user's menu at the foot of the rail. */
export function AccountMenu() {
  const { t, i18n } = useT("layout");
  const user = useAuthStore((s) => s.user);
  const p = useWorkspacePaths();
  const logout = useLogout();
  const { theme, setTheme } = useTheme();
  const language = useLanguageSwitch();
  const themeValue = (theme ?? "system") as (typeof THEMES)[number]["value"];
  const name = user?.name ?? "";
  const avatar = (size: "md" | "lg") => (
    <ActorAvatar
      name={name}
      initials={(name || "U").charAt(0).toUpperCase()}
      avatarUrl={resolvePublicFileUrl(user?.avatar_url)}
      size={size}
    />
  );

  return (
    <DropdownMenu>
      <TooltipHint label={t(($) => $.account.trigger)} side="right">
        <DropdownMenuTrigger
          aria-label={t(($) => $.account.trigger)}
          className={cn(RAIL_ICON_BUTTON_CLASS, "rounded-full")}
        >
          {avatar("md")}
        </DropdownMenuTrigger>
      </TooltipHint>
      <DropdownMenuContent side="right" align="end" sideOffset={8} className="w-64">
        <div className="flex items-center gap-2.5 px-2 py-1.5">
          {avatar("lg")}
          <div className="min-w-0 flex-1">
            <p className="truncate text-body font-medium leading-tight">{user?.name}</p>
            <p className="truncate text-caption leading-tight text-muted-foreground">
              {user?.email}
            </p>
          </div>
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem render={<AppLink href={`${p.settings()}?tab=profile`} />}>
            <User />
            {t(($) => $.account.profile)}
          </DropdownMenuItem>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <Palette />
              <SubmenuLabel
                label={t(($) => $.account.theme)}
                value={t(($) => $.account.themes[themeValue])}
              />
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              <DropdownMenuRadioGroup value={themeValue} onValueChange={setTheme}>
                {THEMES.map(({ value, icon: Icon }) => (
                  <DropdownMenuRadioItem key={value} value={value}>
                    <Icon />
                    {t(($) => $.account.themes[value])}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <Globe />
              <SubmenuLabel
                label={t(($) => $.account.language)}
                value={language.options.find((o) => o.value === language.currentLocale)?.label}
              />
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              <DropdownMenuRadioGroup
                value={language.currentLocale}
                onValueChange={(next) => void language.changeLanguage(next as SupportedLocale)}
              >
                {language.options.map((option) => (
                  <DropdownMenuRadioItem key={option.value} value={option.value}>
                    {option.label}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuItem render={<AppLink href={`${p.settings()}?tab=shortcuts`} />}>
            <Keyboard />
            {t(($) => $.account.shortcuts)}
          </DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <ExternalItem
            href={`${DOCS_URL}${docsLocalePrefix(i18n.language)}`}
            icon={<BookOpen />}
            label={t(($) => $.help.docs)}
          />
          <ExternalItem href={CHANGELOG_URL} icon={<History />} label={t(($) => $.help.changelog)} />
          <ExternalItem href={DISCORD_URL} icon={<DiscordIcon />} label={t(($) => $.help.discord)} />
          {!isDesktopShell() && (
            <ExternalItem
              href={DOWNLOAD_URL}
              icon={<Download />}
              label={t(($) => $.help.download_desktop)}
            />
          )}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem variant="destructive" onClick={logout}>
            <LogOut />
            {t(($) => $.sidebar.log_out)}
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
