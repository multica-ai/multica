export function agentChatUrl(inboxPath: string, agentId: string): string {
  return `${inboxPath}?chat=new-chat&agent=${encodeURIComponent(agentId)}`;
}
