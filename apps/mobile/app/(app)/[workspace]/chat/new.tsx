/**
 * Chat conversation for a not-yet-created session — reached from the
 * session list's "+" button (or its agent picker) with a chosen agent
 * but no session yet. On first send, ChatConversationView creates the
 * session and router.replace()s to chat/[id].
 */
import { useLocalSearchParams } from "expo-router";
import { ChatConversationView } from "@/components/chat/chat-conversation";

export default function ChatNewPage() {
  const { agentId } = useLocalSearchParams<{ agentId: string }>();
  return <ChatConversationView mode="new" agentId={agentId} />;
}
