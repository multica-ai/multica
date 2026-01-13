function App(): React.JSX.Element {
  const handleTestIPC = async (): Promise<void> => {
    try {
      const session = await window.electronAPI.createSession('/tmp')
      console.log('Session created:', session)
    } catch (error) {
      console.error('IPC Error:', error)
    }
  }

  return (
    <div className="flex h-screen flex-col">
      {/* Title bar drag region */}
      <div className="titlebar-drag-region h-8 flex-shrink-0" />

      {/* Main content */}
      <div className="flex flex-1 overflow-hidden">
        {/* Sidebar placeholder */}
        <aside className="w-64 flex-shrink-0 border-r border-[var(--color-border)] bg-[var(--color-surface)] p-4">
          <h2 className="mb-4 text-sm font-semibold text-[var(--color-text-muted)]">Sessions</h2>
          <p className="text-sm text-[var(--color-text-muted)]">No sessions yet</p>
        </aside>

        {/* Chat area placeholder */}
        <main className="flex flex-1 flex-col">
          {/* Chat messages area */}
          <div className="flex flex-1 items-center justify-center overflow-y-auto p-4">
            <div className="text-center">
              <h1 className="mb-2 text-3xl font-bold">Multica</h1>
              <p className="mb-6 text-[var(--color-text-muted)]">
                A GUI client for ACP-compatible coding agents
              </p>
              <button
                onClick={handleTestIPC}
                className="titlebar-no-drag rounded-lg bg-[var(--color-primary)] px-4 py-2 font-medium text-white transition-colors hover:bg-[var(--color-primary-dark)]"
              >
                Test IPC Connection
              </button>
              <p className="mt-4 text-sm text-[var(--color-text-muted)]">
                Press F12 to open DevTools
              </p>
            </div>
          </div>

          {/* Input area placeholder */}
          <div className="flex-shrink-0 border-t border-[var(--color-border)] p-4">
            <div className="mx-auto flex max-w-3xl gap-2">
              <input
                type="text"
                placeholder="Type a message..."
                className="flex-1 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-2 text-[var(--color-text)] outline-none placeholder:text-[var(--color-text-muted)] focus:border-[var(--color-primary)]"
              />
              <button className="rounded-lg bg-[var(--color-primary)] px-4 py-2 font-medium text-white transition-colors hover:bg-[var(--color-primary-dark)]">
                Send
              </button>
            </div>
          </div>
        </main>
      </div>
    </div>
  )
}

export default App
