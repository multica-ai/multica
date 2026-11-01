package knowledge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Fake is an in-memory Knowledge for tests in other packages. It keeps
// WeKnora's observable behaviour that callers depend on: a draft is stored but
// never searchable, a duplicate URL or file is refused with the existing
// document, a token is shown once, and each workspace key only sees its own
// tenant. It is safe for concurrent use.
//
// Failures are injected by operation name (see the Op constants) with
// FailNext and FailAlways; SetUnavailable makes every call return
// ErrUnavailable, as a down sidecar would. Calls counts how often an
// operation ran, which is how idempotency is asserted.
type Fake struct {
	// PublicURL is the base MCPURL builds on. Defaults to "http://knowledge.test".
	PublicURL string

	mu          sync.Mutex
	seq         int
	unavailable bool
	failNext    map[string][]error
	failAlways  map[string]error
	calls       map[string]int

	tenants map[string]*fakeTenant // by id
	keys    map[string]string      // token -> tenant id
}

type fakeTenant struct {
	Tenant
	models    map[string]*Model
	kbs       map[string]*KnowledgeBase
	kbOrder   []string
	docs      map[string]*fakeDoc
	endpoints map[string]*MCPEndpoint
}

type fakeDoc struct {
	Document
	content string
}

// Operation names for FailNext, FailAlways and Calls.
const (
	OpPing                = "Ping"
	OpEnsureTenant        = "EnsureTenant"
	OpCreateWorkspaceKey  = "CreateWorkspaceKey"
	OpDeleteTenant        = "DeleteTenant"
	OpEnsureModel         = "EnsureEmbeddingModel"
	OpCreateKB            = "CreateKnowledgeBase"
	OpListKB              = "ListKnowledgeBases"
	OpDeleteKB            = "DeleteKnowledgeBase"
	OpCreateManual        = "CreateManual"
	OpCreateURL           = "CreateURL"
	OpCreateFile          = "CreateFile"
	OpPublish             = "Publish"
	OpDelete              = "Delete"
	OpListDocuments       = "ListDocuments"
	OpRead                = "Read"
	OpSearch              = "Search"
	OpListMCPEndpoints    = "ListMCPEndpoints"
	OpCreateMCPEndpoint   = "CreateMCPEndpoint"
	OpRotateMCPToken      = "RotateMCPEndpointToken"
	OpSetMCPEndpointBases = "SetMCPEndpointKnowledgeBases"
)

var _ Knowledge = (*Fake)(nil)

// NewFake returns an empty fake.
func NewFake() *Fake {
	return &Fake{
		PublicURL:  "http://knowledge.test",
		failNext:   map[string][]error{},
		failAlways: map[string]error{},
		calls:      map[string]int{},
		tenants:    map[string]*fakeTenant{},
		keys:       map[string]string{},
	}
}

// SetUnavailable makes every call return ErrUnavailable until cleared.
func (f *Fake) SetUnavailable(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unavailable = down
}

// FailNext makes the next call to op return err, once.
func (f *Fake) FailNext(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext[op] = append(f.failNext[op], err)
}

// FailAlways makes every call to op return err until ClearFailures.
func (f *Fake) FailAlways(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAlways[op] = err
}

// ClearFailures removes injected failures, including SetUnavailable.
func (f *Fake) ClearFailures() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unavailable = false
	f.failNext = map[string][]error{}
	f.failAlways = map[string]error{}
}

// Calls returns how many times op has been called, failed calls included.
func (f *Fake) Calls(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[op]
}

