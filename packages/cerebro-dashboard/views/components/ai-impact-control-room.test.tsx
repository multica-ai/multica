// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AIImpactControlRoom } from "./ai-impact-control-room";
afterEach(cleanup);
describe("AIImpactControlRoom",()=>{it("renders Value Flow, evidence states, three views and drill-down",()=>{const onEvidence=vi.fn();render(<AIImpactControlRoom data={{period_start:"2026-07-01T00:00:00Z",period_end:"2026-07-17T00:00:00Z",realized_cash_cents:1000,approved_capacity_cents:500,estimated_value_cents:800,ai_cost_cents:200,implementation_cost_cents:100,net_value_cents:1200,decision:"Scale",evidence_status:"Measured",confidence:.82,functions:[],quality_guardrails:[]}} loading={false} onOpenEvidence={onEvidence}/>);for(const label of ["AI cost","Capacity released","Outcome value","Net value","Overview","Functions","Quality & Risk","Measured"])expect(screen.getByText(label)).toBeTruthy();fireEvent.click(screen.getByRole("button",{name:"Open evidence"}));expect(onEvidence).toHaveBeenCalled()})});
