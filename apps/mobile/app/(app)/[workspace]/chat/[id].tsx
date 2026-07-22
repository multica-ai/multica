/**
 * Chat conversation for an existing session. Thin route wrapper — all
 * rendering and logic lives in ChatConversationView, shared with
 * chat/new.tsx.
 */
import { useLocalSearchParams } from "expo-router";
import { ChatConversationView } from "@/components/chat/chat-conversation";

export default function ChatSessionPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <ChatConversationView mode="session" sessionId={id} />;
}