// Tenants returns the tenants created so far, ordered by id.
func (f *Fake) Tenants() []Tenant {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Tenant, 0, len(f.tenants))
	for _, t := range f.tenants {
		out = append(out, t.Tenant)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// enter records the call and returns the injected error for it, if any. The
// caller must hold f.mu.
func (f *Fake) enter(op string) error {
	f.calls[op]++
	if f.unavailable {
		return fmt.Errorf("knowledge: %s: %w", op, ErrUnavailable)
	}
	if q := f.failNext[op]; len(q) > 0 {
		f.failNext[op] = q[1:]
		return q[0]
	}
	return f.failAlways[op]
}

func (f *Fake) nextID(prefix string) string {
	f.seq++
	return prefix + "-" + strconv.Itoa(f.seq)
}

func (f *Fake) now() time.Time { return time.Unix(1_700_000_000+int64(f.seq), 0).UTC() }

// Ping implements Knowledge.
func (f *Fake) Ping(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enter(OpPing)
}

// EnsureTenant implements Knowledge.
func (f *Fake) EnsureTenant(_ context.Context, name, description string) (Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter(OpEnsureTenant); err != nil {
		return Tenant{}, err
	}
	for _, t := range f.tenants {
		if t.Name == name {
			return t.Tenant, nil
		}
	}
	t := &fakeTenant{
		Tenant:    Tenant{ID: strconv.Itoa(10000 + len(f.tenants)), Name: name, Description: description},
		models:    map[string]*Model{},
		kbs:       map[string]*KnowledgeBase{},
		docs:      map[string]*fakeDoc{},
		endpoints: map[string]*MCPEndpoint{},
	}
	f.tenants[t.ID] = t
	return t.Tenant, nil
}

// CreateWorkspaceKey implements Knowledge. Each call mints a new token.
func (f *Fake) CreateWorkspaceKey(_ context.Context, tenantID, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter(OpCreateWorkspaceKey); err != nil {
		return "", err
	}
	if _, ok := f.tenants[tenantID]; !ok {
		return "", fmt.Errorf("knowledge: tenant %s: %w", tenantID, ErrNotFound)
	}
	f.seq++
	token := "sk-fake-" + tenantID + "-" + strconv.Itoa(f.seq)
	f.keys[token] = tenantID
	return token, nil
}

// DeleteTenant implements Knowledge. Its keys stop working.
func (f *Fake) DeleteTenant(_ context.Context, tenantID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter(OpDeleteTenant); err != nil {
		return err
	}
	if _, ok := f.tenants[tenantID]; !ok {
		return fmt.Errorf("knowledge: tenant %s: %w", tenantID, ErrNotFound)
	}
	delete(f.tenants, tenantID)
	for tok, id := range f.keys {
		if id == tenantID {
			delete(f.keys, tok)
		}
	}
	return nil
}

// MCPURL implements Knowledge.
func (f *Fake) MCPURL(endpointID string) string {
	return strings.TrimRight(f.PublicURL, "/") + "/mcp/" + endpointID
}

// Workspace implements Knowledge.
func (f *Fake) Workspace(apiKey string) Workspace { return &fakeWorkspace{f: f, key: apiKey} }

// ---- workspace -------------------------------------------------------------

type fakeWorkspace struct {
	f   *Fake
	key string
}

// begin takes the lock, records the call, authenticates the key and returns
// the tenant. The caller must release f.mu.
func (w *fakeWorkspace) begin(op string) (*fakeTenant, error) {
	w.f.mu.Lock()
	if err := w.f.enter(op); err != nil {
		w.f.mu.Unlock()
		return nil, err
	}
	id, ok := w.f.keys[w.key]
	if !ok {
		w.f.mu.Unlock()
		return nil, fmt.Errorf("knowledge: %s: %w", op, ErrAuth)
	}
	t, ok := w.f.tenants[id]
	if !ok {
		w.f.mu.Unlock()
		return nil, fmt.Errorf("knowledge: %s: %w", op, ErrAuth)
	}
	return t, nil
}

func (w *fakeWorkspace) EnsureEmbeddingModel(_ context.Context, spec EmbeddingModelSpec) (Model, error) {
	t, err := w.begin(OpEnsureModel)
	if err != nil {
		return Model{}, err
	}
	defer w.f.mu.Unlock()
	if spec.Name == "" || spec.Dimension <= 0 {
		return Model{}, fmt.Errorf("knowledge: embedding model needs a name and a dimension: %w", ErrInvalid)
	}
	for _, m := range t.models {
		if m.Name == spec.Name {
			return *m, nil
		}
	}
	m := &Model{ID: w.f.nextID("model"), Name: spec.Name, Status: "active"}
	t.models[m.ID] = m
	return *m, nil
}

func (w *fakeWorkspace) CreateKnowledgeBase(_ context.Context, spec KnowledgeBaseSpec) (KnowledgeBase, error) {
	t, err := w.begin(OpCreateKB)
	if err != nil {
		return KnowledgeBase{}, err
	}
	defer w.f.mu.Unlock()
	if strings.TrimSpace(spec.Name) == "" {
		return KnowledgeBase{}, fmt.Errorf("knowledge: name cannot be empty: %w", ErrInvalid)
	}
	if _, ok := t.models[spec.EmbeddingModelID]; !ok {
		return KnowledgeBase{}, fmt.Errorf("knowledge: embedding model %q: %w", spec.EmbeddingModelID, ErrNotFound)
	}
	kb := &KnowledgeBase{ID: w.f.nextID("kb"), Name: spec.Name, Description: spec.Description, EmbeddingModelID: spec.EmbeddingModelID}
	t.kbs[kb.ID] = kb
	t.kbOrder = append(t.kbOrder, kb.ID)
	return *kb, nil
}

func (w *fakeWorkspace) ListKnowledgeBases(context.Context) ([]KnowledgeBase, error) {
	t, err := w.begin(OpListKB)
	if err != nil {
		return nil, err
	}
	defer w.f.mu.Unlock()
	out := make([]KnowledgeBase, 0, len(t.kbOrder))
	for _, id := range t.kbOrder {
		kb := *t.kbs[id]
		kb.DocumentCount = t.countDocs(id)
		out = append(out, kb)
	}
	return out, nil
}

func (t *fakeTenant) countDocs(kbID string) int {
	n := 0
	for _, d := range t.docs {
		if d.KnowledgeBaseID == kbID {
			n++
		}
	}
	return n
}

func (w *fakeWorkspace) DeleteKnowledgeBase(_ context.Context, kbID string) error {
	t, err := w.begin(OpDeleteKB)
	if err != nil {
		return err
	}
	defer w.f.mu.Unlock()
	if _, ok := t.kbs[kbID]; !ok {
		return fmt.Errorf("knowledge: knowledge base %s: %w", kbID, ErrNotFound)
	}
	delete(t.kbs, kbID)
	for i, id := range t.kbOrder {
		if id == kbID {
			t.kbOrder = append(t.kbOrder[:i], t.kbOrder[i+1:]...)
			break
		}
	}
	for id, d := range t.docs {
		if d.KnowledgeBaseID == kbID {
			delete(t.docs, id)
		}
	}
	return nil
}

func (w *fakeWorkspace) addDoc(t *fakeTenant, kbID string, d Document, content string) (Document, error) {
	if _, ok := t.kbs[kbID]; !ok {
		return Document{}, fmt.Errorf("knowledge: knowledge base %s: %w", kbID, ErrNotFound)
	}
	d.ID = w.f.nextID("doc")
	d.KnowledgeBaseID = kbID
	d.CreatedAt = w.f.now()
	d.UpdatedAt = d.CreatedAt
	t.docs[d.ID] = &fakeDoc{Document: d, content: content}
	return d, nil
}

func (w *fakeWorkspace) CreateManual(_ context.Context, kbID string, doc ManualDocument) (Document, error) {
	t, err := w.begin(OpCreateManual)
	if err != nil {
		return Document{}, err
	}
	defer w.f.mu.Unlock()
	if strings.TrimSpace(doc.Content) == "" {
		return Document{}, fmt.Errorf("knowledge: manual document needs content: %w", ErrInvalid)
	}
	status := ParseDraft
	if doc.Publish {
		status = ParseCompleted
	}
	return w.addDoc(t, kbID, Document{Title: doc.Title, FileName: doc.Title + ".md", Type: "manual", ParseStatus: status}, doc.Content)
}

func (w *fakeWorkspace) findDuplicate(t *fakeTenant, kbID string, match func(*fakeDoc) bool) (Document, bool) {
	ids := make([]string, 0, len(t.docs))
	for id := range t.docs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if d := t.docs[id]; d.KnowledgeBaseID == kbID && match(d) {
			return d.Document, true
		}
	}
	return Document{}, false
}

