import { randomUUID } from "node:crypto";
import fs from "node:fs";
import path from "node:path";

/** One Weixin bot account relayed to one Multica agent. */
export interface Installation {
  id: string;
  workspace_id: string;
  agent_id: string;
  /** The plugin's normalized account id ("<hex>-im-bot"). */
  account_id: string;
  installer_user_id: string;
  /** Personal access token of the installer; the Chats are theirs. */
  token: string;
  /** Id of that token in Multica, so disconnecting can revoke it. */
  token_id: string;
  created_at: string;
}

export type NewInstallation = Omit<Installation, "id" | "created_at">;

export class InstallationStore {
  private readonly file: string;
  private installations: Installation[];

  constructor(stateDir: string) {
    this.file = path.join(stateDir, "installations.json");
    try {
      this.installations = JSON.parse(fs.readFileSync(this.file, "utf-8")) as Installation[];
    } catch {
      this.installations = [];
    }
  }

  list(): Installation[] {
    return [...this.installations];
  }

  get(id: string): Installation | undefined {
    return this.installations.find((i) => i.id === id);
  }

  add(input: NewInstallation): Installation {
    const installation: Installation = {
      ...input,
      id: randomUUID(),
      created_at: new Date().toISOString(),
    };
    this.installations.push(installation);
    this.persist();
    return installation;
  }

  remove(id: string): Installation | undefined {
    const installation = this.get(id);
    if (!installation) return undefined;
    this.installations = this.installations.filter((i) => i.id !== id);
    this.persist();
    return installation;
  }

  private persist(): void {
    fs.mkdirSync(path.dirname(this.file), { recursive: true });
    // Holds access tokens: owner-only.
    fs.writeFileSync(this.file, `${JSON.stringify(this.installations, null, 2)}\n`, {
      mode: 0o600,
    });
  }
}
