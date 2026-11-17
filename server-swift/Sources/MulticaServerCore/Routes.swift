import Foundation
import Hummingbird
import Logging

// 🎭 The Gallery — every window the outside world may gaze through:
// health, stats, census, HUD contract, metrics, and the dashboard itself.
//
// Handlers are thin curtains: they summon, shape, and respond. All wisdom
// lives in the core types. ✨

/// 📊 The envelope the dashboard and HUD both drink from.
public struct PoolStatsEnvelope: Sendable, Codable, ResponseEncodable {
    public var applicationName: String
    public var nameSource: String
    public var pressure: String
    public var pressureSigil: String
    public var pressureCaption: String
    public var current: PoolStats
    public var samples: [PoolSample]
}

/// 🛰️ The HUD contract — compact, tile-shaped, stable keys.
public struct HUDPayload: Sendable, Codable, ResponseEncodable {
    public struct Tile: Sendable, Codable {
        public var app: String
        public var status: String
        public var pressure: String
        public var pressureSigil: String
        public var totalConns: Int
        public var maxConns: Int
        public var spark: [Int]
    }
    public var service: String
    public var generatedAt: Date
    public var tile: Tile
}

public struct HealthPayload: Sendable, Codable, ResponseEncodable {
    public var status: String
    public var applicationName: String
}

/// 🌟 Build the whole router — pure wiring, no secrets.
public func buildRouter(
    oracle: Oracle,
    observer: TelemetryObserver,
    logger: Logger
) -> Router<BasicRequestContext> {
    let router = Router<BasicRequestContext>()

    // 🧪 The health incantation
    router.get("health") { _, _ in
        HealthPayload(
            status: "ok",
            applicationName: await oracle.poolHandle.stats().applicationName
        )
    }

    // 📊 The ledger + the evening's memory
    router.get("api/pool-stats") { _, _ in
        let current = await oracle.poolHandle.stats()
        let samples = await observer.history()
        let lastDelta = samples.last?.delta ?? .init(acquires: 0, emptyAcquires: 0)
        let pressure = Pressure.verdict(for: current, delta: lastDelta)
        return PoolStatsEnvelope(
            applicationName: current.applicationName,
            nameSource: current.nameSource,
            pressure: pressure.rawValue,
            pressureSigil: pressure.sigil,
            pressureCaption: pressure.caption,
            current: current,
            samples: samples
        )
    }

    // 📖 The census — who is connected, under what names
    router.get("api/census") { _, _ in
        try await oracle.census()
    }

    // 📡 The living stream — Server-Sent Events, one heartbeat per verse.
    // EventSource on the browser side; curl -N on the terminal side.
    router.get("api/events") { _, _ -> Response in
        let stream = await observer.subscribe()
        return Response(
            status: .ok,
            headers: [
                .contentType: "text/event-stream",
                .cacheControl: "no-cache",
                .connection: "keep-alive",
            ],
            body: ResponseBody { writer in
                let encoder = JSONEncoder()
                // 🎬 The overture — a comment to warm the pipe
                try await writer.write(ByteBuffer(string: ": orchestrion live\nretry: 15000\n\n"))
                for await sample in stream {
                    let json = String(decoding: try encoder.encode(sample), as: UTF8.self)
                    try await writer.write(ByteBuffer(string: "event: pool-sample\ndata: \(json)\n\n"))
                }
                try await writer.finish(nil)
            }
        )
    }

    // 🛰️ The HUD tile's daily bread
    router.get("hud.json") { _, _ in
        let stats = await oracle.poolHandle.stats()
        let samples = await observer.history()
        let lastDelta = samples.last?.delta ?? .init(acquires: 0, emptyAcquires: 0)
        let pressure = Pressure.verdict(for: stats, delta: lastDelta)
        let spark = samples.suffix(30).map(\.stats.totalConns)
        return HUDPayload(
            service: "orchestrion",
            generatedAt: Date(),
            tile: .init(
                app: stats.applicationName,
                status: "ok",
                pressure: pressure.rawValue,
                pressureSigil: pressure.sigil,
                totalConns: stats.totalConns,
                maxConns: stats.maxConns,
                spark: Array(spark)
            )
        )
    }

    // 📈 Prometheus-text metrics, for the scrapers of the world
    router.get("metrics") { _, _ -> String in
        let stats = await oracle.poolHandle.stats()
        return """
        # TYPE multica_pool_total_conns gauge
        multica_pool_total_conns{\(Metrics.label)} \(stats.totalConns)
        # TYPE multica_pool_idle_conns gauge
        multica_pool_idle_conns{\(Metrics.label)} \(stats.idleConns)
        # TYPE multica_pool_checked_out gauge
        multica_pool_checked_out{\(Metrics.label)} \(stats.checkedOut)
        # TYPE multica_pool_waiters gauge
        multica_pool_waiters{\(Metrics.label)} \(stats.waiters)
        # TYPE multica_pool_acquire_count counter
        multica_pool_acquire_count{\(Metrics.label)} \(stats.acquireCount)
        # TYPE multica_pool_empty_acquire_count counter
        multica_pool_empty_acquire_count{\(Metrics.label)} \(stats.emptyAcquireCount)
        # TYPE multica_pool_high_water gauge
        multica_pool_high_water{\(Metrics.label)} \(stats.highWater)
        """
    }

    // 🖼️ The dashboard — dark, self-contained, alive
    router.get("dashboard") { _, _ in
        EditedResponse(
            headers: [.contentType: "text/html"],
            response: Dashboard.page
        )
    }

    return router
}

