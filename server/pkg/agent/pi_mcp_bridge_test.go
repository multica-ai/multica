package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the generated extension, using a fake adapter and event bus. Node
// is only a JS interpreter here; no installed Pi or MCP executable is loaded.
func TestPiMCPBridgeWaitsForInitialization(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required to execute the bridge fixture")
	}
	for _, ready := range []bool{true, false} {
		t.Run(map[bool]string{true: "initialized", false: "initialization_failure"}[ready], func(t *testing.T) {
			dir := piMCPFixture(t)
			emit := ""
			if ready {
				emit = `pi.events.emit("pi-mcp-adapter/status/v1",{servers:[{name:"echo",status:"not-connected"}]});`
			}
			fake := `export function createMcpAdapter({config}) { return (pi) => {
    if (Object.keys(config.mcpServers).join() !== "echo") throw Error("assignment mismatch");
    pi.on("session_start",async()=>{await Promise.resolve();` + emit + `});
  }; }`
			_ = os.WriteFile(filepath.Join(dir, "npm/node_modules/pi-mcp-adapter/index.ts"), []byte(fake), 0600)
			run, err := preparePiMCP(json.RawMessage(`{"mcpServers":{"echo":{"command":"fake-echo"}}}`), []string{"PI_CODING_AGENT_DIR=" + dir})
			if err != nil {
				t.Fatal(err)
			}
			defer run.cleanup()
			path, _ := json.Marshal(filepath.ToSlash(filepath.Join(run.dir, "multica-mcp.ts")))
			// The fixtures contain JS syntax. This test-only loader lets Node evaluate
			// .ts under node_modules, as Pi's production source loader does.
			js := `import {registerHooks} from "node:module";
import {readFileSync} from "node:fs";
import {pathToFileURL} from "node:url";
registerHooks({load(url,ctx,next){if(url.endsWith(".ts"))return {format:"module",source:readFileSync(new URL(url),"utf8"),shortCircuit:true};return next(url,ctx)}});
const handlers=[], listeners=new Map();
const pi={on(type,fn){if(type==="session_start")handlers.push(fn)},events:{on(type,fn){listeners.set(type,fn)},emit(type,data){listeners.get(type)?.(data)}}};
const lines=[];process.stdout.write=(text)=>{lines.push(text);return true};
const {default:extension}=await import(pathToFileURL(` + string(path) + `));
await extension(pi);
if(lines.length)throw Error("readiness emitted before session initialization");
const originalTimer=globalThis.setTimeout;globalThis.setTimeout=(fn)=>originalTimer(fn,5);
for(const fn of handlers)await fn({},{});
process.stderr.write(lines.join(""));`
			cmd := exec.CommandContext(t.Context(), node, "--input-type=module", "--eval", js)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("bridge fixture: %v: %s", err, out)
			}
			want := "multica_managed_mcp_failed"
			if ready {
				want = "multica_managed_mcp_ready"
			}
			if !strings.Contains(string(out), want) {
				t.Fatalf("expected %s: %s", want, out)
			}
		})
	}
}
