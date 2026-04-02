import SwiftUI

@main
struct MulticaMenuBarApp: App {
    @StateObject private var client = DaemonClient(autoStart: true)

    var body: some Scene {
        MenuBarExtra {
            MenuBarView(client: client)
        } label: {
            MenuBarLabel(client: client)
        }
    }
}

/// The menu bar icon and badge.
struct MenuBarLabel: View {
    @ObservedObject var client: DaemonClient

    var body: some View {
        let taskCount = client.activeTasks.count
        if client.isRunning && taskCount > 0 {
            Label("\(taskCount)", systemImage: "bolt.fill")
        } else if client.isRunning {
            Image(systemName: "bolt.fill")
        } else {
            Image(systemName: "bolt.slash")
        }
    }
}

/// The dropdown menu content.
struct MenuBarView: View {
    @ObservedObject var client: DaemonClient

    var body: some View {
        if client.isRunning, let health = client.health {
            Section("Daemon") {
                Label("Running (pid \(health.pid))", systemImage: "checkmark.circle.fill")
                Label("Uptime: \(health.uptime)", systemImage: "clock")
                Label("Agents: \(health.agents.joined(separator: ", "))", systemImage: "cpu")
                Label("Workspaces: \(health.workspaces.count)", systemImage: "folder")
            }

            Divider()

            if client.activeTasks.isEmpty {
                Section("Tasks") {
                    Label("No running tasks", systemImage: "tray")
                        .foregroundStyle(.secondary)
                }
            } else {
                Section("Running Tasks (\(client.activeTasks.count))") {
                    ForEach(client.activeTasks) { task in
                        VStack(alignment: .leading, spacing: 2) {
                            Label {
                                Text("\(task.agentName) / \(task.provider)")
                                    .fontWeight(.medium)
                            } icon: {
                                Image(systemName: "gearshape.2.fill")
                            }
                            Text("Issue: \(task.shortIssueID)  ·  \(task.duration)")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        .padding(.vertical, 2)
                    }
                }
            }
        } else {
            Section("Daemon") {
                Label("Stopped", systemImage: "xmark.circle")
                    .foregroundStyle(.secondary)
            }

            Divider()

            Button("Start Daemon") {
                startDaemon()
            }
        }

        Divider()

        Button("Refresh") {
            // Force a poll
            client.stopPolling()
            client.startPolling()
        }
        .keyboardShortcut("r")

        Button("Quit") {
            NSApplication.shared.terminate(nil)
        }
        .keyboardShortcut("q")
    }

    private func startDaemon() {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = ["multica", "daemon", "start"]
        process.standardOutput = nil
        process.standardError = nil
        try? process.run()
    }
}
