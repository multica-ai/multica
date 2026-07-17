// @vitest-environment jsdom
import React from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PeopleControlRoom } from "./people-control-room";
afterEach(cleanup);
const people=[{id:"m1",type:"member" as const,name:"Maya",activity:[{bucket:"2026-07-17",count:4}],usage:{runs:2,issues:3,projects:1,chats:5,channels:2},outcomes:{needs_solved:8,solution_quality:.91,frustration_free:.84,prompt_effectiveness:.79,skill_activity:3,cost_cents:42},confidence:.76,sample_size:12},{id:"a1",type:"agent" as const,name:"Lone",activity:[{bucket:"2026-07-17",count:7}],usage:{runs:7,issues:4,projects:2,chats:1,channels:3},outcomes:{needs_solved:6,solution_quality:.88,frustration_free:.9,prompt_effectiveness:.81,skill_activity:5,cost_cents:120},confidence:.8,sample_size:15}];
describe("PeopleControlRoom",()=>{it("renders members and agents in B2 split view without Tasks or ranking",()=>{const onPeriodChange=vi.fn();render(<PeopleControlRoom people={people} period="day" onPeriodChange={onPeriodChange} loading={false}/>);expect(screen.getByRole("button",{name:/Maya/})).toBeTruthy();expect(screen.getByRole("button",{name:/Lone/})).toBeTruthy();expect(screen.getByText("Needs solved")).toBeTruthy();for(const label of ["Runs","Issues","Projects","Chats","Channels"]) expect(screen.getByText(label)).toBeTruthy();expect(screen.queryByText("Tasks")).toBeNull();fireEvent.click(screen.getByRole("button",{name:"Month"}));expect(onPeriodChange).toHaveBeenCalledWith("month")})});
