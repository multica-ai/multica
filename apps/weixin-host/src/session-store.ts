import fs from "node:fs";
import path from "node:path";

/**
 * Maps one Weixin conversation (bot account + peer) to the Multica chat
 * session that carries it, so every message from the same person continues
 * the same Chat with the agent.
 */
export class SessionStore {
  private readonly file: string;
  private sessions: Record<string, string>;

  constructor(stateDir: string) {
    this.file = path.join(stateDir, "multica-sessions.json");
    try {
      this.sessions = JSON.parse(fs.readFileSync(this.file, "utf-8")) as Record<string, string>;
    } catch {
      this.sessions = {};
    }
  }

  get(accountId: string, peerId: string): string | undefined {
    return this.sessions[key(accountId, peerId)];
  }

  set(accountId: string, peerId: string, sessionId: string): void {
    this.sessions[key(accountId, peerId)] = sessionId;
    this.persist();
  }

  delete(accountId: string, peerId: string): void {
    delete this.sessions[key(accountId, peerId)];
    this.persist();
  }

  private persist(): void {
    fs.mkdirSync(path.dirname(this.file), { recursive: true });
    fs.writeFileSync(this.file, `${JSON.stringify(this.sessions, null, 2)}\n`);
  }
}

function key(accountId: string, peerId: string): string {
  return `${accountId}/${peerId}`;
}