enum Metrics {
    static let label = "app=\"orchestrion\""
}

/// 🎨 The dashboard itself — one HTML page, no assets, polls its siblings.
enum Dashboard {
    static let page = """
    <!doctype html>
    <html lang="en"><head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>🌊 Orchestrion — Pool of Souls</title>
    <style>
      :root { color-scheme: dark; }
      body { margin: 0; font-family: -apple-system, "SF Pro Text", system-ui, sans-serif;
             background: oklch(0.16 0.02 260); color: oklch(0.92 0.01 90); }
      main { max-width: 860px; margin: 2rem auto; padding: 0 1rem; }
      h1 { font-size: 1.4rem; letter-spacing: .02em; }
      h1 span { opacity: .6; font-weight: 400; }
      .badge { display: inline-block; padding: .15rem .6rem; border-radius: 999px;
               font-size: .8rem; border: 1px solid oklch(0.7 0.05 90 / .35); }
      .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
              gap: .75rem; margin: 1.25rem 0; }
      .cell { background: oklch(0.21 0.025 260); border: 1px solid oklch(0.35 0.03 260);
              border-radius: 12px; padding: .75rem .9rem; }
      .cell b { display: block; font-size: 1.5rem; font-variant-numeric: tabular-nums; }
      .cell small { opacity: .55; }
      canvas { width: 100%; height: 110px; background: oklch(0.21 0.025 260);
               border: 1px solid oklch(0.35 0.03 260); border-radius: 12px; }
      table { width: 100%; border-collapse: collapse; margin-top: 1.25rem; font-variant-numeric: tabular-nums; }
      th, td { text-align: left; padding: .5rem .6rem; border-bottom: 1px solid oklch(0.3 0.03 260); }
      th { opacity: .55; font-weight: 500; font-size: .8rem; }
      footer { margin-top: 2rem; opacity: .45; font-size: .8rem; }
    </style>
    </head><body><main>
      <h1>🌊 Pool of Souls <span>· orchestrion · 🎭</span></h1>
      <div id="badge" class="badge">🔮 consulting the oracle…</div>
      <div class="grid" id="grid"></div>
      <canvas id="spark" width="820" height="110"></canvas>
      <table><thead><tr><th>application_name</th><th>souls</th></tr></thead>
      <tbody id="census"></tbody></table>
      <footer>every soul named · every mood derived · never again a nameless process ✨</footer>
    </main>
    <script>
      const fmt = n => new Intl.NumberFormat().format(n);
      function paint(env) {
        document.getElementById('badge').textContent =
          env.pressureSigil + ' ' + env.pressureCaption + ' · ' + env.applicationName;
        const s = env.current;
        document.getElementById('grid').innerHTML = [
          ['total / max', s.totalConns + ' / ' + s.maxConns],
          ['dreaming', s.idleConns], ['on stage', s.checkedOut],
          ['in line', s.waiters], ['high water', s.highWater],
          ['empty acquires', fmt(s.emptyAcquireCount)]
        ].map(([k, v]) => '<div class="cell"><small>' + k + '</small><b>' + v + '</b></div>').join('');
        const c = document.getElementById('spark'), g = c.getContext('2d');
        g.clearRect(0, 0, c.width, c.height);
        const series = env.samples.map(x => x.stats.totalConns);
        if (series.length > 1) {
          const max = Math.max(...series, env.current.maxConns);
          const pts = series.map((v, i) =>
            [10 + i * (c.width - 20) / (series.length - 1), c.height - 10 - (v / max) * (c.height - 25)]);
          g.strokeStyle = '#7dd3fc'; g.lineWidth = 2; g.beginPath();
          pts.forEach(([x, y], i) => i ? g.lineTo(x, y) : g.moveTo(x, y)); g.stroke();
        }
      }
      function census(rows) {
        document.getElementById('census').innerHTML =
          rows.map(r => '<tr><td>' + r.applicationName + '</td><td>' + r.souls + '</td></tr>').join('');
      }
      async function tick() {
        try {
          const env = await (await fetch('/api/pool-stats')).json();
          paint(env);
          census(await (await fetch('/api/census')).json());
        } catch (e) { document.getElementById('badge').textContent = '🌩️ ' + e; }
      }
      // 📡 Live verse when the browser speaks SSE; honest polling otherwise
      let history = [];
      const mergeSample = s => {
        history.push(s); if (history.length > 60) history.shift();
        paint({ current: s.stats, samples: history, pressureCaption: s.pressure,
                pressureSigil: s.pressureSigil, applicationName: s.stats.applicationName });
      };
      let es = typeof EventSource !== 'undefined'
        ? new EventSource('/api/events') : null;
      if (es) {
        es.addEventListener('pool-sample', e => mergeSample(JSON.parse(e.data)));
        es.onerror = () => { es.close(); location.reload(); };
      }
      tick();
      setInterval(tick, es ? 15000 : 2000);
    </script>
    </body></html>
    """
}
