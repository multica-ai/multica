package wecom

// credential_probe.go — proving the installer controls the bot.
//
// A BYO install used to persist whatever pair was pasted into the dialog. The
// bot id is not a secret: it is on the WeCom admin console, and any member of
// a workspace can read it back out of GET /wecom/installations. Only the
// secret proves anything, and nothing checked it.
//
// Two things followed, and neither needs an insider.
//
// The routing slot is global. channel_installation's unique index on
// (channel_type, config->>'app_id') has no workspace in it, so an admin of any
// workspace on the deployment could write a row claiming somebody else's bot
// id with a junk secret and hold that slot indefinitely. The rightful owner
// then hits a conflict naming a workspace they cannot see.
//
// Worse, the reclaim is keyed on the same unproven id. A bot the rightful
// owner has merely DISCONNECTED sits revoked, and a revoked row is what
// ReclaimDeadChannelInstallationByAppID hard-deletes — along with every user
// binding, session binding and pending token beneath it. So an outsider
// pasting a bot id they read off a list could destroy another workspace's
// bindings and force everyone there to re-link.
//
// The fix is the one both peers already use: prove control before persisting.
// DingTalk mints an access token (dingtalk/byo_install.go), Slack calls
// auth.test (slack/byo_install.go). WeCom has no REST surface, so the proof
// is the protocol's own handshake — dial, aibot_subscribe, read the verdict,
// hang up. A wrong secret is refused with a non-zero errcode, which is
// exactly the signal.
//
// The probe never runs against a bot somebody else is actively connected to.
// WeCom allows one live subscriber per bot, so a probe against a live slot
// would displace the rightful owner's connection, whose supervisor would
// reconnect and displace the probe in turn — and if the request is then
// refused, a rejected install has knocked a live bot offline for nothing.
// InstallationService.Upsert enforces this: it takes the slot's advisory lock,
// reads the current owner, and returns the conflict without probing for any
// live owner other than the caller's own row (see botSlotConflictErr). What
// reaches the probe is a free slot, a revoked row, an orphan, or a re-install
// of the caller's own bot — nothing anybody else is connected to.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
)

// ErrCredentialsRejected is WeCom saying the pair is not valid: a wrong
// secret, a bot that no longer exists, a bot whose API mode is off. It is the
// answer that must reach the admin, because it is the one they can act on.
var ErrCredentialsRejected = errors.New("wecom: WeCom rejected this bot id and secret")

// ErrCredentialsUnverifiable is everything else — the dial failed, the
// handshake timed out, the network is down. Distinct from rejection on
// purpose: telling an admin their credentials are wrong when the deployment
// simply could not reach WeCom sends them to rotate a secret that was fine.
var ErrCredentialsUnverifiable = errors.New("wecom: could not reach WeCom to verify this bot")

// rejectionErrCodes are the WeCom global error codes (document/path/90313)
// documented as a refusal of the credential pair itself — the only answers
// entitled to tell an admin their Bot ID or secret is wrong.
//
// Everything else non-zero is ErrCredentialsUnverifiable, deliberately. WeCom
// only guarantees that 0 means success; the subscribe path is also under
// frequency and concurrency protection (45009, 45033), and the platform can
// fail on its own account. Reading any non-zero code as "wrong secret" pushes
// an admin to rotate a long-connection secret that was fine, and a rotated one
// cannot be recovered — the exact damage this file exists to prevent. So the
// list is a whitelist and the default is fail-closed: refuse the install, keep
// the stored credentials untouched, and say we could not verify.
//
// Codes only, never errmsg text: the message is human-facing Chinese prose
// that WeCom is free to reword.
var rejectionErrCodes = map[int]struct{}{
	40001: {}, // 不合法的secret参数 — the secret does not match this bot
	40013: {}, // 不合法的CorpID — the identity half of the pair is not valid
}

