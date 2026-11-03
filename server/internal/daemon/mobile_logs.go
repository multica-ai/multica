package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/daemon/mobilediag"
	"github.com/multica-ai/multica/server/internal/daemon/p2p"
	"github.com/multica-ai/multica/server/internal/localconversation/service"
)

const (
	mobileExportsDir      = "mobile-exports"
	maxMobileExports      = 20
	mobileExportRetention = 7 * 24 * time.Hour
)

var mobileExportName = regexp.MustCompile(`^multica-mobile-logs-[\dTZ-]+-[\da-f-]+\.jsonl$`)

var errDeviceLinkNotRunning = errors.New("The device link is not running on this computer")

// mobileLogs reads phones through this device's link into its private diagnostics.
type mobileLogs struct {
	link func() *p2p.Service
	dir  string
	now  func() time.Time
}

func (d *Daemon) newMobileLogs() *mobileLogs {
	return &mobileLogs{link: d.P2P, dir: filepath.Join(d.cfg.ProfileDir, "local-conversations", "diagnostics", mobileExportsDir), now: time.Now}
}

func (m *mobileLogs) Phones() ([]service.MobilePhone, error) {
	svc := m.link()
	if svc == nil {
		return nil, errDeviceLinkNotRunning
	}
	phones := []service.MobilePhone{}
	for _, phone := range mobilediag.Phones(svc) {
		phones = append(phones, service.MobilePhone{ID: phone.ID, Name: phone.Name, Connected: phone.Connected, Connection: phone.Connection, PeerID: phone.Peer.String()})
	}
	return phones, nil
}

func (m *mobileLogs) Read(ctx context.Context, deviceID string, sinceMinutes int) (any, error) {
	svc := m.link()
	if svc == nil {
		return nil, errDeviceLinkNotRunning
	}
	for _, phone := range mobilediag.Phones(svc) {
		if phone.ID == deviceID {
			return m.read(ctx, svc, phone, sinceMinutes)
		}
	}
	return nil, fmt.Errorf("No phone has device_id %q", deviceID)
}

func (m *mobileLogs) read(ctx context.Context, svc *p2p.Service, phone mobilediag.Phone, sinceMinutes int) (any, error) {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return nil, err
	}
	now := m.now()
	path := filepath.Join(m.dir, fmt.Sprintf("multica-mobile-logs-%s-%s.jsonl", now.UTC().Format("2006-01-02T15-04-05Z"), uuid.NewString()))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	fetched, err := mobilediag.Fetch(ctx, svc, phone, sinceMinutes, file)
	if err != nil || fetched.Bytes == 0 {
		_ = os.Remove(path)
		if err == nil {
			err = errors.New("the phone sent nothing")
		}
		return nil, describeReadFailure(svc, phone, err)
	}
	m.prune(now)
	return map[string]any{
		"path": path, "format": "jsonl", "bytes": fetched.Bytes, "device_id": phone.ID, "device_name": phone.Name,
		"connection": svc.Connection(phone.Peer), "since_minutes": sinceMinutes, "complete": fetched.Complete, "summary": fetched.Summary,
	}, nil
}

func describeReadFailure(svc *p2p.Service, phone mobilediag.Phone, err error) error {
	switch {
	case errors.Is(err, mobilediag.ErrRemoteReadDisabled):
		return errors.New("This phone has turned off reads by computers. The user can allow them in the Multica iOS app under Settings")
	case phone.Connected && !svc.SupportsProtocol(phone.Peer, mobilediag.Protocol):
		return errors.New("This phone's Multica app is too old to share its logs. Ask the user to update it")
	}
	return fmt.Errorf("Could not read this phone's logs: the Multica iOS app must be running on it, and iOS suspends apps soon after they leave the foreground. Ask the user to open the app, then retry (%w)", err)
}

func (m *mobileLogs) prune(now time.Time) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}
	type export struct {
		path     string
		modified time.Time
	}
	exports := []export{}
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && mobileExportName.MatchString(entry.Name()) {
			exports = append(exports, export{filepath.Join(m.dir, entry.Name()), info.ModTime()})
		}
	}
	sort.Slice(exports, func(i, j int) bool { return exports[i].modified.After(exports[j].modified) })
	for i, file := range exports {
		if i >= maxMobileExports || now.Sub(file.modified) > mobileExportRetention {
			_ = os.Remove(file.path)
		}
	}
}
