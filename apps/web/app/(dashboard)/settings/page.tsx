"use client";

import { User, Palette, Key, Settings, Users, FolderGit2 } from "lucide-react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@multica/ui/components/ui/tabs";
import { useWorkspaceStore } from "@/platform/workspace";
import { useDashboardLocale } from "@/features/dashboard/i18n";
import { AccountTab } from "./_components/account-tab";
import { AppearanceTab } from "./_components/general-tab";
import { TokensTab } from "./_components/tokens-tab";
import { WorkspaceTab } from "./_components/workspace-tab";
import { MembersTab } from "./_components/members-tab";
import { RepositoriesTab } from "./_components/repositories-tab";

export default function SettingsPage() {
  const workspaceName = useWorkspaceStore((s) => s.workspace?.name);
  const { t } = useDashboardLocale();

  const accountTabs = [
    { value: "profile", label: t.settings.profile, icon: User },
    { value: "appearance", label: t.settings.appearance, icon: Palette },
    { value: "tokens", label: t.settings.apiTokens, icon: Key },
  ];

  const workspaceTabs = [
    { value: "workspace", label: t.settings.general, icon: Settings },
    { value: "repositories", label: t.settings.repositories, icon: FolderGit2 },
    { value: "members", label: t.settings.members, icon: Users },
  ];

  return (
    <Tabs defaultValue="profile" orientation="vertical" className="flex-1 min-h-0 gap-0">
      {/* Left nav */}
      <div className="w-52 shrink-0 border-r overflow-y-auto p-4">
        <h1 className="text-sm font-semibold mb-4 px-2">{t.settings.title}</h1>
        <TabsList variant="line" className="flex-col items-stretch">
          {/* My Account group */}
          <span className="px-2 pb-1 pt-2 text-xs font-medium text-muted-foreground">
            {t.settings.myAccount}
          </span>
          {accountTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}

          {/* Workspace group */}
          <span className="px-2 pb-1 pt-4 text-xs font-medium text-muted-foreground truncate">
            {workspaceName ?? t.settings.general}
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
          <TabsContent value="profile"><AccountTab /></TabsContent>
          <TabsContent value="appearance"><AppearanceTab /></TabsContent>
          <TabsContent value="tokens"><TokensTab /></TabsContent>
          <TabsContent value="workspace"><WorkspaceTab /></TabsContent>
          <TabsContent value="repositories"><RepositoriesTab /></TabsContent>
          <TabsContent value="members"><MembersTab /></TabsContent>
        </div>
      </div>
    </Tabs>
  );
}
