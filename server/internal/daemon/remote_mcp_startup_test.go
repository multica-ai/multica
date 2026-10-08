package daemon

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/remotemcp"
	"github.com/multica-ai/multica/server/pkg/remotemcp/remotemcptest"
)

// Connection startup: the check that makes "the administrator approved these
// tools" mean something at runtime.
//
// The proxy below this layer was already covered against the same fixture, but
// startup itself was not reachable from a test — it validates the endpoint as a
// public HTTPS URL, and the fixture serves plain HTTP on a loopback address. So
// the layer carrying the whole approval guarantee ran zero times. These tests
// exist because MULTICA_PLUGIN_DEV_ORIGINS now lets a caller name that origin.

// The version remotemcptest.Server negotiates. Named once so a fixture change
// is a single edit rather than four.
var fixtureProtocolVersions = []string{"2025-03-26"}

func startupConnection(fixture *remotemcptest.Server, approved []remotemcp.Tool) remotemcp.Connection {
	return remotemcp.Connection{
		InstallationID:       "install-1",
		ContributionID:       remotemcp.PluginContributionPrefix + "install-1:toolbox",
		ContributionKey:      "toolbox",
		Endpoint:             fixture.URL,
		Transport:            "http",
		EndpointAllowedHosts: []string{"127.0.0.1"},
		ProtocolVersions:     fixtureProtocolVersions,
		CredentialHeader:     "Authorization",
		ApprovedTools:        approved,
	}
}

func fixtureCredential(context.Context, string) (http.Header, error) {
	return http.Header{"Authorization": []string{"Bearer " + remotemcptest.Credential}}, nil
}

// discoveredTools asks the fixture what it offers, so a test can pin the real
// digest instead of hardcoding one that would rot the moment the fixture's
// schema changed.
func discoveredTools(t *testing.T, fixture *remotemcptest.Server) map[string]remotemcp.Tool {
	t.Helper()
	tools, _, err := remotemcp.Discover(context.Background(), fixture.URL,
		[]string{"127.0.0.1"}, fixtureProtocolVersions,
		http.Header{"Authorization": []string{"Bearer " + remotemcptest.Credential}})
	if err != nil {
		t.Fatalf("discover fixture tools: %v", err)
	}
	byName := make(map[string]remotemcp.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	return byName
}

func startBrokers(t *testing.T, connection remotemcp.Connection) ([]string, error) {
	t.Helper()
	ctx := context.Background()
	config, diagnostics, set, err := startTaskRemoteMCPBrokers(
		ctx, ctx, "task-1", "claude", []remotemcp.Connection{connection}, fixtureCredential, nil)
	if set != nil {
		t.Cleanup(set.Close)
	}
	if err == nil && len(diagnostics) == 0 && len(config) == 0 {
		t.Fatal("startup reported neither a connection, a diagnostic, nor an error")
	}
	return diagnostics, err
}

// The seam is off unless an operator names the origin. Without it this exact
// connection is refused, which is what every production deployment does.
func TestRemoteMCPStartupRefusesALoopbackEndpointByDefault(t *testing.T) {
	fixture := remotemcptest.NewServer()
	defer fixture.Close()

	tools := discoveredToolsWithDevOrigin(t, fixture)
	_, err := startBrokers(t, startupConnection(fixture, []remotemcp.Tool{tools["fixture.read"]}))
	if err == nil {
		t.Fatal("a loopback endpoint started without the operator naming it")
	}
}

// discoveredToolsWithDevOrigin is the same lookup, but scoped so the enclosing
// test can then run WITHOUT the variable set.
func discoveredToolsWithDevOrigin(t *testing.T, fixture *remotemcptest.Server) map[string]remotemcp.Tool {
	t.Helper()
	t.Setenv(remotemcp.DevOriginsEnv, fixture.URL)
	tools := discoveredTools(t, fixture)
	t.Setenv(remotemcp.DevOriginsEnv, "")
	return tools
}

func TestRemoteMCPStartupAcceptsAnApprovedToolSet(t *testing.T) {
	fixture := remotemcptest.NewServer()
	defer fixture.Close()
	t.Setenv(remotemcp.DevOriginsEnv, fixture.URL)

	tools := discoveredTools(t, fixture)
	diagnostics, err := startBrokers(t, startupConnection(fixture, []remotemcp.Tool{tools["fixture.read"]}))
	if err != nil {
		t.Fatalf("an approved, unchanged tool set must start: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
}

// The server dropped a tool the administrator approved. Refusing here is the
// point: the agent would otherwise get a connection whose contents no longer
// match what anyone signed off on.
func TestRemoteMCPStartupRefusesWhenAnApprovedToolIsMissing(t *testing.T) {
	fixture := remotemcptest.NewServer()
	defer fixture.Close()
	t.Setenv(remotemcp.DevOriginsEnv, fixture.URL)

	approved := []remotemcp.Tool{{Name: "fixture.deleted", SchemaDigest: "sha256:whatever"}}
	_, err := startBrokers(t, startupConnection(fixture, approved))
	if err == nil {
		t.Fatal("startup accepted a connection whose approved tool is gone")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error should say the approved tool is missing, got: %v", err)
	}
}

// The tool kept its name and changed its arguments. This is the case a
// name-only approval could not catch, and the reason approvals pin a digest.
func TestRemoteMCPStartupRefusesWhenAnApprovedSchemaDrifted(t *testing.T) {
	fixture := remotemcptest.NewServer()
	defer fixture.Close()
	t.Setenv(remotemcp.DevOriginsEnv, fixture.URL)

	approved := []remotemcp.Tool{{Name: "fixture.read", SchemaDigest: "sha256:the-shape-that-was-approved"}}
	_, err := startBrokers(t, startupConnection(fixture, approved))
	if err == nil {
		t.Fatal("startup accepted a tool whose schema no longer matches the approval")
	}
	if !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("error should say the schema drifted, got: %v", err)
	}
}

// A Plugin's MCP server being down must not fail somebody's task. The optional
// policy is what a plugin-contributed connection always carries.
func TestRemoteMCPStartupDegradesAnOptionalConnectionInsteadOfFailingTheTask(t *testing.T) {
	fixture := remotemcptest.NewServer()
	defer fixture.Close()
	t.Setenv(remotemcp.DevOriginsEnv, fixture.URL)

	connection := startupConnection(fixture, []remotemcp.Tool{{Name: "fixture.deleted"}})
	connection.FailurePolicy = "optional"

	diagnostics, err := startBrokers(t, connection)
	if err != nil {
		t.Fatalf("an optional connection must not fail the task: %v", err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0], "toolbox") {
		t.Fatalf("the failure must still be reported as a diagnostic, got: %v", diagnostics)
	}
}
