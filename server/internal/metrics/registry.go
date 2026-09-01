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
}

type Registry struct {
	Gatherer     prometheus.Gatherer
	HTTP         *HTTPMetrics
	Business     *BusinessMetrics
	ChannelMedia *ChannelMediaReconcilerMetrics
	ChannelLease *ChannelLeaseMetrics
	Wecom        *WecomMetrics
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

	channelMedia := NewChannelMediaReconcilerMetrics()
	reg.MustRegister(channelMedia.Collectors()...)

	channelLease := NewChannelLeaseMetrics()
	reg.MustRegister(channelLease.Collectors()...)

	wecomMetrics := NewWecomMetrics()
	reg.MustRegister(wecomMetrics.Collectors()...)

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
		Gatherer:     reg,
		HTTP:         httpMetrics,
		Business:     businessMetrics,
		ChannelMedia: channelMedia,
		ChannelLease: channelLease,
		Wecom:        wecomMetrics,
	}
}

func defaultLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