func (w *fakeWorkspace) CreateURL(_ context.Context, kbID string, doc URLDocument) (Document, error) {
	t, err := w.begin(OpCreateURL)
	if err != nil {
		return Document{}, err
	}
	defer w.f.mu.Unlock()
	if existing, ok := w.findDuplicate(t, kbID, func(d *fakeDoc) bool { return d.Type == "url" && d.Source == doc.URL }); ok {
		return Document{}, &DuplicateError{APIError: APIError{Status: 409, Code: "duplicate_url", Op: "import url"}, Existing: existing}
	}
	return w.addDoc(t, kbID, Document{Title: doc.Title, Type: "url", Source: doc.URL, ParseStatus: ParseCompleted}, "Imported from "+doc.URL)
}

func (w *fakeWorkspace) CreateFile(_ context.Context, kbID string, doc FileDocument) (Document, error) {
	t, err := w.begin(OpCreateFile)
	if err != nil {
		return Document{}, err
	}
	defer w.f.mu.Unlock()
	if doc.Name == "" || doc.Content == nil {
		return Document{}, fmt.Errorf("knowledge: file upload needs a name and content: %w", ErrInvalid)
	}
	body, err := io.ReadAll(doc.Content)
	if err != nil {
		return Document{}, err
	}
	folder := strings.Trim(doc.FolderPath, "/")
	if existing, ok := w.findDuplicate(t, kbID, func(d *fakeDoc) bool {
		return d.Type == "file" && d.FileName == doc.Name && d.FolderPath == folder
	}); ok {
		return Document{}, &DuplicateError{APIError: APIError{Status: 409, Code: "duplicate_file", Op: "upload file"}, Existing: existing}
	}
	return w.addDoc(t, kbID, Document{Title: doc.Name, FileName: doc.Name, FolderPath: folder, Type: "file", ParseStatus: ParseCompleted}, string(bytes.TrimSpace(body)))
}

