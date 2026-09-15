package lark

import (
	"strings"
	"testing"
)

const testDocumentURL = "https://acme.feishu.cn/docx/AbcdefghijkLMNop"

func TestParseFeishuDocumentURLAcceptsSupportedCloudsAndKinds(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantKind  DocumentKind
		wantToken string
	}{
		{name: "mainland docx", raw: "https://acme.feishu.cn/docx/AbcdefghijkLMNop", wantKind: DocumentKindDocx, wantToken: "AbcdefghijkLMNop"},
		{name: "international wiki with harmless share query", raw: "https://acme.larksuite.com/wiki/WikidefghijkLMNop?from=from_copylink", wantKind: DocumentKindWiki, wantToken: "WikidefghijkLMNop"},
		{name: "legacy international host", raw: "https://acme.larkoffice.com/docx/Legacy2345678901", wantKind: DocumentKindDocx, wantToken: "Legacy2345678901"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFeishuDocumentURL(tt.raw)
			if err != nil {
				t.Fatalf("ParseFeishuDocumentURL() error = %v", err)
			}
			if got.Kind != tt.wantKind || got.Token != tt.wantToken {
				t.Fatalf("ParseFeishuDocumentURL() = %+v, want kind=%q token=%q", got, tt.wantKind, tt.wantToken)
			}
		})
	}
}

func TestParseFeishuDocumentURLRejectsIdentityAndEndpointConfusion(t *testing.T) {
	tests := []string{
		"AbcdefghijkLMNop",
		"http://acme.feishu.cn/docx/AbcdefghijkLMNop",
		"https://evilfeishu.cn/docx/AbcdefghijkLMNop",
		"https://feishu.cn.evil.example/docx/AbcdefghijkLMNop",
		"https://127.0.0.1/docx/AbcdefghijkLMNop",
		"https://user@acme.feishu.cn/docx/AbcdefghijkLMNop",
		"https://acme.feishu.cn:8443/docx/AbcdefghijkLMNop",
		"https://acme.feishu.cn/docx/AbcdefghijkLMNop#fragment",
		"https://acme.feishu.cn/drive/AbcdefghijkLMNop",
		"https://acme.feishu.cn/docx/short",
		"https://acme.feishu.cn/docx/AbcdefghijkLMNop/children",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseFeishuDocumentURL(raw); err == nil {
				t.Fatalf("ParseFeishuDocumentURL(%q) unexpectedly succeeded", raw)
			}
		})
	}
}

func TestValidateDocumentToolInputAppliesOperationContracts(t *testing.T) {
	tests := []struct {
		name string
		op   DocumentOperation
		in   DocumentToolInput
	}{
		{name: "fetch defaults to full", op: DocumentOperationFetch, in: DocumentToolInput{DocumentURL: testDocumentURL}},
		{name: "fetch section", op: DocumentOperationFetch, in: DocumentToolInput{DocumentURL: testDocumentURL, Scope: DocumentFetchScopeSection, StartBlockID: "section-heading"}},
		{name: "fetch keyword", op: DocumentOperationFetch, in: DocumentToolInput{DocumentURL: testDocumentURL, Scope: DocumentFetchScopeKeyword, Keyword: "quarterly result"}},
		{name: "append small text", op: DocumentOperationAppend, in: DocumentToolInput{DocumentURL: testDocumentURL, Content: "status update"}},
		{name: "replace unique text request", op: DocumentOperationReplaceText, in: DocumentToolInput{DocumentURL: testDocumentURL, Pattern: "old", Content: "new"}},
		{name: "insert after block", op: DocumentOperationInsertAfter, in: DocumentToolInput{DocumentURL: testDocumentURL, BlockID: "doxcn_target-1", Content: "new paragraph"}},
		{name: "replace one block", op: DocumentOperationReplaceBlock, in: DocumentToolInput{DocumentURL: testDocumentURL, BlockID: "doxcn_target-1", Content: "replacement"}},
		{name: "delete confirmed block", op: DocumentOperationDeleteBlock, in: DocumentToolInput{DocumentURL: testDocumentURL, BlockID: "doxcn_target-1", Confirmed: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, toolErr := ValidateDocumentToolInput(tt.op, tt.in)
			if toolErr != nil {
				t.Fatalf("ValidateDocumentToolInput() error = %v", toolErr)
			}
			if got.Operation != tt.op || got.Document.Token != "AbcdefghijkLMNop" {
				t.Fatalf("ValidateDocumentToolInput() = %+v", got)
			}
			if tt.name == "fetch defaults to full" && got.Input.Scope != DocumentFetchScopeFull {
				t.Fatalf("default scope = %q, want %q", got.Input.Scope, DocumentFetchScopeFull)
			}
		})
	}
}

