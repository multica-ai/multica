"use client";

import { Volume2 } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Slider } from "@/components/ui/slider";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { usePomodoroSettings } from "@/features/time-tracking/hooks/use-pomodoro-settings";
import { useSoundSystem } from "@/features/time-tracking/hooks/use-sound-system";

const WHITE_NOISE_OPTIONS = [
  { value: "none", label: "None" },
  { value: "brown", label: "Brown Noise" },
  { value: "rain", label: "Rain" },
  { value: "cafe", label: "Cafe Ambience" },
  { value: "pink", label: "Pink Noise" },
] as const;

export function PomodoroSettingsTab() {
  const { settings, updateSettings } = usePomodoroSettings();
  const { playWorkComplete } = useSoundSystem(settings);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-base font-semibold">Pomodoro</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Configure timer durations, auto-start behavior, and sound preferences.
        </p>
      </div>

      {/* Time Intervals */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-medium">Time Intervals</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="work-minutes">Work Duration</Label>
              <div className="flex items-center gap-2">
                <Input
                  id="work-minutes"
                  type="number"
                  min={1}
                  max={120}
                  value={settings.work_minutes}
                  onChange={(e) =>
                    updateSettings({ work_minutes: Number(e.target.value) })
                  }
                  className="w-20"
                />
                <span className="text-sm text-muted-foreground">minutes</span>
              </div>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="short-break-minutes">Short Break</Label>
              <div className="flex items-center gap-2">
                <Input
                  id="short-break-minutes"
                  type="number"
                  min={1}
                  max={60}
                  value={settings.short_break_minutes}
                  onChange={(e) =>
                    updateSettings({ short_break_minutes: Number(e.target.value) })
                  }
                  className="w-20"
                />
                <span className="text-sm text-muted-foreground">minutes</span>
              </div>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="long-break-minutes">Long Break</Label>
              <div className="flex items-center gap-2">
                <Input
                  id="long-break-minutes"
                  type="number"
                  min={1}
                  max={120}
                  value={settings.long_break_minutes}
                  onChange={(e) =>
                    updateSettings({ long_break_minutes: Number(e.target.value) })
                  }
                  className="w-20"
                />
                <span className="text-sm text-muted-foreground">minutes</span>
              </div>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="long-break-after">Long Break After</Label>
              <div className="flex items-center gap-2">
                <Input
                  id="long-break-after"
                  type="number"
                  min={1}
                  max={10}
                  value={settings.long_break_after}
                  onChange={(e) =>
                    updateSettings({ long_break_after: Number(e.target.value) })
                  }
                  className="w-20"
                />
                <span className="text-sm text-muted-foreground">pomodoros</span>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Behavior */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-medium">Behavior</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <Label htmlFor="auto-start-break" className="cursor-pointer">
                Auto-start Breaks
              </Label>
              <p className="text-xs text-muted-foreground">
                Automatically start the break timer when a work phase ends.
              </p>
            </div>
            <Switch
              id="auto-start-break"
              checked={settings.auto_start_break}
              onCheckedChange={(checked) =>
                updateSettings({ auto_start_break: checked })
              }
            />
          </div>

          <div className="flex items-center justify-between">
            <div>
              <Label htmlFor="auto-start-work" className="cursor-pointer">
                Auto-start Work
              </Label>
              <p className="text-xs text-muted-foreground">
                Automatically start the next work phase when a break ends.
              </p>
            </div>
            <Switch
              id="auto-start-work"
              checked={settings.auto_start_work}
              onCheckedChange={(checked) =>
                updateSettings({ auto_start_work: checked })
              }
            />
          </div>
        </CardContent>
      </Card>

      {/* Sound */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-medium">Sound</CardTitle>
        </CardHeader>
        <CardContent className="space-y-5">
          <div className="flex items-center justify-between">
            <div>
              <Label htmlFor="sound-enabled" className="cursor-pointer">
                Sound Effects
              </Label>
              <p className="text-xs text-muted-foreground">
                Play tones when a phase starts or ends.
              </p>
            </div>
            <Switch
              id="sound-enabled"
              checked={settings.sound_enabled}
              onCheckedChange={(checked) =>
                updateSettings({ sound_enabled: checked })
              }
            />
          </div>

          <div className="flex items-center justify-between">
            <div>
              <Label htmlFor="tick-enabled" className="cursor-pointer">
                Tick Sound
              </Label>
              <p className="text-xs text-muted-foreground">
                Play a subtle tick each second while the countdown is running.
              </p>
            </div>
            <Switch
              id="tick-enabled"
              checked={settings.tick_enabled}
              onCheckedChange={(checked) =>
                updateSettings({ tick_enabled: checked })
              }
            />
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>Volume</Label>
              <span className="text-xs text-muted-foreground">
                {Math.round(settings.volume * 100)}%
              </span>
            </div>
            <Slider
              min={0}
              max={100}
              step={1}
              value={[Math.round(settings.volume * 100)]}
              onValueChange={(val) => {
                const v = Array.isArray(val) ? val[0] : (val as number);
                updateSettings({ volume: v / 100 });
              }}
              disabled={!settings.sound_enabled}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="white-noise">White Noise</Label>
            <Select
              value={settings.white_noise}
              onValueChange={(val) =>
                updateSettings({
                  white_noise: val as typeof settings.white_noise,
                })
              }
            >
              <SelectTrigger id="white-noise" className="w-52">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {WHITE_NOISE_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              Background noise to help you focus during work phases.
            </p>
          </div>

          <div className="pt-1">
            <Button
              variant="outline"
              size="sm"
              onClick={() => playWorkComplete()}
              disabled={!settings.sound_enabled}
            >
              <Volume2 className="h-4 w-4" />
              Preview Sound
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
