"use client";

import { User, Palette, Key, Settings, Users, FolderGit2 } from "lucide-react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { useWorkspaceStore } from "@/features/workspace";
import { useIsMobile } from "@/hooks/use-mobile";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { AccountTab } from "./_components/account-tab";
import { AppearanceTab } from "./_components/general-tab";
import { TokensTab } from "./_components/tokens-tab";
import { WorkspaceTab } from "./_components/workspace-tab";
import { MembersTab } from "./_components/members-tab";
import { RepositoriesTab } from "./_components/repositories-tab";

const accountTabs = [
  { value: "profile", label: "Profile", icon: User },
  { value: "appearance", label: "Appearance", icon: Palette },
  { value: "tokens", label: "API Tokens", icon: Key },
];

const workspaceTabs = [
  { value: "workspace", label: "General", icon: Settings },
  { value: "repositories", label: "Repositories", icon: FolderGit2 },
  { value: "members", label: "Members", icon: Users },
];

export default function SettingsPage() {
  const isMobile = useIsMobile();
  const workspaceName = useWorkspaceStore((s) => s.workspace?.name);

  const tabContent = (
    <>
      <TabsContent value="profile"><AccountTab /></TabsContent>
      <TabsContent value="appearance"><AppearanceTab /></TabsContent>
      <TabsContent value="tokens"><TokensTab /></TabsContent>
      <TabsContent value="workspace"><WorkspaceTab /></TabsContent>
      <TabsContent value="repositories"><RepositoriesTab /></TabsContent>
      <TabsContent value="members"><MembersTab /></TabsContent>
    </>
  );

  if (isMobile) {
    return (
      <Tabs defaultValue="profile" className="flex flex-col flex-1 min-h-0 gap-0">
        {/* Top: scrollable horizontal tabs */}
        <div className="shrink-0 border-b px-3 pt-3 pb-0">
          <div className="flex items-center gap-2 mb-3 px-1">
            <SidebarTrigger className="md:hidden" />
            <h1 className="text-sm font-semibold">Settings</h1>
          </div>
          <TabsList variant="line" className="overflow-x-auto whitespace-nowrap w-full justify-start gap-0">
            {accountTabs.map((tab) => (
              <TabsTrigger key={tab.value} value={tab.value} className="text-xs px-2.5 py-1.5">
                <tab.icon className="h-3.5 w-3.5" />
                {tab.label}
              </TabsTrigger>
            ))}
            {workspaceTabs.map((tab) => (
              <TabsTrigger key={tab.value} value={tab.value} className="text-xs px-2.5 py-1.5">
                <tab.icon className="h-3.5 w-3.5" />
                {tab.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>

        {/* Content */}
        <div className="flex-1 min-h-0 overflow-y-auto">
          <div className="w-full p-4">
            {tabContent}
          </div>
        </div>
      </Tabs>
    );
  }

  return (
    <Tabs defaultValue="profile" orientation="vertical" className="flex-1 min-h-0 gap-0">
      {/* Left nav */}
      <div className="w-52 shrink-0 border-r overflow-y-auto p-4">
        <h1 className="text-sm font-semibold mb-4 px-2">Settings</h1>
        <TabsList variant="line" className="flex-col items-stretch">
          {/* My Account group */}
          <span className="px-2 pb-1 pt-2 text-xs font-medium text-muted-foreground">
            My Account
          </span>
          {accountTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}

          {/* Workspace group */}
          <span className="px-2 pb-1 pt-4 text-xs font-medium text-muted-foreground truncate">
            {workspaceName ?? "Workspace"}
          </span>
          {workspaceTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>

      {/* Right content */}
      <div className="flex-1 min-w-0 overflow-y-auto">
        <div className="w-full max-w-3xl mx-auto p-6">
          {tabContent}
        </div>
      </div>
    </Tabs>
  );
}
