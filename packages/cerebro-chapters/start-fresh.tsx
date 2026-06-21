"use client";

import { useState } from "react";
import { Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@multica/ui/components/ui/dialog";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Input } from "@multica/ui/components/ui/input";
import { useStartFresh } from "./use-chapters";
import type { ChapterStartMode } from "./types";

export function StartFresh({ issueId }: { issueId: string }) {
  const [open, setOpen] = useState(false);
  const [summary, setSummary] = useState("");
  const [done, setDone] = useState("");
  const [remaining, setRemaining] = useState("");
  const [planRef, setPlanRef] = useState("");
  const [mode, setMode] = useState<ChapterStartMode>("handoff");
  const mutation = useStartFresh(issueId);

  async function submit() {
    await mutation.mutateAsync({
      mode,
      summary,
      done: done.split("\n").map((s) => s.trim()).filter(Boolean),
      remaining: remaining.split("\n").map((s) => s.trim()).filter(Boolean),
      plan_ref: planRef.trim() || null,
    });
    setOpen(false);
    setSummary("");
    setDone("");
    setRemaining("");
    setPlanRef("");
    setMode("handoff");
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button type="button" variant="outline" size="sm" />}>
        <Plus className="h-4 w-4" />
        Start fresh
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Start fresh</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <Textarea value={summary} onChange={(e) => setSummary(e.target.value)} placeholder="Summary" />
          <Textarea value={done} onChange={(e) => setDone(e.target.value)} placeholder="Done, one per line" />
          <Textarea value={remaining} onChange={(e) => setRemaining(e.target.value)} placeholder="Remaining, one per line" />
          <Input value={planRef} onChange={(e) => setPlanRef(e.target.value)} placeholder="Plan link" />
          <div className="space-y-1.5">
            <div className="text-sm font-medium">New session</div>
            <div className="flex gap-2">
              <Button
                type="button"
                size="sm"
                variant={mode === "handoff" ? "default" : "outline"}
                onClick={() => setMode("handoff")}
              >
                Carry over handoff
              </Button>
              <Button
                type="button"
                size="sm"
                variant={mode === "blank" ? "default" : "outline"}
                onClick={() => setMode("blank")}
              >
                Start blank
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              {mode === "handoff"
                ? "The new chapter starts with this handoff carried over."
                : "The new chapter starts empty — no context carried over. The handoff is still saved on the chapter you're closing."}
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button type="button" onClick={submit} disabled={mutation.isPending}>
            Start fresh
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
