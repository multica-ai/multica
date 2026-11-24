import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentPresenceIndicator } from "./agent-presence-indicator";
vi.mock("../../i18n",()=>({useT:()=>({t:(select:(x:unknown)=>unknown)=>select({availability:{online:"Online"},workload:{working:"Working"},presence:{queue_badge:"Queued"}})})}));
describe("AgentPresenceIndicator",()=>{
 it("shows the shared running total without a misleading per-user denominator",()=>{
  render(<AgentPresenceIndicator detail={{availability:"online",workload:"working",runningCount:9,queuedCount:0,capacity:null}}/>);
  expect(screen.getByText("9")).toBeInTheDocument();
  expect(screen.queryByText(/9\s*\//)).not.toBeInTheDocument();
 });
});
