package kagentbroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const (
	uninitialized = "Uninitialized"
	createIntent  = "CreateIntent"
	bound         = "Bound"
)

type ownershipRecord struct {
	Version int    `json:"version"`
	Policy  string `json:"policy"`
	State   string `json:"state"`
	Digest  string `json:"digest"`
	UID     string `json:"uid"`
}

// OwnershipJournal must live in an operator-owned private directory, retained
// without replacement or rollback. Close only after every request has stopped.
type OwnershipJournal struct {
	mu     sync.Mutex
	gate   chan struct{}
	dir    *os.File
	lock   *os.File
	path   string
	record ownershipRecord
	failed bool
	// Test fault boundary; production always uses durableReplace.
	persist func(ownershipRecord) error
	fault   func(string) error
}

func fingerprint(p Policy) string      { b, _ := json.Marshal(p); return fingerprintBytes(b) }
func fingerprintBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// OpenOwnershipJournal initializes only an explicitly requested, absent directory.
// A missing record/lock in an existing directory is never a first start.
func OpenOwnershipJournal(path string, p Policy, initialize bool) (*OwnershipJournal, error) {
	if !p.valid() || path == "" {
		return nil, errDenied
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, errDenied
	}
	// Reject symlinks in every existing component, including ancestors.
	for cur := path; ; cur = filepath.Dir(cur) {
		info, e := os.Lstat(cur)
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, errDenied
		}
		if e != nil && !(initialize && cur == path && os.IsNotExist(e)) {
			return nil, errDenied
		}
		if cur == filepath.Dir(cur) {
			break
		}
	}
	if initialize {
		if p.ExpectedUID != "" || os.Mkdir(path, 0700) != nil {
			return nil, errDenied
		}
		parent, e := os.Open(filepath.Dir(path))
		if e != nil {
			return nil, errDenied
		}
		e = parent.Sync()
		parent.Close()
		if e != nil {
			return nil, errDenied
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, errDenied
	}
	dir, err := os.Open(path)
	if err != nil {
		return nil, errDenied
	}
	flags := os.O_RDWR
	if initialize {
		flags |= os.O_CREATE | os.O_EXCL
	}
	lock, err := privateFile(filepath.Join(path, "lock"), flags)
	if err != nil {
		dir.Close()
		return nil, errDenied
	}
	j := &OwnershipJournal{dir: dir, lock: lock, path: path, gate: make(chan struct{}, 1)}
	fail := func() (*OwnershipJournal, error) { j.Close(); return nil, errDenied }
	if lockJournal(lock) != nil {
		return fail()
	}
	j.persist = j.durableReplace
	if initialize {
		if lock.Sync() != nil {
			return fail()
		}
		if j.save(ownershipRecord{Version: 1, Policy: fingerprint(p), State: uninitialized}) != nil {
			return fail()
		}
	} else {
		f, e := privateFile(filepath.Join(path, "record"), os.O_RDONLY)
		if e != nil {
			return fail()
		}
		raw, e := boundedRead(f, 4096)
		f.Close()
		if e != nil {
			return fail()
		}
		m, e := parseObject(raw)
		if e != nil || !keys(m, []string{"version", "policy", "state", "digest", "uid"}) {
			return fail()
		}
		for _, key := range []string{"policy", "state", "digest", "uid"} {
			if _, ok := m[key].(string); !ok {
				return fail()
			}
		}
		if json.Unmarshal(raw, &j.record) != nil {
			return fail()
		}
	}
	r := j.record
	if r.Version != 1 || r.Policy != fingerprint(p) {
		return fail()
	}
	switch r.State {
	case uninitialized:
		if r.Digest != "" || r.UID != "" {
			return fail()
		}
	case createIntent:
		if !digestValid(r.Digest) || r.UID != "" {
			return fail()
		}
	case bound:
		if !digestValid(r.Digest) || !tokenSafe(r.UID) || len(r.UID) > 256 {
			return fail()
		}
	default:
		return fail()
	}
	return j, nil
}
func digestValid(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func privateFile(path string, flags int) (*os.File, error) {
	info, e := os.Lstat(path)
	if e == nil && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600) {
		return nil, errDenied
	}
	f, e := openOwnershipFile(path, flags)
	if e != nil {
		return nil, errDenied
	}
	actual, e := f.Stat()
	linked, le := os.Lstat(path)
	if e != nil || le != nil || !actual.Mode().IsRegular() || actual.Mode().Perm() != 0600 || !os.SameFile(actual, linked) || !singleLink(actual) {
		f.Close()
		return nil, errDenied
	}
	return f, nil
}
func (j *OwnershipJournal) durableReplace(r ownershipRecord) error {
	step := func(name string) error {
		if j.fault != nil {
			return j.fault(name)
		}
		return nil
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return errDenied
	}
	f, err := os.CreateTemp(j.path, ".record-")
	if err != nil {
		return errDenied
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = step("write"); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = step("file-sync")
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errDenied
	}
	if step("rename") != nil || os.Rename(tmp, filepath.Join(j.path, "record")) != nil || step("directory-sync") != nil || j.dir.Sync() != nil {
		return errDenied
	}
	return nil
}

// save poisons this process even when a rename succeeded but its fsync failed.
func (j *OwnershipJournal) save(r ownershipRecord) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failed || j.persist(r) != nil {
		j.failed = true
		return errDenied
	}
	j.record = r
	return nil
}
func (j *OwnershipJournal) snapshot() (ownershipRecord, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.record, !j.failed
}
func (j *OwnershipJournal) acquire(ctx context.Context) bool {
	select {
	case j.gate <- struct{}{}:
		if ctx.Err() == nil {
			return true
		}
		<-j.gate
		return false
	case <-ctx.Done():
		return false
	}
}
func (j *OwnershipJournal) Close() error {
	j.mu.Lock()
	j.failed = true
	j.mu.Unlock()
	e := j.lock.Close()
	j.dir.Close()
	return e
}
