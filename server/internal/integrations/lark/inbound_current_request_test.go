package lark

import (
	"fmt"
	"strings"
	"testing"
)

func currentRequest(body string) string {
	start := strings.Index(body, "<current_request ")
	if start < 0 {
		return ""
	}
	end := strings.Index(body[start:], "</current_request>")
	if end < 0 {
		return ""
	}
	return body[start : start+end]
}

func TestEnrichBareMentionDoesNotRepeatAnsweredQuestions(t *testing.T) {
	fake := newEnricherFake()
	fake.byChat["oc_g"] = []LarkMessage{textMsg("om_new", "ou_alice", "Which two scenes and what UI?", "99000"), cardMsg("om_answer", "80000"), textMsg("om_old", "ou_alice", "Explain value and free quota", "70000")}
	in := InboundMessage{MessageID: "om_at", MessageType: "text", ChatID: "oc_g", ChatType: ChatTypeGroup, SenderOpenID: "ou_alice", AddressedToBot: true, CreateTime: "100000"}
	out := enrich(t, fake, in, groupCfg())
	request := currentRequest(out.Body)
	if !strings.Contains(request, "om_new") || !strings.Contains(request, "Which two scenes and what UI?") || strings.Contains(out.Body, "free quota") {
		t.Fatalf("wrong current request: %s", out.Body)
	}
	if out.MessageID != in.MessageID || out.ChatID != in.ChatID || out.CommandBody != in.CommandBody {
		t.Fatal("inference changed delivery or command source")
	}
	if len(fake.listCalls) != 1 {
		t.Fatalf("extra network calls: %v", fake.listCalls)
	}
}

func TestEnrichBareMentionBoundaries(t *testing.T) {
	in := InboundMessage{MessageID: "om_at", MessageType: "text", ChatID: "oc_g", ChatType: ChatTypeGroup, SenderOpenID: "ou_alice", AddressedToBot: true, CreateTime: "1000000"}
	mentioned := textMsg("om_already", "ou_alice", "@_user_1 do old work", "999000")
	mentioned.Mentions = []LarkMessageMention{{Key: "@_user_1", ID: "ou_bot"}}
	sibling := textMsg("om_sibling", "ou_alice", "Sibling request", "999000")
	sibling.ThreadID = "th_other"
	deleted := textMsg("om_deleted", "ou_alice", "Deleted request", "999000")
	deleted.Deleted = true
	cases := []struct {
		name  string
		items []LarkMessage
		want  string
	}{
		{"other speaker", []LarkMessage{textMsg("om_other", "ou_other", "Someone else's task", "999000")}, ""},
		{"old", []LarkMessage{textMsg("om_old", "ou_alice", "Old task", "1000")}, ""},
		{"future", []LarkMessage{textMsg("om_future", "ou_alice", "Future task", "1000001")}, ""},
		{"missing timestamp", []LarkMessage{textMsg("om_unknown", "ou_alice", "Unknown time", "")}, ""},
		{"already addressed", []LarkMessage{mentioned}, ""},
		{"sibling topic", []LarkMessage{sibling}, ""},
		{"deleted", []LarkMessage{deleted}, ""},
		{"card boundary", []LarkMessage{cardMsg("om_answer", "999500"), textMsg("om_old", "ou_alice", "Old task", "999000")}, ""},
		{"consecutive supplements", []LarkMessage{textMsg("om_b", "ou_alice", "And show UI", "999500"), textMsg("om_a", "ou_alice", "Pick two scenes", "999000")}, "Pick two scenes"},
		{"interleaved boundary", []LarkMessage{textMsg("om_c", "ou_alice", "My new task", "999800"), textMsg("om_b", "ou_other", "Other task", "999500"), textMsg("om_a", "ou_alice", "Earlier task", "999000")}, "My new task"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newEnricherFake()
			fake.byChat["oc_g"] = tc.items
			out := enrich(t, fake, in, groupCfg())
			r := currentRequest(out.Body)
			if tc.want == "" {
				if !strings.Contains(r, `status="needs_clarification"`) {
					t.Fatalf("expected clarification: %s", out.Body)
				}
			} else if !strings.Contains(r, tc.want) {
				t.Fatalf("missing current request: %s", out.Body)
			}
			if strings.Contains(r, "Earlier task") || strings.Contains(r, "Other task") {
				t.Fatalf("crossed speaker boundary: %s", out.Body)
			}
		})
	}
}

