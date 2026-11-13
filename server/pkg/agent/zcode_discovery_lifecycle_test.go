//go:build !windows

package agent

import (
	"bytes"
	"context"
	"runtime/pprof"
	"strings"
	"testing"
	"time"
)

func TestZcodeModelDiscoveryReapsCommandWatchers(t *testing.T) {
	watchers := func() int {
		t.Helper()
		var profile bytes.Buffer
		if err := pprof.Lookup("goroutine").WriteTo(&profile, 2); err != nil {
			t.Fatal(err)
		}
		return strings.Count(profile.String(), "os/exec.(*Cmd).watchCtx(")
	}
	before := watchers()
	bin := writeFakeZcodeScript(t, fakeZcodeDiscoveryScript)
	for range 12 {
		catalog, err := discoverZcodeModels(context.Background(), NewCommand(bin, nil))
		if err != nil || len(catalog.Models) != 2 {
			t.Fatalf("discovery=%+v err=%v", catalog, err)
		}
	}
	deadline := time.Now().Add(time.Second)
	for {
		after := watchers()
		if after <= before+1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("CommandContext watchers leaked across repeated discovery: before=%d after=%d", before, after)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
