package metrics

import (
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/dwickyfp/wallts/server/internal/daemonws"
	"github.com/dwickyfp/wallts/server/internal/realtime"
)

type RegistryOptions struct {
	Pool     *pgxpool.Pool
	Realtime *realtime.Metrics
	DaemonWS *daemonws.Metrics
	Version  string
	Commit   string
}

type Registry struct {
	Gatherer prometheus.Gatherer
	HTTP     *HTTPMetrics
}

func NewRegistry(opts RegistryOptions) *Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "wallts_build_info",
		Help: "Build information for the Wallts server binary.",
	}, []string{"version", "commit"})
	buildInfo.WithLabelValues(defaultLabel(opts.Version, "dev"), defaultLabel(opts.Commit, "unknown")).Set(1)
	reg.MustRegister(buildInfo)

	httpMetrics := NewHTTPMetrics()
	reg.MustRegister(httpMetrics.Collectors()...)

	if opts.Pool != nil {
		reg.MustRegister(NewDBCollector(opts.Pool))
	}
	if opts.Realtime != nil {
		reg.MustRegister(NewRealtimeCollector(opts.Realtime))
	}
	if opts.DaemonWS != nil {
		reg.MustRegister(NewDaemonWSCollector(opts.DaemonWS))
	}

	return &Registry{
		Gatherer: reg,
		HTTP:     httpMetrics,
	}
}

func defaultLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
