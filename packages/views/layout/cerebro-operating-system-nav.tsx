"use client";

// CEREBRO-PATCH(operating-system-sidebar-component): FIR-2816 fork-owned navigation mount.
import { Gem, Milestone } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
// CEREBRO-PATCH(operating-system-sidebar-settings): Load workspace terminology for fork-owned navigation.
import { useQuery } from "@tanstack/react-query";
import { settingsOptions } from "@multica/cerebro-operating-system/core";
import { SidebarMenuButton, SidebarMenuItem } from "@multica/ui/components/ui/sidebar";
import { AppLink, useNavigation } from "../navigation";

export function OperatingSystemNavItems({ workspaceSlug, onClick }: { workspaceSlug: string; onClick?: () => void }) {
  const enabled = useFeatureFlag("cerebro_operating_system");
  const wsId = useCurrentWorkspace()?.id ?? "";
  const settings = useQuery({ ...settingsOptions(wsId), enabled: enabled && !!wsId });
  const { pathname } = useNavigation();
  if (!enabled || !workspaceSlug) return null;
  const terminology = settings.data?.terminology ?? { strategy: "Strategy", rocks: "Rocks" };
  return <>
    {/* CEREBRO-PATCH(operating-system-sidebar-group): FIR-2816 v4 groups Strategy and Rocks under Operating System. */}
    <SidebarMenuItem><div className="mt-3 flex items-center justify-between px-2 py-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground"><span>Operating System</span><span className="rounded-full bg-primary/10 px-1.5 py-0.5 text-[9px] text-primary">NEW</span></div></SidebarMenuItem>
    <Item href={`/${workspaceSlug}/rocks`} label={terminology.rocks} active={pathname.startsWith(`/${workspaceSlug}/rocks`)} icon={<Milestone />} onClick={onClick} />
    <Item href={`/${workspaceSlug}/strategy`} label={terminology.strategy} active={pathname.startsWith(`/${workspaceSlug}/strategy`)} icon={<Gem />} onClick={onClick} />
  </>;
}

function Item({ href, label, active, icon, onClick }: { href: string; label: string; active: boolean; icon: React.ReactNode; onClick?: () => void }) {
  return <SidebarMenuItem><SidebarMenuButton isActive={active} render={<AppLink href={href} />} onClick={onClick} className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground">{icon}<span>{label}</span></SidebarMenuButton></SidebarMenuItem>;
}
