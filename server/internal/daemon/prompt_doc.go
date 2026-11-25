package daemon

import (
	"fmt"
	"strings"
)

// buildDocCommentPrompt is the per-turn prompt of a run started from a doc
// discussion (MUL-9933). The doc, discussion and reply command are re-emitted
// on every turn: runs on one doc share a session, so a resumed session must
// never reply under an earlier discussion's --parent.
func buildDocCommentPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "You were asked for in a discussion on the doc %q (doc ID: %s, discussion ID: %s).\n\n", task.DocTitle, task.DocID, task.DocThreadID)
	if quote := strings.TrimSpace(task.DocThreadQuote); quote != "" {
		b.WriteString("The discussion is about this text in the doc (it may have changed since):\n\n")
		writeQuoted(&b, quote)
	}
	if content := strings.TrimSpace(task.DocCommentContent); content != "" {
		author := task.DocCommentAuthorName
		if author == "" {
			author = "Someone"
		}
		fmt.Fprintf(&b, "[NEW COMMENT] %s just wrote this in the discussion. Focus on THIS comment — earlier ones are context:\n\n", author)
		writeQuoted(&b, content)
	} else {
		b.WriteString("The comment that asked for you has been deleted. Read the discussion and answer what is still open in it.\n\n")
	}

	b.WriteString("Workflow:\n\n")
	fmt.Fprintf(&b, "1. Read the doc: `multica doc get %s --output content`.\n", task.DocID)
	fmt.Fprintf(&b, "2. Read the whole discussion: `multica doc comment list %s --thread %s --output json`.\n", task.DocID, task.DocThreadID)
	b.WriteString("3. Do what the comment asks. When it asks for a change to the doc, or changing the doc is clearly the best answer, make the change yourself: `multica doc edit` for targeted changes, `multica doc update` only to rewrite the whole doc (see `multica doc --help`). Every change is kept in the doc's history and can be restored.\n")
	b.WriteString("4. Reply in this discussion before you exit — this step is mandatory. Write the reply to a UTF-8 file in your working directory, then post it:\n\n")
	fmt.Fprintf(&b, "    multica doc comment add %s --parent %s --content-file ./reply.md --output table && rm ./reply.md\n\n", task.DocID, task.DocThreadID)
	b.WriteString("Post one reply with your result, not progress updates. You can only reply in this discussion: starting a new one, resolving it and deleting comments are left to people.\n")
	return b.String()
}

func writeQuoted(b *strings.Builder, text string) {
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintf(b, "> %s\n", line)
	}
	b.WriteString("\n")
}
