/**
 * FIR-31 — accumulated session cost shown in the chat header (mobile).
 *
 * Mirrors web's session-total chip (packages/cerebro-chat/.../session-cost-chip.tsx
 * + SessionCostChip): same `GET /api/chat/sessions/:id/usage` source, same
 * `cost_cents → $X.XX` formatting, same default-on `cerebro_chat_message_cost`
 * gate. UI diverges intentionally: mobile renders a compact "cost $X.XX" label
 * in the native iOS header (no pill / hover tooltip — there's no hover on
 * touch). Self-hides until the session has logged spend.
 */
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { chatSessionUsageOptions } from "@/data/queries/chat";
import { useChatMessageCostEnabled } from "@/data/queries/feature-flags";

export function ChatSessionCost({ sessionId }: { sessionId: string | null }) {
  const enabled = useChatMessageCostEnabled();
  const { data: usage } = useQuery({
    ...chatSessionUsageOptions(sessionId),
    enabled: enabled && !!sessionId,
  });

  if (!enabled || !sessionId || !usage || usage.task_count <= 0) return null;

  return (
    <Text className="text-xs font-medium text-muted-foreground">
      cost {formatCost(usage.cost_cents)}
    </Text>
  );
}

// Mirror web's formatChatCost rule.
function formatCost(cents: number): string {
  const usd = cents / 100;
  if (usd === 0) return "$0.00";
  if (usd < 0.01) return "<$0.01";
  return `$${usd.toFixed(2)}`;
}
