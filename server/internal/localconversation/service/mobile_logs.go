package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// MobileLogInstructions describe fetch_mobile_logs to a provider.
const MobileLogInstructions = "When diagnosing the Multica iOS app, or a problem that may involve it, and the user requests its logs, use fetch_mobile_logs in the built-in MCP. It reads the phone's app logs, timing marks and device link logs over the device link into a local JSONL file; the app must be running on the phone. When the current turn came from a phone that phone is read by default, otherwise phones are told apart by device_id, not by name. Read the returned path with a native file tool and correlate with export_developer_logs by requestId or turnId. Unlike export_developer_logs, these logs are not scrubbed and cover every workspace of the account: they may contain addresses, request paths and private details, so review them before sharing and never upload them without the user's request."

const maxMobileLogFetches = 20

// TurnOrigin is the device that asked for a turn, as the request's connection
// proved it. The zero value is this device, or no device.
type TurnOrigin struct {
	PeerID     string
	ClientKind string
}

// MobilePhone is a phone of this account. ID is its identity; names repeat.
type MobilePhone struct {
	ID         string `json:"device_id"`
	Name       string `json:"device_name"`
	Connected  bool   `json:"connected"`
	Connection string `json:"connection,omitempty"`
	PeerID     string `json:"-"`
}

// MobileLogs reads phones' logs over the device link.
type MobileLogs interface {
	Phones() ([]MobilePhone, error)
	// Read stores one phone's logs on this device and describes the file.
	Read(ctx context.Context, deviceID string, sinceMinutes int) (any, error)
}

// SetMobileLogs connects the service to this device's link; without it the tool is absent.
func (s *Service) SetMobileLogs(logs MobileLogs) {
	s.mu.Lock()
	s.mobileLogs = logs
	s.mu.Unlock()
}

func describePhones(phones []MobilePhone) string {
	parts := make([]string, 0, len(phones))
	for _, phone := range phones {
		state := "offline"
		if phone.Connected {
			state = "connected"
		}
		parts = append(parts, fmt.Sprintf("%s (%s, %s)", phone.ID, phone.Name, state))
	}
	return strings.Join(parts, "; ")
}

// choosePhone picks the phone to read. A named phone, or the phone the turn came
// from, is tried even when not connected, because the attempt says why it failed
// and another phone's logs would be wrong. Otherwise a choice is made only when it
// cannot be the wrong phone.
func choosePhone(phones []MobilePhone, deviceID string, origin TurnOrigin) (MobilePhone, error) {
	if deviceID != "" {
		for _, phone := range phones {
			if phone.ID == deviceID {
				return phone, nil
			}
		}
		return MobilePhone{}, fmt.Errorf("No phone has device_id %q. Phones of this account: %s", deviceID, describePhones(phones))
	}
	if origin.ClientKind == timing.ClientMobile {
		for _, phone := range phones {
			if phone.PeerID == origin.PeerID {
				return phone, nil
			}
		}
		return MobilePhone{}, errors.New("The phone this turn came from is no longer on the device list. Ask the user to open the Multica iOS app, or supply device_id")
	}
	if len(phones) == 0 {
		return MobilePhone{}, errors.New("This account has no phone on the device link. Sign in to the Multica iOS app and keep it open")
	}
	var connected []MobilePhone
	for _, phone := range phones {
		if phone.Connected {
			connected = append(connected, phone)
		}
	}
	switch {
	case len(connected) == 1:
		return connected[0], nil
	case len(connected) > 1:
		return MobilePhone{}, fmt.Errorf("Several phones are connected; supply device_id. Phones of this account: %s", describePhones(connected))
	case len(phones) == 1:
		return phones[0], nil
	}
	return MobilePhone{}, fmt.Errorf("No phone is connected. Ask the user to open the Multica iOS app, or supply device_id. Phones of this account: %s", describePhones(phones))
}

