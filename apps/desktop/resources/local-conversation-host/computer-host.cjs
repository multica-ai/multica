// The daemon runs this installation's persistent Computer Use runtime.
require(require("node:path").join(__dirname, "..", "..", "..", "app.asar", "out", "main", "computer-host.cjs"));