func (w *fakeWorkspace) Publish(_ context.Context, docID string) (Document, error) {
	t, err := w.begin(OpPublish)
	if err != nil {
		return Document{}, err
	}
	defer w.f.mu.Unlock()
	d, ok := t.docs[docID]
	if !ok {
		return Document{}, fmt.Errorf("knowledge: document %s: %w", docID, ErrNotFound)
	}
	if d.Type != "manual" {
		return Document{}, fmt.Errorf("knowledge: document %s is a %s: %w", docID, d.Type, ErrNotManual)
	}
	if d.ParseStatus == ParseDraft {
		d.ParseStatus = ParseCompleted
		d.UpdatedAt = w.f.now()
	}
	return d.Document, nil
}

func (w *fakeWorkspace) Delete(_ context.Context, docID string) error {
	t, err := w.begin(OpDelete)
	if err != nil {
		return err
	}
	defer w.f.mu.Unlock()
	if _, ok := t.docs[docID]; !ok {
		return fmt.Errorf("knowledge: document %s: %w", docID, ErrNotFound)
	}
	delete(t.docs, docID)
	return nil
}

func (w *fakeWorkspace) ListDocuments(_ context.Context, kbID string, opts ListOptions) (DocumentPage, error) {
	t, err := w.begin(OpListDocuments)
	if err != nil {
		return DocumentPage{}, err
	}
	defer w.f.mu.Unlock()
	if _, ok := t.kbs[kbID]; !ok {
		return DocumentPage{}, fmt.Errorf("knowledge: knowledge base %s: %w", kbID, ErrNotFound)
	}
	var all []Document
	for _, d := range t.docs {
		if d.KnowledgeBaseID != kbID {
			continue
		}
		if opts.Keyword != "" && !strings.Contains(strings.ToLower(d.Title+" "+d.FileName), strings.ToLower(opts.Keyword)) {
			continue
		}
		all = append(all, d.Document)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	page, size := opts.Page, opts.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	start := (page - 1) * size
	if start > len(all) {
		start = len(all)
	}
	end := start + size
	if end > len(all) {
		end = len(all)
	}
	return DocumentPage{Items: all[start:end], Total: len(all), Page: page, PageSize: size}, nil
}

func (w *fakeWorkspace) Read(_ context.Context, docID string) (DocumentContent, error) {
	t, err := w.begin(OpRead)
	if err != nil {
		return DocumentContent{}, err
	}
	defer w.f.mu.Unlock()
	d, ok := t.docs[docID]
	if !ok {
		return DocumentContent{}, fmt.Errorf("knowledge: document %s: %w", docID, ErrNotFound)
	}
	return DocumentContent{Document: d.Document, Content: d.content}, nil
}

// Search matches when every word of the query appears in the document's text
// or title. Drafts never match. Scores are 1.0, and the order is by id, so
// tests need not depend on ranking.
func (w *fakeWorkspace) Search(_ context.Context, req SearchRequest) ([]SearchHit, error) {
	t, err := w.begin(OpSearch)
	if err != nil {
		return nil, err
	}
	defer w.f.mu.Unlock()
	if strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("knowledge: search needs a query: %w", ErrInvalid)
	}
	inScope := map[string]bool{}
	for _, id := range req.KnowledgeBaseIDs {
		inScope[id] = true
	}
	words := strings.Fields(strings.ToLower(req.Query))
	ids := make([]string, 0, len(t.docs))
	for id := range t.docs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var hits []SearchHit
	for _, id := range ids {
		d := t.docs[id]
		if d.ParseStatus != ParseCompleted || (len(inScope) > 0 && !inScope[d.KnowledgeBaseID]) {
			continue
		}
		text := strings.ToLower(d.Title + " " + d.content)
		match := true
		for _, word := range words {
			if !strings.Contains(text, word) {
				match = false
				break
			}
		}
		if !match || 1.0 < req.MinScore {
			continue
		}
		hits = append(hits, SearchHit{
			ChunkID: "chunk-" + d.ID, DocumentID: d.ID, KnowledgeBaseID: d.KnowledgeBaseID,
			Title: d.Title, FileName: d.FileName, Source: d.Source, Content: d.content, Score: 1.0,
		})
		if req.Limit > 0 && len(hits) >= req.Limit {
			break
		}
	}
	return hits, nil
}

