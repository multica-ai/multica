package metrics

import (
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/realtime"
)

type RegistryOptions struct {
	Pool     *pgxpool.Pool
	Realtime *realtime.Metrics
	DaemonWS *daemonws.Metrics
	Version  string
	Commit   string

	// BusinessSampler, when non-nil, opts the registry into the
	// scrape-time SQL sampler from PR4 (MUL-2947). It is intentionally
	// separate from Pool so existing tests (and any deployment without
	// METRICS_ADDR) cannot accidentally start hitting the database on
	// every /metrics scrape.
	BusinessSampler *BusinessSamplerOptions
}

type Registry struct {
	Gatherer prometheus.Gatherer
	HTTP     *HTTPMetrics
	Business *BusinessMetrics
	// Sampler is non-nil only when RegistryOptions.BusinessSampler was
	// supplied with a valid Pool. Exposed so the cmd/server entrypoint
	// can plumb the same instance into health checks if it ever wants to.
	Sampler *BusinessSamplerCollector
}

func NewRegistry(opts RegistryOptions) *Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "multica_build_info",
		Help: "Build information for the Multica server binary.",
	}, []string{"version", "commit"})
	buildInfo.WithLabelValues(defaultLabel(opts.Version, "dev"), defaultLabel(opts.Commit, "unknown")).Set(1)
	reg.MustRegister(buildInfo)

	httpMetrics := NewHTTPMetrics()
	reg.MustRegister(httpMetrics.Collectors()...)

	businessMetrics := NewBusinessMetrics()
	reg.MustRegister(businessMetrics.Collectors()...)

	if opts.Pool != nil {
		reg.MustRegister(NewDBCollector(opts.Pool))
	}
	if opts.Realtime != nil {
		reg.MustRegister(NewRealtimeCollector(opts.Realtime))
	}
	if opts.DaemonWS != nil {
		reg.MustRegister(NewDaemonWSCollector(opts.DaemonWS))
	}

	sampler := NewBusinessSamplerCollector(opts.BusinessSampler)
	if sampler != nil {
		reg.MustRegister(sampler.Collectors()...)
	}

	return &Registry{
		Gatherer: reg,
		HTTP:     httpMetrics,
		Business: businessMetrics,
		Sampler:  sampler,
	}
}

func defaultLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
