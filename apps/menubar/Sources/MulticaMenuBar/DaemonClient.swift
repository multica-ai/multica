import Foundation

/// Represents a task currently being executed by the daemon.
struct ActiveTask: Codable, Identifiable {
    let id: String
    let issueID: String
    let workspaceID: String
    let agentName: String
    let provider: String
    let startedAt: Date

    enum CodingKeys: String, CodingKey {
        case id
        case issueID = "issue_id"
        case workspaceID = "workspace_id"
        case agentName = "agent_name"
        case provider
        case startedAt = "started_at"
    }

    var shortID: String {
        String(id.prefix(8))
    }

    var shortIssueID: String {
        String(issueID.prefix(8))
    }

    var duration: String {
        let elapsed = Date().timeIntervalSince(startedAt)
        let minutes = Int(elapsed) / 60
        let seconds = Int(elapsed) % 60
        if minutes > 0 {
            return "\(minutes)m \(seconds)s"
        }
        return "\(seconds)s"
    }
}

/// Health response from the daemon's local endpoint.
struct HealthResponse: Codable {
    let status: String
    let pid: Int
    let uptime: String
    let daemonID: String
    let deviceName: String
    let serverURL: String
    let agents: [String]
    let workspaces: [HealthWorkspace]
    let activeTasks: [ActiveTask]?

    enum CodingKeys: String, CodingKey {
        case status, pid, uptime, agents, workspaces
        case daemonID = "daemon_id"
        case deviceName = "device_name"
        case serverURL = "server_url"
        case activeTasks = "active_tasks"
    }
}

struct HealthWorkspace: Codable {
    let id: String
    let runtimes: [String]
}

/// Client for polling the daemon's local health endpoint.
@MainActor
final class DaemonClient: ObservableObject {
    @Published var isRunning = false
    @Published var health: HealthResponse?
    @Published var activeTasks: [ActiveTask] = []

    private let port: Int
    private var timer: Timer?

    private lazy var decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
    }()

    init(port: Int = 19514, autoStart: Bool = false) {
        self.port = port
        if autoStart {
            // Defer to next run loop to allow MainActor context.
            DispatchQueue.main.async { [weak self] in
                self?.startPolling()
            }
        }
    }

    func startPolling(interval: TimeInterval = 3.0) {
        poll()
        timer = Timer.scheduledTimer(withTimeInterval: interval, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.poll()
            }
        }
    }

    func stopPolling() {
        timer?.invalidate()
        timer = nil
    }

    private func poll() {
        let url = URL(string: "http://127.0.0.1:\(port)/health")!
        var request = URLRequest(url: url)
        request.timeoutInterval = 2

        URLSession.shared.dataTask(with: request) { [weak self] data, response, error in
            Task { @MainActor [weak self] in
                guard let self else { return }

                guard let data, error == nil,
                      let resp = try? self.decoder.decode(HealthResponse.self, from: data) else {
                    self.isRunning = false
                    self.health = nil
                    self.activeTasks = []
                    return
                }

                self.isRunning = resp.status == "running"
                self.health = resp
                self.activeTasks = resp.activeTasks ?? []
            }
        }.resume()
    }
}
