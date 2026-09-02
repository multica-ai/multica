package wecom

// locale_wiring_test.go — the streaming bubble reads the copy pack, not a
// literal compiled into the file that happens to send it.
//
// Trimmed to the bubble: the trunk's version of this file drives every surface
// the adapter speaks on (the replier's notices, the inbox card, the media
// notices, the unsupported-kind receipt) and the deployment-level locale knob.
// Those surfaces keep their Chinese constants on this branch, so only the
// bubble's own surface and the zh-Hans compatibility pin are here.

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

// localeCases is the pair the bubble is driven with. The expected text is read
// off the packs rather than spelled out again: the assertion is that the
// SURFACE consults the pack, and duplicating the wording here would only give
// it a second place to drift from.
var localeCases = []struct {
	name     string
	language string
	locale   Locale
}{
	{"english profile", "en", LocaleEn},
	{"chinese profile", "zh-Hans", LocaleZhHans},
}

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
			rig.ran(t, "REQ-L", 1, "task-1")
			rig.answer(t, "   \n ", "task-1")

			frames := rig.conn.streamFrames(t)
			if len(frames) != 2 {
				t.Fatalf("got %d stream frames, want 2 (open + seal)", len(frames))
			}
			if got, want := frames[1]["content"], copyPacks[tc.locale].StreamNoReply; got != want {
				t.Fatalf("closing copy = %q, want the %s copy %q", got, tc.locale, want)
			}
		})
	}
}

// TestTheRoomReadsTheDeploymentNotTheMember — a room has many readers and no
// shared profile, so it reads the deployment default. This is the guard on
// the fallback: it must be deploymentLocale(), not the triggering member's
// personal setting.
func TestTheRoomReadsTheDeploymentNotTheMember(t *testing.T) {
	t.Parallel()
	inst := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	// The member who spoke reads English...
	q := fakeLanguages{senderID: "T-asker", userID: localeTestUserID, language: "en"}
	if got := localeFor(context.Background(), q, inst, chatTypeSingleInt, "T-asker"); got != LocaleEn {
		t.Fatalf("1:1 locale = %q, want the asker's own %q", got, LocaleEn)
	}
	// ...and the room still reads the deployment default, in front of
	// everybody else in it.
	if got := localeFor(context.Background(), q, inst, chatTypeGroupInt, "T-asker"); got != deploymentLocale() {
		t.Fatalf("room locale = %q, want the deployment's %q", got, deploymentLocale())
	}
}

// TestDefaultLocaleIsChinese is the compatibility guard: WeCom is a Chinese
// platform, and a reader nobody has a profile for reads zh-Hans.
func TestDefaultLocaleIsChinese(t *testing.T) {
	t.Parallel()
	if DefaultLocale != LocaleZhHans {
		t.Fatalf("DefaultLocale = %q, want zh-Hans — WeCom is a Chinese platform", DefaultLocale)
	}
	if got := copyFor(deploymentLocale()).StreamNoReply; got != copyPacks[LocaleZhHans].StreamNoReply {
		t.Fatalf("unset deployment reads %q, want the Chinese pack", got)
	}
	// An unknown locale falls back rather than to an empty pack.
	if got := copyFor(Locale("fr")).StreamFailed; got != copyPacks[LocaleZhHans].StreamFailed {
		t.Fatalf("copyFor(fr) = %q, want the deployment's pack", got)
	}
}

// ---- the compatibility pin ----