func TestValidateDocumentToolInputRejectsUnsafeShapesAndBounds(t *testing.T) {
	tests := []struct {
		name string
		op   DocumentOperation
		in   DocumentToolInput
	}{
		{"unknown operation", DocumentOperation("overwrite"), DocumentToolInput{DocumentURL: testDocumentURL}},
		{"section without anchor", DocumentOperationFetch, DocumentToolInput{DocumentURL: testDocumentURL, Scope: DocumentFetchScopeSection}},
		{"keyword scope without keyword", DocumentOperationFetch, DocumentToolInput{DocumentURL: testDocumentURL, Scope: DocumentFetchScopeKeyword}},
		{"range without end", DocumentOperationFetch, DocumentToolInput{DocumentURL: testDocumentURL, Scope: DocumentFetchScopeRange, StartBlockID: "start"}},
		{"append without content", DocumentOperationAppend, DocumentToolInput{DocumentURL: testDocumentURL}},
		{"pattern too large", DocumentOperationReplaceText, DocumentToolInput{DocumentURL: testDocumentURL, Pattern: strings.Repeat("x", DocumentMaxPatternBytes+1), Content: "new"}},
		{"content too large", DocumentOperationAppend, DocumentToolInput{DocumentURL: testDocumentURL, Content: strings.Repeat("x", DocumentMaxContentBytes+1), Confirmed: true}},
		{"large edit without confirmation", DocumentOperationAppend, DocumentToolInput{DocumentURL: testDocumentURL, Content: strings.Repeat("x", DocumentConfirmBytes+1)}},
		{"delete without confirmation", DocumentOperationDeleteBlock, DocumentToolInput{DocumentURL: testDocumentURL, BlockID: "target"}},
		{"multi block replace without confirmation", DocumentOperationReplaceBlock, DocumentToolInput{DocumentURL: testDocumentURL, StartBlockID: "start", EndBlockID: "end", Content: "replacement"}},
		{"block id with slash", DocumentOperationInsertAfter, DocumentToolInput{DocumentURL: testDocumentURL, BlockID: "../target", Content: "new"}},
		{"negative revision", DocumentOperationAppend, DocumentToolInput{DocumentURL: testDocumentURL, Content: "new", RevisionID: -1}},
		{"nul content", DocumentOperationAppend, DocumentToolInput{DocumentURL: testDocumentURL, Content: "before\x00after"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, toolErr := ValidateDocumentToolInput(tt.op, tt.in)
			if toolErr == nil || toolErr.Code != DocumentErrorInvalidRequest {
				t.Fatalf("ValidateDocumentToolInput() error = %+v, want invalid_request", toolErr)
			}
		})
	}
}

func TestDocumentValidationErrorsDoNotEchoRejectedValues(t *testing.T) {
	const sentinel = "SECRET-REJECTED-VALUE"
	_, toolErr := ValidateDocumentToolInput(
		DocumentOperationAppend,
		DocumentToolInput{DocumentURL: "https://evil.example/docx/" + sentinel, Content: sentinel},
	)
	if toolErr == nil {
		t.Fatal("ValidateDocumentToolInput() unexpectedly succeeded")
	}
	if strings.Contains(toolErr.Error(), sentinel) {
		t.Fatalf("validation error leaked rejected input: %v", toolErr)
	}
}
