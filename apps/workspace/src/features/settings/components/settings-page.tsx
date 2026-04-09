"use client";

import { User, Palette, Key, Settings, Users, FolderGit2 } from "lucide-react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { useIsMobile } from "@/hooks/use-mobile";
import { useWorkspaceStore } from "@/features/workspace";
import { AccountTab } from "./account-tab";
import { AppearanceTab } from "./general-tab";
import { TokensTab } from "./tokens-tab";
import { WorkspaceTab } from "./workspace-tab";
import { MembersTab } from "./members-tab";
import { RepositoriesTab } from "./repositories-tab";

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

const mobileTabs = [...accountTabs, ...workspaceTabs];

const settingsSectionLabelClassName =
  "col-span-full px-2 pb-1 text-xs font-medium text-muted-foreground";

export default function SettingsPage() {
  const isMobile = useIsMobile();
  const workspaceName = useWorkspaceStore((s) => s.workspace?.name);

  return (
    <Tabs
      defaultValue="profile"
      orientation={isMobile ? "horizontal" : "vertical"}
      className="flex-1 min-h-0 flex-col gap-0 lg:flex-row"
    >
      {isMobile ? (
        <div className="w-full shrink-0 border-b px-4 py-4">
          <h1 className="mb-3 text-base font-semibold">Settings</h1>
          <TabsList
            variant="line"
            className="w-full justify-start gap-2 overflow-x-auto bg-transparent p-0"
          >
            {mobileTabs.map((tab) => (
              <TabsTrigger
                key={tab.value}
                value={tab.value}
                className="h-9 flex-none justify-start rounded-full border border-border bg-background px-3 after:hidden hover:bg-accent/50 data-active:border-primary/20 data-active:bg-accent/60 data-active:text-foreground"
              >
                <tab.icon className="h-4 w-4" />
                {tab.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
      ) : (
        <div className="w-full shrink-0 border-b p-4 lg:w-60 lg:border-r lg:border-b-0 lg:overflow-y-auto">
          <h1 className="mb-4 px-2 text-sm font-semibold">Settings</h1>
          <TabsList
            variant="line"
            className="!flex w-full flex-col items-stretch gap-1"
          >
            <span className={settingsSectionLabelClassName}>
              My Account
            </span>
            {accountTabs.map((tab) => (
              <TabsTrigger
                key={tab.value}
                value={tab.value}
                className="min-h-10 justify-start px-3 text-left after:hidden hover:bg-accent/50 data-active:bg-accent/60 data-active:text-foreground"
              >
                <tab.icon className="h-4 w-4" />
                {tab.label}
              </TabsTrigger>
            ))}

            <span
              className={`${settingsSectionLabelClassName} pt-3 lg:pt-4 truncate`}
            >
              {workspaceName ?? "Workspace"}
            </span>
            {workspaceTabs.map((tab) => (
              <TabsTrigger
                key={tab.value}
                value={tab.value}
                className="min-h-10 justify-start px-3 text-left after:hidden hover:bg-accent/50 data-active:bg-accent/60 data-active:text-foreground"
              >
                <tab.icon className="h-4 w-4" />
                {tab.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
      )}

      <div className="flex-1 min-w-0 overflow-y-auto">
        <div className="w-full max-w-none p-4 sm:p-5 lg:mx-auto lg:max-w-3xl lg:p-6">
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