// TestZhHansPackIsTheCopyThatAlreadyShipped pins the wording of every line the
// bubble says. Every other test in this file reads its expected text off the
// pack, which proves the SURFACE consults the pack and proves nothing at all
// about what the pack says — edit a zh-Hans string and they all still pass. So
// the wording is spelled out once, here, so that changing one is a deliberate
// edit here rather than a silent one in the pack.
//
// It walks the struct rather than checking a handful of fields, so a copy
// string added later without a line in the table fails here instead of
// shipping unreviewed. That makes the table the place a zh-Hans wording change
// has to be argued for, which is the point.
func TestZhHansPackIsTheCopyThatAlreadyShipped(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"StreamNoReply":          "（这轮没有需要回复的内容）",
		"StreamNoReplyWithFiles": "（这轮没有文字回复，附件在下面）",
		"StreamMerged":           "✅ 这条已并入上一条回复一起处理了。",
		"StreamNotStarted":       "已收到，但这条暂时没能开始处理。",
		"StreamFailed":           "⚠️ 这次没跑通，请稍后再试一次。",
		"StreamCancelled":        "⏹️ 这次处理已取消。",
		"StreamContinued":        "处理时间较长，接下一条",
		"StreamStuck":            "⚠️ 上面那条进度不会再更新了，这轮的结果我用新消息发你。",
		"StreamProgressPrefix":   "正在处理：",
	}

	zh := reflect.ValueOf(copyPacks[LocaleZhHans])
	typ := zh.Type()
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		switch typ.Field(i).Type.Kind() {
		case reflect.String:
			expected, listed := want[name]
			if !listed {
				t.Errorf("copyPack.%s is a reader-visible string with no line in this table; add the text it shipped with", name)
				continue
			}
			if got := zh.Field(i).String(); got != expected {
				t.Errorf("zh-Hans %s = %q, want %q — a Chinese tenant reads this, so change it deliberately or not at all", name, got, expected)
			}
			delete(want, name)
		case reflect.Struct:
			if name != "Progress" {
				t.Errorf("copyPack.%s is a nested struct this test does not know how to pin", name)
				continue
			}
			pinProgressCopy(t, zh.Field(i))
		default:
			t.Errorf("copyPack.%s has kind %s, which this test does not pin", name, typ.Field(i).Type.Kind())
		}
	}
	for name := range want {
		t.Errorf("this table pins copyPack.%s, which no longer exists", name)
	}
}

// pinProgressCopy is the same table for the step lines inside the bubble.
// What this pins is that a change to any of them is made here, deliberately,
// and not slipped into the pack — and that every line keeps the verb count its
// format string needs, which is the half a reviewer cannot see by reading.
func pinProgressCopy(t *testing.T, v reflect.Value) {
	t.Helper()
	want := map[string]string{
		"Read":         "正在读取 %s",
		"ReadPlain":    "正在读取文件",
		"Edit":         "正在修改 %s",
		"EditPlain":    "正在修改文件",
		"Command":      "正在执行命令",
		"CommandNamed": "正在执行 %s",
		"Search":       "正在检索代码",
		"SearchNamed":  "正在检索 %s",
		"Web":          "正在查资料",
		"WebNamed":     "正在查 %s",
		"Subtask":      "正在派子任务",
		"SubtaskNamed": "正在派子任务：%s",
		"Plan":         "正在梳理计划",
		"PlanNamed":    "正在梳理计划：%s",
		"Service":      "正在调用 %s · %s",
		"ServiceArgs":  "正在调用 %s · %s：%s",
		"Skill":        "正在启用技能 %s",
		"SkillPlain":   "正在启用技能",
		"Tool":         "正在使用 %s",
		"ToolArgs":     "正在使用 %s：%s",
		"Fallback":     "正在处理",
		"Failed":       "上一步出错了，正在继续",
		"FailedNamed":  "上一步出错了：%s，正在继续",
		"Thinking":     "思考：",
		"Elapsed":      "已用时 %s",
	}
	typ := v.Type()
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		expected, listed := want[name]
		if !listed {
			t.Errorf("progressCopy.%s is a reader-visible string with no line in this table", name)
			continue
		}
		if got := v.Field(i).String(); got != expected {
			t.Errorf("zh-Hans progress %s = %q, want %q", name, got, expected)
		}
		delete(want, name)
	}
	for name := range want {
		t.Errorf("this table pins progressCopy.%s, which no longer exists", name)
	}
}
