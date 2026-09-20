package lark

import (
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	DocumentMaxRequestBytes  = 256 << 10
	DocumentMaxContentBytes  = 32 << 10
	DocumentMaxPatternBytes  = 2 << 10
	DocumentMaxBlocks        = 20
	DocumentConfirmBytes     = 8 << 10
	DocumentConfirmBlocks    = 5
	DocumentMaxResponseBytes = 4 << 20
)

type DocumentKind string

const (
	DocumentKindDocx DocumentKind = "docx"
	DocumentKindWiki DocumentKind = "wiki"
)

type DocumentRef struct {
	Token string
	Kind  DocumentKind
}

type DocumentOperation string

const (
	DocumentOperationFetch        DocumentOperation = "feishu_docs_fetch"
	DocumentOperationAppend       DocumentOperation = "feishu_docs_append"
	DocumentOperationReplaceText  DocumentOperation = "feishu_docs_replace_text"
	DocumentOperationInsertAfter  DocumentOperation = "feishu_docs_insert_after"
	DocumentOperationReplaceBlock DocumentOperation = "feishu_docs_replace_block"
	DocumentOperationDeleteBlock  DocumentOperation = "feishu_docs_delete_block"
)

type DocumentFetchScope string

const (
	DocumentFetchScopeFull    DocumentFetchScope = "full"
	DocumentFetchScopeOutline DocumentFetchScope = "outline"
	DocumentFetchScopeSection DocumentFetchScope = "section"
	DocumentFetchScopeKeyword DocumentFetchScope = "keyword"
	DocumentFetchScopeRange   DocumentFetchScope = "range"
)

type DocumentToolInput struct {
	DocumentURL  string             `json:"document_url"`
	Scope        DocumentFetchScope `json:"scope,omitempty"`
	Keyword      string             `json:"keyword,omitempty"`
	BlockID      string             `json:"block_id,omitempty"`
	StartBlockID string             `json:"start_block_id,omitempty"`
	EndBlockID   string             `json:"end_block_id,omitempty"`
	Pattern      string             `json:"pattern,omitempty"`
	Content      string             `json:"content,omitempty"`
	RevisionID   int64              `json:"revision_id,omitempty"`
	Confirmed    bool               `json:"confirmed,omitempty"`
}

type ValidatedDocumentRequest struct {
	Operation DocumentOperation
	Document  DocumentRef
	Input     DocumentToolInput
}

type DocumentErrorCode string

const (
	DocumentErrorNotConnected        DocumentErrorCode = "not_connected"
	DocumentErrorCapabilityDenied    DocumentErrorCode = "capability_denied"
	DocumentErrorPermissionDenied    DocumentErrorCode = "permission_denied"
	DocumentErrorInvalidRequest      DocumentErrorCode = "invalid_request"
	DocumentErrorNotFound            DocumentErrorCode = "not_found"
	DocumentErrorConflict            DocumentErrorCode = "conflict"
	DocumentErrorUnsupportedContent  DocumentErrorCode = "unsupported_content"
	DocumentErrorTimeoutUncertain    DocumentErrorCode = "timeout_uncertain"
	DocumentErrorUpstreamUnavailable DocumentErrorCode = "upstream_unavailable"
)

type DocumentToolError struct {
	Code      DocumentErrorCode `json:"code"`
	Message   string            `json:"message"`
	Uncertain bool              `json:"uncertain,omitempty"`
}

func (e *DocumentToolError) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code) + ": " + e.Message
}

