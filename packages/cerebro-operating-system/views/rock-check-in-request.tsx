import { useState } from "react";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useCreateChannel } from "@multica/core/channels";
import { useWorkspaceSlug } from "@multica/core/paths";
import { buildCheckInRequestMessage } from "../core/check-in-request";
import type { Rock, Terminology } from "../core/types";

/**
 * Sends the check-in as a message to the rock owner instead of waiting for them
 * to open this page. The message lands in a direct conversation, so the owner
 * answers by replying there.
 */
export function RockCheckInRequest({ rock, terminology }: { rock: Rock; terminology: Terminology }) {
  const slug = useWorkspaceSlug();
  const currentUserId = useAuthStore((s) => s.user?.id);
  const createChannel = useCreateChannel();
  const [state, setState] = useState<"idle" | "sending" | "sent">("idle");
  const [error, setError] = useState("");

  const ownerName = rock.owner_name || "the owner";
  const ownerIsMember = rock.owner_type === "member" && !!rock.owner_id;
  const ownerIsMe = ownerIsMember && rock.owner_id === currentUserId;

  async function send() {
    setState("sending");
    setError("");
    try {
      const channel = await createChannel.mutateAsync({ kind: "dm", name: "", member_ids: [rock.owner_id as string], agent_ids: [] });
      await api.createComment(channel.id, buildCheckInRequestMessage(rock, terminology, slug ? `/${slug}/rocks` : null));
      setState("sent");
    } catch (err) {
      setState("idle");
      setError(err instanceof Error ? err.message : "Could not send the check-in request");
    }
  }

  if (!ownerIsMember) {
    return <p className="mt-3 text-xs text-muted-foreground">{rock.owner_id ? `${ownerName} is an agent, so the check-in stays on this page.` : `Give this ${terminology.rock} an owner to send them the check-in as a message.`}</p>;
  }
  if (ownerIsMe) {
    return <p className="mt-3 text-xs text-muted-foreground">You own this {terminology.rock} — fill in the check-in below.</p>;
  }

  return (
    <div className="mt-3">
      <button type="button" onClick={send} disabled={state !== "idle"} className="h-9 w-full rounded-md border px-3 text-sm font-medium">
        {state === "sending" ? "Sending…" : `Ask ${ownerName} for a check-in`}
      </button>
      {state === "sent" && <p className="mt-2 text-xs text-muted-foreground">Sent to {ownerName}. They answer by replying to the message.</p>}
      {error && <p className="mt-2 text-xs text-destructive">{error}</p>}
    </div>
  );
}