func TestEnrichCurrentRequestKeepsExplicitSummaryAndQuote(t *testing.T) {
	fake := newEnricherFake()
	fake.byChat["oc_g"] = []LarkMessage{textMsg("om_old", "ou_alice", "Prior discussion", "90000")}
	in := InboundMessage{MessageID: "om_at", MessageType: "text", ChatID: "oc_g", ChatType: ChatTypeGroup, SenderOpenID: "ou_alice", AddressedToBot: true, CreateTime: "100000", Body: "Summarize our discussion"}
	out := enrich(t, fake, in, groupCfg())
	if !strings.Contains(currentRequest(out.Body), "Summarize our discussion") || !strings.Contains(out.Body, "Prior discussion") {
		t.Fatalf("lost summary request or history: %s", out.Body)
	}
	in.Body = ""
	in.ParentID = "om_quoted"
	fake.byID[in.ParentID] = []LarkMessage{textMsg(in.ParentID, "ou_other", "Explain this again", "1000")}
	out = enrich(t, fake, in, groupCfg())
	if !strings.Contains(currentRequest(out.Body), "om_quoted") || strings.Contains(currentRequest(out.Body), "Prior discussion") {
		t.Fatalf("explicit quote not selected: %s", out.Body)
	}
}

func TestEnrichBareMentionNeverFallsBackFromMissingQuote(t *testing.T) {
	fake := newEnricherFake()
	fake.byChat["oc_g"] = []LarkMessage{textMsg("om_recent", "ou_alice", "Unrelated new task", "99900")}
	in := InboundMessage{MessageID: "om_at", MessageType: "text", ChatID: "oc_g", ChatType: ChatTypeGroup, SenderOpenID: "ou_alice", AddressedToBot: true, CreateTime: "100000", ParentID: "om_missing"}
	out := enrich(t, fake, in, groupCfg())
	if !strings.Contains(currentRequest(out.Body), `status="needs_clarification"`) || strings.Contains(out.Body, "Unrelated new task") {
		t.Fatalf("fell back from missing explicit quote: %s", out.Body)
	}
}

func TestEnrichBareMentionStopsAtPreviousTrigger(t *testing.T) {
	fake := newEnricherFake()
	earlier := textMsg("om_first_at", "ou_alice", "@_user_1", "99900")
	earlier.Mentions = []LarkMessageMention{{Key: "@_user_1", ID: "ou_bot"}}
	fake.byChat["oc_g"] = []LarkMessage{earlier, textMsg("om_task", "ou_alice", "Still running or failed task", "98000")}
	in := InboundMessage{MessageID: "om_second_at", MessageType: "text", ChatID: "oc_g", ChatType: ChatTypeGroup, SenderOpenID: "ou_alice", AddressedToBot: true, CreateTime: "100000"}
	out := enrich(t, fake, in, groupCfg())
	if !strings.Contains(currentRequest(out.Body), `status="needs_clarification"`) || strings.Contains(out.Body, "Still running or failed task") {
		t.Fatalf("restarted a prior turn: %s", out.Body)
	}
}

func TestEnrichBareMentionDoesNotTruncateLongMessageRun(t *testing.T) {
	fake := newEnricherFake()
	for i := 0; i < 4; i++ {
		fake.byChat["oc_g"] = append(fake.byChat["oc_g"], textMsg(fmt.Sprint("om_", i), "ou_alice", fmt.Sprint("Part ", i), fmt.Sprint(99000+i)))
	}
	in := InboundMessage{MessageID: "om_at", MessageType: "text", ChatID: "oc_g", ChatType: ChatTypeGroup, SenderOpenID: "ou_alice", AddressedToBot: true, CreateTime: "100000"}
	out := enrich(t, fake, in, groupCfg())
	if !strings.Contains(currentRequest(out.Body), `status="needs_clarification"`) {
		t.Fatalf("silently truncated task: %s", out.Body)
	}
}

func TestEnrichBareMentionIncludesPrecedingMessageInSameSecond(t *testing.T) {
	fake := newEnricherFake()
	fake.byChat["oc_g"] = []LarkMessage{textMsg("om_task", "ou_alice", "New request", "100100")}
	in := InboundMessage{MessageID: "om_at", MessageType: "text", ChatID: "oc_g", ChatType: ChatTypeGroup, SenderOpenID: "ou_alice", AddressedToBot: true, CreateTime: "100200"}
	out := enrich(t, fake, in, groupCfg())
	if fake.listParams[0].EndTime != 101 || !strings.Contains(currentRequest(out.Body), "om_task") {
		t.Fatalf("same-second message omitted: %s; %+v", out.Body, fake.listParams)
	}
}
