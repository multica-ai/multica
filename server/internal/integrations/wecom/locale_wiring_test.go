package wecom

// locale_wiring_test.go — the bubble's own copy is read off the pack for the
// reader it is addressed to, not off a literal compiled into whichever closer
// happens to seal it.
//
// The language is resolved when the bubble is OPENED, while who asked is still
// in hand, and carried on the handle to every closer — each of which runs
// later, from an event that names a task and nobody else. A closer that reached
// for the deployment default instead would look right in isolation and be wrong
// for every reader who set a language, so the test below drives the real
// open-then-close path and asserts on what came out of the socket.
//
// The rest of what this adapter says is still the Chinese literal at its own
// call site; those surfaces get their own test when they move to the pack.

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// localeTestUserID is the Multica user every bound sender in this file
// resolves to. Deliberately not mustTestUUID's installation id: a lookup that
// confuses the two must not pass.
var localeTestUserID = pgtype.UUID{Bytes: [16]byte{77}, Valid: true}

// fakeLanguages is a languageLookup holding one bound person: their WeCom
// userid, the Multica user it resolves to, and that profile's language.
// Anyone else is unbound, which is what a real first-time sender is.
type fakeLanguages struct {
	senderID string
	userID   pgtype.UUID
	language string
}

func (f fakeLanguages) GetChannelUserBindingByUserID(_ context.Context, arg db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	if arg.ChannelUserID == f.senderID {
		return db.ChannelUserBinding{MulticaUserID: f.userID}, nil
	}
	return db.ChannelUserBinding{}, pgx.ErrNoRows
}

func (f fakeLanguages) GetUser(_ context.Context, id pgtype.UUID) (db.User, error) {
	if id == f.userID {
		return db.User{ID: id, Language: pgtype.Text{String: f.language, Valid: true}}, nil
	}
	return db.User{}, pgx.ErrNoRows
}

// localeCases is the pair every surface below is driven with. The expected
// text is read off the packs rather than spelled out again: the assertion is
// that the SURFACE consults the pack, and duplicating the wording here would
// only give it a second place to drift from.
var localeCases = []struct {
	name     string
	language string
	locale   Locale
}{
	{"english profile", "en", LocaleEn},
	{"chinese profile", "zh-Hans", LocaleZhHans},
}

// ---- surface 5: the streaming bubble ----

// TestTheBubbleClosesInTheAskersLanguage drives the real open-then-close path.
// The language is resolved when the bubble is opened and carried on the
// handle, because every closer runs later from an event that names a task and
// nobody else — so a closer that reached for the deployment default instead
// would look right in isolation and be wrong for every reader who set a
// language.
func TestTheBubbleClosesInTheAskersLanguage(t *testing.T) {
	t.Parallel()
	for _, tc := range localeCases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newBubbleRig(t)
			// ask() sends as USER_1 in a 1:1, so the bubble belongs to one
			// person and reads their profile.
			rig.typing.languages = fakeLanguages{senderID: "USER_1", userID: localeTestUserID, language: tc.language}
			rig.ran(t, "REQ-L", "task-1")
			rig.answer(t, "   \n ", "task-1")

			frames := rig.conn.streamFrames(t)
			if len(frames) != 2 {
				t.Fatalf("got %d stream frames, want 2 (open + seal)", len(frames))
			}
			if frames[1]["finish"] != true {
				t.Fatal("an empty answer did not seal the bubble; it spins until the platform ends it")
			}
			if got, want := frames[1]["content"], copyPacks[tc.locale].StreamNoReply; got != want {
				t.Fatalf("closing copy = %q, want the %s copy %q", got, tc.locale, want)
			}
		})
	}
}

// ---- the deployment knob ----

// restoreLocale points the deployment at l for the duration of one test and
// puts the previous value back. Callers must NOT be parallel: this is a
// process-wide setting every reader with no profile reads.
func restoreLocale(t *testing.T, l Locale) {
	t.Helper()
	prev := deploymentLocale()
	deploymentLocaleValue.Store(l)
	t.Cleanup(func() { deploymentLocaleValue.Store(prev) })
}

// TestSetDeploymentLocaleIgnoresWhatItDoesNotRecognise — an env var is
// validated by nobody. A typo must leave the language where it was rather than
// quietly moving a tenant onto the other pack.
func TestSetDeploymentLocaleIgnoresWhatItDoesNotRecognise(t *testing.T) {
	// Every existing deployment sets nothing, and nothing must keep meaning
	// zh-Hans — WeCom is a Chinese platform.
	if DefaultLocale != LocaleZhHans {
		t.Fatalf("DefaultLocale = %q, want zh-Hans", DefaultLocale)
	}
	restoreLocale(t, LocaleZhHans)
	for _, junk := range []string{"zh_Hant", "english", "EN-US", `"en"`, "  ", "fr"} {
		if got := SetDeploymentLocale(junk); got != LocaleZhHans {
			t.Fatalf("SetDeploymentLocale(%q) = %q, want the previous value kept", junk, got)
		}
	}
	for _, ok := range []struct {
		in   string
		want Locale
	}{{"en", LocaleEn}, {"EN", LocaleEn}, {" en ", LocaleEn}, {"zh-Hans", LocaleZhHans}, {"zh", LocaleZhHans}} {
		if got := SetDeploymentLocale(ok.in); got != ok.want {
			t.Fatalf("SetDeploymentLocale(%q) = %q, want %q", ok.in, got, ok.want)
		}
	}
}

// ---- the packs ----

// TestEveryPackSaysSomethingVisible: each copy string is the whole content of
// a closing frame, and WeCom discards a closing frame with nothing visible in
// it — the bubble it was meant to seal then spins until the platform ends it.
// So no string in any pack may be blank. The wording itself is not pinned: the
// test above proves the closer reads the pack, and a copy edit should be a
// one-file change.
func TestEveryPackSaysSomethingVisible(t *testing.T) {
	t.Parallel()
	for locale, pack := range copyPacks {
		v := reflect.ValueOf(pack)
		for i := range v.NumField() {
			if v.Field(i).Kind() != reflect.String {
				continue
			}
			if s := v.Field(i).String(); !hasVisibleChar(s) {
				t.Errorf("%s copy %s = %q has nothing visible; WeCom would discard the closing frame",
					locale, v.Type().Field(i).Name, s)
			}
		}
	}
}
