import { z } from "zod";

/** An app the signed-in account chose not to be asked about again. */
export interface ComputerUseApprovedApp {
  id: string;
  name: string;
}

export interface ComputerUseApprovals {
  /** False when this device has no Computer Use (non-macOS or not started). */
  available: boolean;
  apps: ComputerUseApprovedApp[];
}

export interface ComputerUseApprovalsClient {
  list(): Promise<ComputerUseApprovals>;
  remove(appId: string): Promise<ComputerUseApprovals>;
  clear(): Promise<ComputerUseApprovals>;
}

export const computerUseApprovalsSchema = z.object({
  available: z.boolean(),
  apps: z.array(z.object({ id: z.string(), name: z.string() })),
});
