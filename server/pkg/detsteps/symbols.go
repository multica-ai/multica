package detsteps

import (
	"strings"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// allowedPackages is the whitelist of pure, deterministic stdlib packages a step
// may import. The sandbox is allow-list, not deny-list: anything not listed here
// is simply not importable inside an interpreted step. Everything that can touch
// the host or the outside world — os, os/exec, io, bufio, net*, syscall,
// runtime, plugin, reflect, unsafe, path/filepath — is excluded by omission, so
// a step is computation-only.
//
// Adding a package here widens the trust boundary; do it deliberately. A package
// that exposes process/file/network/clock side effects must stay out.
var allowedPackages = []string{
	"errors",
	"fmt",
	"sort",
	"strings",
	"strconv",
	"unicode",
	"unicode/utf8",
	"bytes",
	"regexp",
	"math",
	"math/bits",
	"encoding/json",
	"encoding/base64",
	"encoding/hex",
	"time",
	"slices",
	"maps",
	"cmp",
}

// sandboxSymbols builds the yaegi symbol table exposed to interpreted steps,
// containing only the whitelisted packages. yaegi keys stdlib symbols by
// "<import-path>/<package-name>" (e.g. "encoding/json/json"), which is what
// symbolKey reconstructs.
func sandboxSymbols() interp.Exports {
	out := interp.Exports{}
	for _, pkg := range allowedPackages {
		key := symbolKey(pkg)
		if syms, ok := stdlib.Symbols[key]; ok {
			out[key] = syms
		}
	}
	return out
}

// symbolKey returns the yaegi stdlib.Symbols key for an import path: the path
// plus a trailing "/<final-segment>".
func symbolKey(importPath string) string {
	base := importPath
	if i := strings.LastIndexByte(importPath, '/'); i >= 0 {
		base = importPath[i+1:]
	}
	return importPath + "/" + base
}
