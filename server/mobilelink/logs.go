package mobilelink

import (
	"encoding/json"
	"path/filepath"
	"sync"

	"github.com/multica-ai/multica/server/internal/daemon/mobilediag"
)

const logsDirName = "logs"

var (
	logStoresMu sync.Mutex
	logStores   = map[string]*mobilediag.Store{}
)

// openStore returns the one log of storagePath, shared by the app and each link it
// starts there.
func openStore(storagePath string) (*mobilediag.Store, error) {
	logStoresMu.Lock()
	defer logStoresMu.Unlock()
	if store := logStores[storagePath]; store != nil {
		return store, nil
	}
	store, err := mobilediag.Open(filepath.Join(storagePath, logsDirName))
	if err != nil {
		return nil, err
	}
	logStores[storagePath] = store
	return store, nil
}

// Logs is the app's handle on the phone's log. It exists before any link, so the
// app can log what happens before one starts or when one fails to.
type Logs struct{ store *mobilediag.Store }

// OpenLogs opens the log under storagePath, the same path given to Start.
func OpenLogs(storagePath string) (*Logs, error) {
	store, err := openStore(storagePath)
	if err != nil {
		return nil, err
	}
	return &Logs{store: store}, nil
}

// AppendLines adds newline-separated JSON records to source.
func (l *Logs) AppendLines(source, lines string) { l.store.Append(source, []byte(lines)) }

// Clear removes every log, for when the user signs out.
func (l *Logs) Clear() { l.store.Clear() }

// RemoteReadAllowed reports whether computers may read this log.
func (l *Logs) RemoteReadAllowed() bool { return l.store.RemoteReadAllowed() }

// SetRemoteReadAllowed records the user's choice.
func (l *Logs) SetRemoteReadAllowed(allowed bool) { l.store.SetRemoteReadAllowed(allowed) }

// SetRuntime records the JSON describing this app and device for each read.
func (l *Logs) SetRuntime(runtime string) { l.store.SetRuntime([]byte(runtime)) }

// ClearExports removes the temporary files ExportFile made, once shared.
func (l *Logs) ClearExports() { l.store.ClearExports() }

// ExportFile writes the last sinceMinutes to a temporary file for the user to
// share, answering its path and summary as JSON.
func (l *Logs) ExportFile(sinceMinutes int64) (string, error) {
	path, summary, err := l.store.ExportFile(int(sinceMinutes))
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]any{"path": path, "summary": summary})
	return string(data), err
}
