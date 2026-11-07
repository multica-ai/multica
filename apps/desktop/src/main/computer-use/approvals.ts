import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
import { z } from "zod";
import type { ComputerUseApprovedApp } from "../../shared/computer-use-approvals";

const appSchema = z.object({ id: z.string().min(1), name: z.string().min(1) });
const fileSchema = z.object({ schemaVersion: z.literal(1), users: z.record(z.string(), z.array(appSchema)) });

/** Apps each account chose not to be asked about again, kept across turns and restarts. */
export class AppApprovals {
  private users: Record<string, ComputerUseApprovedApp[]>;
  constructor(private readonly file: string) {
    try {
      this.users = fileSchema.parse(JSON.parse(readFileSync(file, "utf8"))).users;
    } catch {
      this.users = {};
    }
  }
  list(userId: string): ComputerUseApprovedApp[] {
    return [...(this.users[userId] ?? [])];
  }
  has(userId: string, appId: string): boolean {
    return this.users[userId]?.some((app) => app.id === appId) ?? false;
  }
  add(userId: string, app: ComputerUseApprovedApp): void {
    if (this.has(userId, app.id)) return;
    this.users[userId] = [...(this.users[userId] ?? []), { id: app.id, name: app.name }];
    this.save();
  }
  remove(userId: string, appId: string): void {
    const apps = this.users[userId];
    if (!apps?.some((app) => app.id === appId)) return;
    const rest = apps.filter((app) => app.id !== appId);
    if (rest.length) this.users[userId] = rest;
    else delete this.users[userId];
    this.save();
  }
  clear(userId: string): void {
    if (!this.users[userId]) return;
    delete this.users[userId];
    this.save();
  }
  private save(): void {
    mkdirSync(dirname(this.file), { recursive: true });
    const temporary = `${this.file}.tmp`;
    writeFileSync(temporary, JSON.stringify({ schemaVersion: 1, users: this.users }), { mode: 0o600 });
    renameSync(temporary, this.file);
  }
}
