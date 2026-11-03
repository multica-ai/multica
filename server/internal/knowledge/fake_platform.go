package knowledge

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// The Fake's platform side: WeKnora logins, tenant members, chat and rerank
// models, knowledge-base settings and data sources. It keeps the behaviour the
// service relies on: a login's password is set only when the login is created,
// a model test fails for a model a test has marked, and a data source is listed
// only under the knowledge base it feeds.

// Operation names for the platform calls, for FailNext, FailAlways and Calls.
const (
	OpEnsureUser          = "EnsureUser"
	OpAddTenantMember     = "AddTenantMember"
	OpEnsureModelOfType   = "EnsureModel"
	OpTestModel           = "TestModel"
	OpUpdateKnowledgeBase = "UpdateKnowledgeBase"
	OpListDataSources     = "ListDataSources"
)

// fakePlatform is the state the platform calls share. It is created on first
// use, under f.mu.
type fakePlatform struct {
	users        map[string]*fakeUser // by email
	members      map[string]map[string]string
	modelTypes   map[string]ModelType // model id -> type
	modelFails   map[string]error     // model name -> test failure
	dataSources  map[string]DataSource
	passwordSeen map[string]string // email -> password given at creation
}

type fakeUser struct {
	User
	username string
}

// plat returns the platform state, creating it on first use. The caller must
// hold f.mu.
func (f *Fake) plat() *fakePlatform {
	if f.platform == nil {
		f.platform = &fakePlatform{
			users:        map[string]*fakeUser{},
			members:      map[string]map[string]string{},
			modelTypes:   map[string]ModelType{},
			modelFails:   map[string]error{},
			dataSources:  map[string]DataSource{},
			passwordSeen: map[string]string{},
		}
	}
	return f.platform
}

// FailModelTest makes every connection test of the named model fail with err,
// which is how a test shows one model of several being broken.
func (f *Fake) FailModelTest(modelName string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plat().modelFails[modelName] = err
}

// UserPassword returns the password a login was created with, or "" when no
// login with that email exists or it was created without one. Tests use it to
// check what the service sent.
func (f *Fake) UserPassword(email string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.plat().passwordSeen[email]
}

// Members returns the emails and roles in a tenant, sorted by email.
func (f *Fake) Members(tenantID string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for email, role := range f.plat().members[tenantID] {
		out[email] = role
	}
	return out
}

// AddDataSource registers a sync connection under a knowledge base of a tenant,
// as an operator would in WeKnora. The returned id is the connection's.
func (f *Fake) AddDataSource(tenantID, kbID string, ds DataSource) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	ds.ID = f.nextID("ds")
	ds.KnowledgeBaseID = kbID
	f.plat().dataSources[tenantID+"/"+ds.ID] = ds
	return ds.ID
}

// EnsureUser implements Knowledge. An existing login is returned with no
// password change, as WeKnora does.
func (f *Fake) EnsureUser(_ context.Context, spec UserSpec) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter(OpEnsureUser); err != nil {
		return User{}, err
	}
	if spec.Username == "" || spec.Email == "" {
		return User{}, fmt.Errorf("knowledge: user needs a username and an email: %w", ErrInvalid)
	}
	p := f.plat()
	if u, ok := p.users[spec.Email]; ok {
		return u.User, nil
	}
	u := &fakeUser{User: User{ID: f.nextID("user"), Email: spec.Email}, username: spec.Username}
	p.users[spec.Email] = u
	p.passwordSeen[spec.Email] = spec.Password
	return u.User, nil
}

// AddTenantMember implements Knowledge. The login must exist and the tenant must
// exist; a login already in the tenant is left as it is.
func (f *Fake) AddTenantMember(_ context.Context, tenantID, email, role string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter(OpAddTenantMember); err != nil {
		return err
	}
	if _, ok := f.tenants[tenantID]; !ok {
		return fmt.Errorf("knowledge: tenant %s: %w", tenantID, ErrNotFound)
	}
	p := f.plat()
	if _, ok := p.users[email]; !ok {
		return fmt.Errorf("knowledge: user %s: %w", email, ErrNotFound)
	}
	if p.members[tenantID] == nil {
		p.members[tenantID] = map[string]string{}
	}
	if _, ok := p.members[tenantID][email]; !ok {
		p.members[tenantID][email] = role
	}
	return nil
}

