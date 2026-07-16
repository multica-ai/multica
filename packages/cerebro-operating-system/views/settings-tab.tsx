"use client";

import { useEffect, useState, type ComponentType, type FormEvent, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Settings2 } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { settingsOptions, useUpdateSettings } from "../core/queries";
import type { Terminology } from "../core/types";

interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: ComponentType<{ className?: string }>;
  content: ReactNode;
  wide?: boolean;
}

const EOS_TERMINOLOGY: Terminology = { strategy: "Strategy", rock: "Rock", rocks: "Rocks" };

export function OperatingSystemSettingsTab() {
  const wsId = useWorkspaceId();
  const settings = useQuery(settingsOptions(wsId));
  const update = useUpdateSettings(wsId);
  const current = settings.data?.terminology ?? EOS_TERMINOLOGY;
  const [values, setValues] = useState<Terminology>(current);
  const [profile, setProfile] = useState("eos");

  useEffect(() => {
    setValues(current);
    setProfile(current.strategy === "Strategy" && current.rock === "Rock" && current.rocks === "Rocks" ? "eos" : "custom");
  }, [current.rock, current.rocks, current.strategy]);

  function changeProfile(next: string) {
    setProfile(next);
    if (next === "eos") setValues(EOS_TERMINOLOGY);
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    update.mutate(values, { onSuccess: () => setProfile(values.strategy === "Strategy" && values.rock === "Rock" && values.rocks === "Rocks" ? "eos" : "custom") });
  }

  return (
    <div className="mx-auto grid max-w-3xl gap-6">
      <div><h2 className="text-xl font-semibold">Operating System</h2><p className="mt-1 text-sm text-muted-foreground">Choose a familiar profile or adapt the language to your company. Data relationships stay unchanged.</p></div>
      <form onSubmit={submit} className="grid gap-5 rounded-xl border bg-card p-5 sm:grid-cols-2">
        <label className="grid gap-1 text-sm sm:col-span-2">Profile
          <select aria-label="Profile" value={profile} onChange={(event) => changeProfile(event.target.value)} className="h-10 rounded-md border bg-background px-3"><option value="eos">EOS</option><option value="custom">Custom</option></select>
        </label>
        <label className="grid gap-1 text-sm sm:col-span-2">Strategy label<input aria-label="Strategy label" required value={values.strategy} onChange={(event) => { setValues({ ...values, strategy: event.target.value }); setProfile("custom"); }} className="h-10 rounded-md border bg-background px-3" /></label>
        <label className="grid gap-1 text-sm">Rock label<input aria-label="Rock label" required value={values.rock} onChange={(event) => { setValues({ ...values, rock: event.target.value }); setProfile("custom"); }} className="h-10 rounded-md border bg-background px-3" /></label>
        <label className="grid gap-1 text-sm">Rocks label<input aria-label="Rocks label" required value={values.rocks} onChange={(event) => { setValues({ ...values, rocks: event.target.value }); setProfile("custom"); }} className="h-10 rounded-md border bg-background px-3" /></label>
        {update.isError && <p role="alert" className="text-sm text-destructive sm:col-span-2">Terminology could not be saved.</p>}
        <div className="flex justify-end sm:col-span-2"><button disabled={update.isPending} className="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">Save terminology</button></div>
      </form>
    </div>
  );
}

export function useCerebroOperatingSystemSettingsTabs(): ExtraSettingsTab[] {
  const enabled = useFeatureFlag("cerebro_operating_system");
  return enabled ? [{ value: "operating-system", label: "Operating System", icon: Settings2, content: <OperatingSystemSettingsTab /> }] : [];
}
