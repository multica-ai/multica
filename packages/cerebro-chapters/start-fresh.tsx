"use client";

import { useState } from "react";
import { Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@multica/ui/components/ui/dialog";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Input } from "@multica/ui/components/ui/input";
import { useStartFresh } from "./use-chapters";

export function StartFresh({ issueId }: { issueId: string }) {
  const [open, setOpen] = useState(false);
  const [summary, setSummary] = useState("");
  const [done, setDone] = useState("");
  const [remaining, setRemaining] = useState("");
  const [planRef, setPlanRef] = useState("");
  const mutation = useStartFresh(issueId);

  async function submit() {
    await mutation.mutateAsync({
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
