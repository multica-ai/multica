// GUI launches can outlive their terminal or session log reader. Keep failed
// log writes from becoming uncaught exceptions, including in dependencies.
// Imported first by the main entry to protect startup logging in all builds.
process.stdout.on("error", () => undefined);
process.stderr.on("error", () => undefined);
