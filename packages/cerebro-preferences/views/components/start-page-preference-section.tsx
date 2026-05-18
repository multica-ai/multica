"use client";

import { useState } from "react";
import { Home } from "lucide-react";
import { Label } from "@multica/ui/components/ui/label";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";

export const START_PAGE_DESKTOP_KEY = "start_page_desktop";
export const START_PAGE_MOBILE_KEY = "start_page_mobile";

const START_PAGE_OPTIONS = [
  { value: "issues", label: "Issues" },
  { value: "inbox", label: "Inbox" },
  { value: "my-issues", label: "My Issues" },
  { value: "projects", label: "Projects" },
  { value: "documents", label: "Documents" },
  { value: "notifications", label: "Notifications" },
] as const;

type StartPage = (typeof START_PAGE_OPTIONS)[number]["value"];

export const VALID_START_PAGES = new Set<string>(
  START_PAGE_OPTIONS.map((o) => o.value),
);

function toStartPage(val: unknown): StartPage {
  if (typeof val === "string" && VALID_START_PAGES.has(val))
    return val as StartPage;
  return "issues";
}

export function StartPagePreferenceSection() {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const [saving, setSaving] = useState<string | null>(null);

  const desktopPage = toStartPage(user?.preferences?.[START_PAGE_DESKTOP_KEY]);
  const mobilePage = toStartPage(user?.preferences?.[START_PAGE_MOBILE_KEY]);

  const handleChange = async (key: string, value: StartPage) => {
    setSaving(key);
    try {
      const updated = await api.updateMyPreferences({ [key]: value });
      setUser(updated);
      toast.success("Start page updated");
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : "Failed to update start page",
      );
    } finally {
      setSaving(null);
    }
  };

  return (
    <section className="space-y-4">
      <div className="flex items-center gap-2">
        <Home className="h-4 w-4 text-muted-foreground" />
        <h2 className="text-sm font-semibold">Start page</h2>
      </div>

      <Card>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between gap-4">
            <div className="space-y-0.5">
              <Label className="text-sm font-medium">Desktop</Label>
              <p className="text-xs text-muted-foreground">
                First page shown when you log in on desktop.
              </p>
            </div>
            <Select
              value={desktopPage}
              onValueChange={(v) =>
                handleChange(START_PAGE_DESKTOP_KEY, v as StartPage)
              }
              disabled={saving === START_PAGE_DESKTOP_KEY || !user}
            >
              <SelectTrigger className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {START_PAGE_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex items-center justify-between gap-4">
            <div className="space-y-0.5">
              <Label className="text-sm font-medium">Mobile</Label>
              <p className="text-xs text-muted-foreground">
                First page shown when you log in on a mobile device.
              </p>
            </div>
            <Select
              value={mobilePage}
              onValueChange={(v) =>
                handleChange(START_PAGE_MOBILE_KEY, v as StartPage)
              }
              disabled={saving === START_PAGE_MOBILE_KEY || !user}
            >
              <SelectTrigger className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {START_PAGE_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </CardContent>
      </Card>
    </section>
  );
}
