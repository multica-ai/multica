import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { indexCommentText } from "./comment-annotation-selection";
import { DescriptionAnchorLinks, descriptionAnchorRange } from "./published-description-anchors";
import type { DescriptionAnchor } from "@multica/core/drafts/description-anchor";

describe("published description navigation", () => {
  it("maps rendered Markdown text and Unicode offsets to a real DOM Range", () => {
    const root=document.createElement("div");root.innerHTML='<h1>Title</h1><p>中文 🐟 <strong>quiet release</strong></p><pre><code>a\n  b</code></pre>';
    const text=indexCommentText(root,true).text,start=text.indexOf("quiet release");
    const anchor:DescriptionAnchor={version:1,id:"a",issue_id:"issue",quote:"quiet release",start,prefix:text.slice(Math.max(0,start-32),start),suffix:text.slice(start+13,start+45),source_text:text};
    expect(descriptionAnchorRange(root,anchor)?.toString()).toBe("quiet release");
    root.prepend(document.createTextNode("Intro\n"));
    expect(descriptionAnchorRange(root,anchor)?.toString()).toBe("quiet release");
    root.querySelector("strong")!.textContent="removed";
    expect(descriptionAnchorRange(root,anchor)).toBeNull();
  });
  it("scopes back navigation to this surface and preserves the quote when outdated", async () => {
    const source="Original quote\n";
    const anchor:DescriptionAnchor={version:1,id:"a",issue_id:"issue",quote:"Original quote",start:0,prefix:"",suffix:"\n",source_text:source};
    const {container}=renderWithI18n(<><div data-mc="issue-detail"><div data-comment-content="description:issue"><p>Other surface</p></div></div><div data-mc="issue-detail"><div data-description-anchor-scroll><div data-comment-content="description:issue"><p>Original quote</p></div><DescriptionAnchorLinks issueId="issue" anchors={[anchor]}/></div></div></>);
    const button=await screen.findByRole("button",{name:"Back to description: Original quote"});await waitFor(()=>expect(button).toBeEnabled());
    fireEvent.click(button);expect(window.getSelection()?.toString()).toBe("Original quote");
    await act(async()=>{container.querySelectorAll('[data-comment-content="description:issue"]')[1]!.textContent="Removed"});
    await waitFor(()=>expect(button).toBeDisabled());expect(screen.getByText(/Outdated/)).toBeVisible();expect(button).toHaveTextContent("Original quote");
  });
});