// EnsureModel implements Workspace. The model is found by name and type, or
// created active.
func (w *fakeWorkspace) EnsureModel(_ context.Context, spec ModelSpec) (Model, error) {
	t, err := w.begin(OpEnsureModelOfType)
	if err != nil {
		return Model{}, err
	}
	defer w.f.mu.Unlock()
	if spec.Name == "" || spec.Type == "" || spec.BaseURL == "" {
		return Model{}, fmt.Errorf("knowledge: model needs a name, a type and an endpoint: %w", ErrInvalid)
	}
	p := w.f.plat()
	for _, m := range t.models {
		if m.Name == spec.Name && p.modelTypes[m.ID] == spec.Type {
			return *m, nil
		}
	}
	m := &Model{ID: w.f.nextID("model"), Name: spec.Name, Status: "active"}
	t.models[m.ID] = m
	p.modelTypes[m.ID] = spec.Type
	return *m, nil
}

// TestModel implements Workspace. A model whose name has a failure set by
// FailModelTest answers with that failure as an *APIError.
func (w *fakeWorkspace) TestModel(_ context.Context, modelID string, probe ModelProbe) error {
	t, err := w.begin(OpTestModel)
	if err != nil {
		return err
	}
	defer w.f.mu.Unlock()
	m, ok := t.models[modelID]
	if !ok {
		return fmt.Errorf("knowledge: model %s: %w", modelID, ErrNotFound)
	}
	if strings.TrimSpace(probe.Input) == "" {
		return fmt.Errorf("knowledge: test model needs an input: %w", ErrInvalid)
	}
	if failure, ok := w.f.plat().modelFails[m.Name]; ok {
		return &APIError{Status: 500, Op: "test model", Message: failure.Error(), class: nil}
	}
	return nil
}

// UpdateKnowledgeBase implements Workspace. Nil fields keep their values.
func (w *fakeWorkspace) UpdateKnowledgeBase(_ context.Context, kbID string, patch KnowledgeBasePatch) (KnowledgeBase, error) {
	t, err := w.begin(OpUpdateKnowledgeBase)
	if err != nil {
		return KnowledgeBase{}, err
	}
	defer w.f.mu.Unlock()
	kb, ok := t.kbs[kbID]
	if !ok {
		return KnowledgeBase{}, fmt.Errorf("knowledge: knowledge base %s: %w", kbID, ErrNotFound)
	}
	if patch.SummaryModelID != nil {
		if *patch.SummaryModelID != "" {
			if _, ok := t.models[*patch.SummaryModelID]; !ok {
				return KnowledgeBase{}, fmt.Errorf("knowledge: model %s: %w", *patch.SummaryModelID, ErrNotFound)
			}
		}
		kb.SummaryModelID = *patch.SummaryModelID
	}
	if patch.WikiEnabled != nil {
		kb.WikiEnabled = *patch.WikiEnabled
	}
	return *kb, nil
}

// ListDataSources implements Workspace. Connections are listed under the base
// they feed, in id order.
func (w *fakeWorkspace) ListDataSources(_ context.Context, kbID string) ([]DataSource, error) {
	t, err := w.begin(OpListDataSources)
	if err != nil {
		return nil, err
	}
	defer w.f.mu.Unlock()
	if _, ok := t.kbs[kbID]; !ok {
		return nil, fmt.Errorf("knowledge: knowledge base %s: %w", kbID, ErrNotFound)
	}
	var out []DataSource
	for key, ds := range w.f.plat().dataSources {
		if ds.KnowledgeBaseID == kbID && strings.HasPrefix(key, t.ID+"/") {
			out = append(out, ds)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if out == nil {
		out = []DataSource{}
	}
	return out, nil
}
