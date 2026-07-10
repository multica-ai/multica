package runtime

import (
	"context"
	"sort"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// FIR-2441 (the Flip, slice 2): the cloud executor moved off the api-only
// APIConnectionResolver.ListForAgent onto the unified ConnectionToolResolver,
// registering each v.Tool from Resolve(...).APITools instead. The swap must not
// change WHICH tools land in the task registry: out.APITools has to drive exactly
// the Register set ListForAgent drove before, or agents on the cloud path would
// gain or lose callable endpoints when the flag flips on.
//
// This test reproduces both register loops against a fresh Registry and asserts
// the two produce byte-identical registered sets — the parity the executor swap
// relies on. It is store-free (fakes only), so it is part of the fast unit pass.
func TestFlipSlice2ExecutorRegisterParity(t *testing.T) {
	ident := mixedIdent()

	// OLD executor source: apiConnectionResolver().ListForAgent(APIConnectionIdentity{...}).
	// NEW executor source: connectionToolResolver().Resolve(ConnectionIdentity{...}).APITools.
	// Both reuse the SAME APIConnectionResolver, so a difference here would mean
	// Resolve dropped or added a handle relative to the direct list.
	oldVerdicts := newMixedResolver().api.ListForAgent(context.Background(), ident.apiIdentity())
	newAPITools := newMixedResolver().Resolve(context.Background(), ident).APITools

	oldReg := NewRegistry(nil)
	for _, v := range oldVerdicts {
		oldReg.Register(v.Tool)
	}
	newReg := NewRegistry(nil)
	for _, v := range newAPITools {
		newReg.Register(v.Tool)
	}

	oldNames := registeredAPINames(oldReg, oldVerdicts)
	newNames := registeredAPINames(newReg, newAPITools)

	if len(newNames) == 0 {
		t.Fatal("Resolve(...).APITools registered no tools — the executor would expose nothing with the flag on")
	}
	if len(oldNames) != len(newNames) {
		t.Fatalf("register-set size drift: old=%d new=%d (%v vs %v)", len(oldNames), len(newNames), oldNames, newNames)
	}
	for i := range oldNames {
		if oldNames[i] != newNames[i] {
			t.Fatalf("register-set mismatch at %d: old=%q new=%q", i, oldNames[i], newNames[i])
		}
	}

	// Each registered handle must be recoverable as a concrete *APIConnectionTool
	// with the same verdict, so the always-on call-time guard can re-resolve it by
	// name exactly as before — the guard the Flip must leave untouched.
	for _, v := range newAPITools {
		got, ok := newReg.Get(v.Tool.Name())
		if !ok {
			t.Fatalf("tool %q not registered", v.Tool.Name())
		}
		if _, isAPI := got.(*APIConnectionTool); !isAPI {
			t.Fatalf("tool %q registered as %T, want *APIConnectionTool", v.Tool.Name(), got)
		}
		if v.Verdict != toolpolicy.SettingAllow && v.Verdict != toolpolicy.SettingAsk {
			t.Fatalf("tool %q verdict = %q, want Allow or Ask (Deny/error must be dropped)", v.Tool.Name(), v.Verdict)
		}
	}
}

// registeredAPINames returns the sorted names of the verdict tools that are
// actually present in the registry after the register loop — the observable
// "register set" the executor builds.
func registeredAPINames(reg *Registry, verds []APIConnectionToolVerdict) []string {
	var names []string
	for _, v := range verds {
		if _, ok := reg.Get(v.Tool.Name()); ok {
			names = append(names, v.Tool.Name())
		}
	}
	sort.Strings(names)
	return names
}
