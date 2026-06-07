"use client";

import { DemoIssueDetail } from "./demo-issue-detail";
import { DemoProductFrame } from "./demo-product-frame";
import { ValueDemoFrame } from "./value-demo-frame";

const DEMO_H = 720;

export function ValueDelegateDemo() {
  return (
    <ValueDemoFrame height={DEMO_H}>
      <DemoProductFrame activeTab="issues" pathname="/demo/issues/issue-137">
        <DemoIssueDetail issueId="issue-137" initialScrollTop={720} />
      </DemoProductFrame>
    </ValueDemoFrame>
  );
}