var (
	documentPathPattern    = regexp.MustCompile(`^/(docx|wiki)/([A-Za-z0-9_-]{8,128})$`)
	documentBlockIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

func ParseFeishuDocumentURL(raw string) (DocumentRef, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" ||
		parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return DocumentRef{}, invalidDocumentRequest("document_url must be a supported Feishu document URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || net.ParseIP(host) != nil || !isFeishuDocumentHost(host) {
		return DocumentRef{}, invalidDocumentRequest("document_url must use a supported Feishu document host")
	}
	match := documentPathPattern.FindStringSubmatch(parsed.EscapedPath())
	if len(match) != 3 {
		return DocumentRef{}, invalidDocumentRequest("document_url must identify one docx or wiki document")
	}
	return DocumentRef{Kind: DocumentKind(match[1]), Token: match[2]}, nil
}

func isFeishuDocumentHost(host string) bool {
	for _, suffix := range []string{"feishu.cn", "larksuite.com", "larkoffice.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func ValidateDocumentToolInput(operation DocumentOperation, input DocumentToolInput) (ValidatedDocumentRequest, *DocumentToolError) {
	ref, err := ParseFeishuDocumentURL(input.DocumentURL)
	if err != nil {
		return ValidatedDocumentRequest{}, invalidDocumentRequest("document_url is invalid")
	}
	if input.RevisionID < 0 || !validDocumentStrings(input) {
		return ValidatedDocumentRequest{}, invalidDocumentRequest("document input is invalid")
	}
	if len(input.Content) > DocumentMaxContentBytes || len(input.Pattern) > DocumentMaxPatternBytes ||
		len(input.Keyword) > DocumentMaxPatternBytes {
		return ValidatedDocumentRequest{}, invalidDocumentRequest("document input exceeds a fixed limit")
	}
	if input.Content != "" && len(input.Content) > DocumentConfirmBytes && !input.Confirmed {
		return ValidatedDocumentRequest{}, invalidDocumentRequest("confirmed is required for this edit")
	}
	if !validOptionalBlockID(input.BlockID) || !validOptionalBlockID(input.StartBlockID) ||
		!validOptionalBlockID(input.EndBlockID) {
		return ValidatedDocumentRequest{}, invalidDocumentRequest("block identifier is invalid")
	}

	normalized := input
	var validationErr *DocumentToolError
	switch operation {
	case DocumentOperationFetch:
		validationErr = validateFetchInput(&normalized)
	case DocumentOperationAppend:
		validationErr = validateAppendInput(normalized)
	case DocumentOperationReplaceText:
		validationErr = validateReplaceTextInput(normalized)
	case DocumentOperationInsertAfter:
		validationErr = validateInsertAfterInput(normalized)
	case DocumentOperationReplaceBlock:
		validationErr = validateReplaceBlockInput(normalized)
	case DocumentOperationDeleteBlock:
		validationErr = validateDeleteBlockInput(normalized)
	default:
		validationErr = invalidDocumentRequest("document operation is unsupported")
	}
	if validationErr != nil {
		return ValidatedDocumentRequest{}, validationErr
	}
	return ValidatedDocumentRequest{Operation: operation, Document: ref, Input: normalized}, nil
}

func validDocumentStrings(input DocumentToolInput) bool {
	for _, value := range []string{
		input.DocumentURL, string(input.Scope), input.Keyword, input.BlockID,
		input.StartBlockID, input.EndBlockID, input.Pattern, input.Content,
	} {
		if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
			return false
		}
	}
	return true
}

func validOptionalBlockID(value string) bool {
	return value == "" || documentBlockIDPattern.MatchString(value)
}

func validateFetchInput(input *DocumentToolInput) *DocumentToolError {
	if input.Scope == "" {
		input.Scope = DocumentFetchScopeFull
	}
	if input.Content != "" || input.Pattern != "" || input.BlockID != "" ||
		input.RevisionID != 0 || input.Confirmed {
		return invalidDocumentRequest("fetch contains edit-only fields")
	}
	switch input.Scope {
	case DocumentFetchScopeFull, DocumentFetchScopeOutline:
		if input.Keyword != "" || input.StartBlockID != "" || input.EndBlockID != "" {
			return invalidDocumentRequest("fetch scope fields do not match")
		}
	case DocumentFetchScopeSection:
		if input.Keyword != "" || input.StartBlockID == "" || input.EndBlockID != "" {
			return invalidDocumentRequest("section fetch requires only a start block")
		}
	case DocumentFetchScopeKeyword:
		if strings.TrimSpace(input.Keyword) == "" || input.StartBlockID != "" || input.EndBlockID != "" {
			return invalidDocumentRequest("keyword fetch requires only keyword")
		}
	case DocumentFetchScopeRange:
		if input.Keyword != "" || input.StartBlockID == "" || input.EndBlockID == "" {
			return invalidDocumentRequest("range fetch requires start and end blocks")
		}
	default:
		return invalidDocumentRequest("fetch scope is unsupported")
	}
	return nil
}

func validateAppendInput(input DocumentToolInput) *DocumentToolError {
	if strings.TrimSpace(input.Content) == "" || input.Scope != "" || input.Keyword != "" ||
		input.Pattern != "" || input.BlockID != "" || input.StartBlockID != "" || input.EndBlockID != "" {
		return invalidDocumentRequest("append fields do not match")
	}
	return nil
}

func validateReplaceTextInput(input DocumentToolInput) *DocumentToolError {
	if input.Pattern == "" || input.Content == "" || input.Scope != "" || input.Keyword != "" ||
		input.BlockID != "" || input.StartBlockID != "" || input.EndBlockID != "" {
		return invalidDocumentRequest("replace_text fields do not match")
	}
	return nil
}

func validateInsertAfterInput(input DocumentToolInput) *DocumentToolError {
	if input.BlockID == "" || strings.TrimSpace(input.Content) == "" || input.Scope != "" ||
		input.Keyword != "" || input.Pattern != "" || input.StartBlockID != "" || input.EndBlockID != "" {
		return invalidDocumentRequest("insert_after fields do not match")
	}
	return nil
}

func validateReplaceBlockInput(input DocumentToolInput) *DocumentToolError {
	hasBlock := input.BlockID != ""
	hasRange := input.StartBlockID != "" && input.EndBlockID != ""
	if hasBlock == hasRange || strings.TrimSpace(input.Content) == "" || input.Scope != "" ||
		input.Keyword != "" || input.Pattern != "" ||
		(input.StartBlockID == "") != (input.EndBlockID == "") {
		return invalidDocumentRequest("replace_block fields do not match")
	}
	if hasRange && !input.Confirmed {
		return invalidDocumentRequest("confirmed is required for a block range")
	}
	return nil
}

func validateDeleteBlockInput(input DocumentToolInput) *DocumentToolError {
	hasBlock := input.BlockID != ""
	hasRange := input.StartBlockID != "" && input.EndBlockID != ""
	if hasBlock == hasRange || !input.Confirmed || input.Scope != "" || input.Keyword != "" ||
		input.Pattern != "" || input.Content != "" || (input.StartBlockID == "") != (input.EndBlockID == "") {
		return invalidDocumentRequest("delete_block fields do not match or confirmation is missing")
	}
	return nil
}

func invalidDocumentRequest(message string) *DocumentToolError {
	return &DocumentToolError{Code: DocumentErrorInvalidRequest, Message: message}
}