// currentOrigin is the device that asked for the turn now running.
func (s *Service) currentOrigin(ctx context.Context, conversationID string, current func() map[string]any) TurnOrigin {
	id, _ := current()["turn_id"].(string)
	if id == "" {
		return TurnOrigin{}
	}
	turn, err := s.store.Turn(ctx, conversationID, id)
	if err != nil {
		return TurnOrigin{}
	}
	return TurnOrigin{PeerID: turn.OriginPeerID, ClientKind: turn.OriginClientKind}
}

// mobileLogTool reads a phone's logs. A read that delivered stays delivered for its
// request_id; a failed one may be retried, since opening the app can fix it.
func (s *Service) mobileLogTool(conversationID string, current func() map[string]any, logs MobileLogs) contextmcp.Tool {
	var mu sync.Mutex
	fetches := map[string]*sideWrite{}
	schema := obj(`"device_id":{"type":"string","maxLength":200,"description":"Which phone to read. Omit to read the phone this turn came from, or the only connected phone; an error lists the choices otherwise."},"since_minutes":{"type":"integer","minimum":1,"maximum":10080,"default":60},"request_id":{"type":"string","minLength":1,"maxLength":160}`, "request_id")
	validate := compileSchema("fetch_mobile_logs", schema)
	return contextmcp.Tool{
		Name:        "fetch_mobile_logs",
		Description: "Read a Multica iOS app's logs over the device link into one private local JSONL file: app console and error logs, timing marks and the phone's device link logs, at most 4 MB with the newest kept. Works with any screen of the app open, including with no conversation open; a phone whose app is closed or suspended cannot answer, and its user can turn these reads off in the app's settings. Defaults to 60 minutes. Returns the path, device, whether the file is complete, and per-source availability and counts; read it with a native file tool. The logs are not scrubbed and cover every workspace of the account: review before sharing, and never upload without the user's request. Reuse request_id on retries.",
		InputSchema: json.RawMessage(schema),
		Call: func(ctx context.Context, args json.RawMessage) (contextmcp.Result, error) {
			if err := validate(args); err != nil {
				return contextmcp.Result{}, err
			}
			var in struct {
				DeviceID     string `json:"device_id"`
				SinceMinutes int    `json:"since_minutes"`
				RequestID    string `json:"request_id"`
			}
			_ = json.Unmarshal(args, &in)
			if in.SinceMinutes == 0 {
				in.SinceMinutes = 60
			}
			fingerprint := fmt.Sprintf("%s\x00%d", in.DeviceID, in.SinceMinutes)
			mu.Lock()
			if prior := fetches[in.RequestID]; prior != nil {
				mu.Unlock()
				if prior.input != fingerprint {
					return contextmcp.Result{}, errors.New("This request_id already belongs to another phone log read")
				}
				<-prior.done
				if prior.err == nil {
					return prior.value, nil
				}
				mu.Lock()
				if fetches[in.RequestID] == prior {
					delete(fetches, in.RequestID)
				}
			}
			if len(fetches) >= maxMobileLogFetches {
				mu.Unlock()
				return contextmcp.Result{}, errors.New("Too many phone log reads in this execution")
			}
			running := &sideWrite{input: fingerprint, done: make(chan struct{})}
			fetches[in.RequestID] = running
			mu.Unlock()
			running.value, running.err = s.fetchMobileLogs(ctx, logs, conversationID, current, in.DeviceID, in.SinceMinutes)
			if running.err != nil {
				mu.Lock()
				delete(fetches, in.RequestID)
				mu.Unlock()
			}
			close(running.done)
			return running.value, running.err
		},
	}
}

func (s *Service) fetchMobileLogs(ctx context.Context, logs MobileLogs, conversationID string, current func() map[string]any, deviceID string, sinceMinutes int) (contextmcp.Result, error) {
	phones, err := logs.Phones()
	if err != nil {
		return contextmcp.Result{}, err
	}
	phone, err := choosePhone(phones, deviceID, s.currentOrigin(ctx, conversationID, current))
	if err != nil {
		return contextmcp.Result{}, err
	}
	value, err := logs.Read(ctx, phone.ID, sinceMinutes)
	if err != nil {
		return contextmcp.Result{}, err
	}
	return contextmcp.Result{Value: value}, nil
}
