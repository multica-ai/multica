"use client";

import { useCallback, useEffect, useState } from "react";
import { ChevronDown, Clock3, MoreHorizontal } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  deleteScheduledMessage, listScheduledMessages, nextMondayAtNine, sendScheduledMessageNow,
  toLocalInputValue, tomorrowAtNine, updateScheduledMessage, type ScheduledMessage,
} from "./scheduled-message";

export function ScheduledMessageControl({ issueId, disabled, canSchedule, onSchedule }: {
  issueId: string;
  disabled: boolean;
  canSchedule: boolean;
  onSchedule: (sendAt: Date) => Promise<void>;
}) {
  const [customOpen, setCustomOpen] = useState(false);
  const [queueOpen, setQueueOpen] = useState(false);
  const [customTime, setCustomTime] = useState(() => toLocalInputValue(tomorrowAtNine()));
  const [messages, setMessages] = useState<ScheduledMessage[]>([]);
  const [editing, setEditing] = useState<ScheduledMessage | null>(null);
  const [editContent, setEditContent] = useState("");
  const [editTime, setEditTime] = useState("");

  const refresh = useCallback(async () => {
    try { setMessages(await listScheduledMessages(issueId)); }
    catch { toast.error("Could not load scheduled messages"); }
  }, [issueId]);
  useEffect(() => { if (queueOpen) void refresh(); }, [queueOpen, refresh]);

  const schedule = async (date: Date) => {
    try {
      await onSchedule(date);
      toast.success(`Message scheduled for ${date.toLocaleString()}`, {
        action: { label: "View", onClick: () => setQueueOpen(true) },
      });
    }
    catch { toast.error("Could not schedule message"); }
  };

  return <>
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button size="icon-sm" aria-label="Schedule message" disabled={disabled} />}>
        <ChevronDown />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" side="top" className="w-60">
        <DropdownMenuItem disabled={!canSchedule} onClick={() => void schedule(tomorrowAtNine())}>Tomorrow at 9:00 AM</DropdownMenuItem>
        <DropdownMenuItem disabled={!canSchedule} onClick={() => void schedule(nextMondayAtNine())}>Next Monday at 9:00 AM</DropdownMenuItem>
        <DropdownMenuItem disabled={!canSchedule} onClick={() => setCustomOpen(true)}>Custom time…</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => setQueueOpen(true)}><Clock3 className="mr-2 size-4" />Scheduled messages</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>

    <Dialog open={customOpen} onOpenChange={setCustomOpen}>
      <DialogContent>
        <DialogHeader><DialogTitle>Schedule message</DialogTitle><DialogDescription>Choose when this message should be sent.</DialogDescription></DialogHeader>
        <Input aria-label="Send at" type="datetime-local" min={toLocalInputValue(new Date())} value={customTime} onChange={(event) => setCustomTime(event.target.value)} />
        <DialogFooter><Button onClick={() => { const date = new Date(customTime); if (date > new Date()) { setCustomOpen(false); void schedule(date); } }}>Schedule</Button></DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog open={queueOpen} onOpenChange={setQueueOpen}>
      <DialogContent className="max-h-[80vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader><DialogTitle>Scheduled messages</DialogTitle><DialogDescription>Messages waiting to be sent in this conversation.</DialogDescription></DialogHeader>
        {messages.length === 0 ? <p className="py-6 text-center text-sm text-muted-foreground">No scheduled messages</p> : messages.map((message) => (
          <div key={message.id} className="rounded-lg border p-3">
            <div className="mb-2 flex items-start justify-between gap-3"><p className="whitespace-pre-wrap text-sm">{message.content}</p>
              <DropdownMenu><DropdownMenuTrigger render={<Button size="icon-sm" variant="ghost" aria-label="Scheduled message actions" />}><MoreHorizontal /></DropdownMenuTrigger><DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => { setQueueOpen(false); setEditing(message); setEditContent(message.content); setEditTime(toLocalInputValue(new Date(message.send_at))); }}>Edit or reschedule</DropdownMenuItem>
                <DropdownMenuItem onClick={async () => { await sendScheduledMessageNow(message.id); await refresh(); }}>Send now</DropdownMenuItem>
                <DropdownMenuItem className="text-destructive" onClick={async () => { await deleteScheduledMessage(message.id); await refresh(); }}>Delete</DropdownMenuItem>
              </DropdownMenuContent></DropdownMenu>
            </div>
            <p className="text-xs text-muted-foreground">{new Date(message.send_at).toLocaleString()}</p>
          </div>
        ))}
      </DialogContent>
    </Dialog>

    <Dialog open={!!editing} onOpenChange={(open) => { if (!open) setEditing(null); }}>
      <DialogContent><DialogHeader><DialogTitle>Edit scheduled message</DialogTitle><DialogDescription>Update the message or its delivery time.</DialogDescription></DialogHeader>
        <textarea className="min-h-28 rounded-md border bg-background p-3 text-sm" aria-label="Message" value={editContent} onChange={(event) => setEditContent(event.target.value)} />
        <Input aria-label="Send at" type="datetime-local" min={toLocalInputValue(new Date())} value={editTime} onChange={(event) => setEditTime(event.target.value)} />
        <DialogFooter><Button onClick={async () => { if (!editing) return; await updateScheduledMessage(editing.id, { content: editContent, send_at: new Date(editTime).toISOString() }); setEditing(null); await refresh(); setQueueOpen(true); }}>Save</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  </>;
}
