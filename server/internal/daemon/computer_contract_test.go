package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/localhost"
	"github.com/multica-ai/multica/server/internal/localconversation/computer"
)

// TestComputerContractVersions keeps the daemon's versions and task server
// name equal to the shared contract, which Desktop implements too.
func TestComputerContractVersions(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "packages", "computer-use", "contract", "computer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Version         string `json:"version"`
		ProtocolVersion string `json:"protocolVersion"`
		TaskServerName  string `json:"taskServerName"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	if computer.CapabilityVersion != contract.Version || localhost.ComputerHostProtocolVersion != contract.ProtocolVersion || taskComputerMCPName != contract.TaskServerName {
		t.Fatalf("daemon speaks Computer %s, channel %s, server %q; contract is %+v", computer.CapabilityVersion, localhost.ComputerHostProtocolVersion, taskComputerMCPName, contract)
	}
}