func (w *fakeWorkspace) ListMCPEndpoints(context.Context) ([]MCPEndpoint, error) {
	t, err := w.begin(OpListMCPEndpoints)
	if err != nil {
		return nil, err
	}
	defer w.f.mu.Unlock()
	out := make([]MCPEndpoint, 0, len(t.endpoints))
	for _, e := range t.endpoints {
		cp := *e
		cp.Token = "" // WeKnora never returns a token after creation
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (w *fakeWorkspace) CreateMCPEndpoint(_ context.Context, spec MCPEndpointSpec) (MCPEndpoint, error) {
	t, err := w.begin(OpCreateMCPEndpoint)
	if err != nil {
		return MCPEndpoint{}, err
	}
	defer w.f.mu.Unlock()
	if len(spec.KnowledgeBaseIDs) == 0 || len(spec.Tools) == 0 {
		return MCPEndpoint{}, fmt.Errorf("knowledge: mcp endpoint needs knowledge bases and tools: %w", ErrInvalid)
	}
	for _, id := range spec.KnowledgeBaseIDs {
		if _, ok := t.kbs[id]; !ok {
			return MCPEndpoint{}, fmt.Errorf("knowledge: knowledge base %s: %w", id, ErrNotFound)
		}
	}
	e := &MCPEndpoint{
		ID: w.f.nextID("mcp"), Name: spec.Name, Enabled: true,
		KnowledgeBaseIDs: append([]string(nil), spec.KnowledgeBaseIDs...),
		Tools:            append([]string(nil), spec.Tools...),
	}
	e.Path = "/mcp/" + e.ID
	t.endpoints[e.ID] = e
	out := *e
	out.Token = w.f.nextID("mcp_token")
	return out, nil
}

func (w *fakeWorkspace) RotateMCPEndpointToken(_ context.Context, endpointID string) (MCPEndpoint, error) {
	t, err := w.begin(OpRotateMCPToken)
	if err != nil {
		return MCPEndpoint{}, err
	}
	defer w.f.mu.Unlock()
	e, ok := t.endpoints[endpointID]
	if !ok {
		return MCPEndpoint{}, fmt.Errorf("knowledge: mcp endpoint %s: %w", endpointID, ErrNotFound)
	}
	out := *e
	out.Token = w.f.nextID("mcp_token")
	return out, nil
}

func (w *fakeWorkspace) SetMCPEndpointKnowledgeBases(_ context.Context, endpointID string, kbIDs []string) (MCPEndpoint, error) {
	t, err := w.begin(OpSetMCPEndpointBases)
	if err != nil {
		return MCPEndpoint{}, err
	}
	defer w.f.mu.Unlock()
	if len(kbIDs) == 0 {
		return MCPEndpoint{}, fmt.Errorf("knowledge: an empty list would expose every knowledge base: %w", ErrInvalid)
	}
	e, ok := t.endpoints[endpointID]
	if !ok {
		return MCPEndpoint{}, fmt.Errorf("knowledge: mcp endpoint %s: %w", endpointID, ErrNotFound)
	}
	for _, id := range kbIDs {
		if _, ok := t.kbs[id]; !ok {
			return MCPEndpoint{}, fmt.Errorf("knowledge: knowledge base %s: %w", id, ErrNotFound)
		}
	}
	e.KnowledgeBaseIDs = append([]string(nil), kbIDs...)
	return *e, nil
}