// credentialProbeTimeout bounds the whole probe. It has to cover a dial and
// one round trip and nothing else, and it is spent with the admin watching a
// spinner, so it is short. Sized off the connection's own two constants
// rather than invented.
const credentialProbeTimeout = handshakeTimeout + subscribeTimeout

// CredentialProbe proves a (bot_id, secret) pair is live. Exported because
// the handler holds one so a test can exercise the install path without a
// socket; production always gets the handshake probe.
type CredentialProbe interface {
	Probe(ctx context.Context, botID, secret string) error
}

// handshakeProbe is the production probe: one connection, one subscribe, no
// read loop, no registration, no sender published. Nothing else in the
// process learns it happened.
type handshakeProbe struct {
	dialer Dialer
	wsURL  string
}

// NewHandshakeProbe builds the probe the install service uses. dialer and
// wsURL are test seams; production passes nil and "".
func NewHandshakeProbe(dialer Dialer, wsURL string) *handshakeProbe {
	if wsURL == "" {
		wsURL = DefaultWSURL
	}
	return &handshakeProbe{dialer: dialer, wsURL: wsURL}
}

func (p *handshakeProbe) Probe(ctx context.Context, botID, secret string) error {
	dialer := p.dialer
	if dialer == nil {
		dialer = defaultDialer
	}

	ctx, cancel := context.WithTimeout(ctx, credentialProbeTimeout)
	defer cancel()

	conn, _, err := dialer.DialContext(ctx, p.wsURL, nil)
	if err != nil {
		return fmt.Errorf("%w: dial: %v", ErrCredentialsUnverifiable, err)
	}
	defer conn.Close()

	// The read below does not observe ctx, so the deadline has to be on the
	// socket as well as on the context.
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(dl)
		_ = conn.SetReadDeadline(dl)
	}

	reqID := newReqID()
	frame, err := json.Marshal(map[string]any{
		"cmd":     cmdSubscribe,
		"headers": frameHeaders{ReqID: reqID},
		"body":    subscribeBody(botID, secret),
	})
	if err != nil {
		return fmt.Errorf("%w: encode subscribe: %v", ErrCredentialsUnverifiable, err)
	}
	if err := conn.WriteMessage(websocketTextMessage, frame); err != nil {
		return fmt.Errorf("%w: send subscribe: %v", ErrCredentialsUnverifiable, err)
	}

	// Read until our own ack comes back. The server may push other frames
	// first; anything that is not the answer to this req_id is not ours.
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %v", ErrCredentialsUnverifiable, err)
		}
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("%w: read subscribe ack: %v", ErrCredentialsUnverifiable, err)
		}
		var env frameEnvelope
		if err := json.Unmarshal(payload, &env); err != nil {
			continue
		}
		if env.Headers.ReqID != reqID {
			continue
		}
		if env.ErrCode != 0 {
			if _, rejected := rejectionErrCodes[env.ErrCode]; rejected {
				// The one answer the admin can act on, and the only branch
				// that blames their credentials.
				return fmt.Errorf("%w (errcode %d: %s)", ErrCredentialsRejected, env.ErrCode, env.ErrMsg)
			}
			// Unknown non-zero: throttling, a platform-side failure, or a code
			// WeCom added since. Fail closed rather than blame the secret. The
			// raw pair is logged here so an operator can see it even if the
			// caller only surfaces the sentinel, and carried on the error so
			// the handler's own log line has it too.
			slog.Warn("wecom credential probe: unrecognized subscribe errcode, treating as unverifiable",
				"errcode", env.ErrCode, "errmsg", env.ErrMsg)
			return fmt.Errorf("%w (subscribe returned errcode %d: %s)", ErrCredentialsUnverifiable, env.ErrCode, env.ErrMsg)
		}
		return nil
	}
}

// websocketTextMessage is gorilla's TextMessage without importing gorilla
// here — the probe writes one frame and this keeps its dependency surface to
// the Dialer interface the package already defines.
const websocketTextMessage = 1
