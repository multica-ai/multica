package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisPingTimeout = 5 * time.Second

// RedisConfig maps the shared Redis connection environment variables.
type RedisConfig struct {
	URL         string `env:"REDIS_URL"`
	ClusterMode bool   `env:"REDIS_CLUSTER_MODE"`
}

// NewRedisClient builds a standalone or cluster client and verifies that it is
// reachable before returning it. rediss:// URLs enable TLS through ParseURL.
func NewRedisClient(cfg RedisConfig) (redis.UniversalClient, error) {
	opts, err := NewRedisOptions(cfg)
	if err != nil {
		return nil, err
	}
	client := redis.NewUniversalClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), redisPingTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, errors.Join(fmt.Errorf("ping redis: %w", err), client.Close())
	}
	return client, nil
}

// RedisContextTimeoutEnabled reports the context-deadline setting for clients
// created by NewUniversalClient. It keeps concrete client inspection inside
// the initialization package while consumers depend only on UniversalClient.
func RedisContextTimeoutEnabled(client redis.UniversalClient) (enabled, known bool) {
	switch client := client.(type) {
	case *redis.Client:
		return client.Options().ContextTimeoutEnabled, true
	case *redis.ClusterClient:
		return client.Options().ContextTimeoutEnabled, true
	default:
		return false, false
	}
}

// NewRedisOptions parses the URL once per seed while preserving the connection,
// pool, authentication, and TLS options understood by go-redis. A comma-separated
// authority is normalized before ParseURL because net/url rejects multiple ports.
func NewRedisOptions(cfg RedisConfig) (*redis.UniversalOptions, error) {
	rawURL := strings.TrimSpace(cfg.URL)
	if strings.HasPrefix(rawURL, "unix://") {
		if cfg.ClusterMode {
			return nil, errors.New("redis cluster mode does not support unix socket URLs")
		}
		opts, err := redis.ParseURL(rawURL)
		if err != nil {
			return nil, fmt.Errorf("parse redis URL: %w", err)
		}
		return universalOptions(opts, []string{opts.Addr}, false), nil
	}

	seedURLs, err := redisSeedURLs(rawURL)
	if err != nil {
		return nil, err
	}
	if len(seedURLs) > 1 && !cfg.ClusterMode {
		return nil, errors.New("REDIS_CLUSTER_MODE must be true when REDIS_URL contains multiple addresses")
	}

	var base *redis.Options
	addrs := make([]string, 0, len(seedURLs))
	for _, seedURL := range seedURLs {
		opts, err := redis.ParseURL(seedURL)
		if err != nil {
			return nil, fmt.Errorf("parse redis URL: %w", err)
		}
		if base == nil {
			base = opts
		}
		addrs = append(addrs, opts.Addr)
	}
	if base == nil {
		return nil, errors.New("REDIS_URL does not contain a Redis address")
	}
	if cfg.ClusterMode && base.DB != 0 {
		return nil, errors.New("REDIS_URL database must be 0 in cluster mode")
	}
	return universalOptions(base, addrs, cfg.ClusterMode), nil
}

func redisSeedURLs(rawURL string) ([]string, error) {
	schemeEnd := strings.Index(rawURL, "://")
	if schemeEnd < 0 {
		return []string{rawURL}, nil
	}
	authorityStart := schemeEnd + len("://")
	authorityEnd := len(rawURL)
	if end := strings.IndexAny(rawURL[authorityStart:], "/?#"); end >= 0 {
		authorityEnd = authorityStart + end
	}
	hostStart := authorityStart
	if userinfoEnd := strings.LastIndex(rawURL[authorityStart:authorityEnd], "@"); userinfoEnd >= 0 {
		hostStart += userinfoEnd + 1
	}

	seeds := strings.Split(rawURL[hostStart:authorityEnd], ",")
	seedURLs := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		seed = strings.TrimSpace(seed)
		if seed == "" {
			return nil, errors.New("REDIS_URL contains an empty Redis address")
		}
		seedURLs = append(seedURLs, rawURL[:hostStart]+seed+rawURL[authorityEnd:])
	}
	return seedURLs, nil
}

func universalOptions(opts *redis.Options, addrs []string, clusterMode bool) *redis.UniversalOptions {
	return &redis.UniversalOptions{
		Addrs:                        addrs,
		ClientName:                   opts.ClientName,
		Dialer:                       opts.Dialer,
		OnConnect:                    opts.OnConnect,
		Protocol:                     opts.Protocol,
		Username:                     opts.Username,
		Password:                     opts.Password,
		CredentialsProvider:          opts.CredentialsProvider,
		CredentialsProviderContext:   opts.CredentialsProviderContext,
		StreamingCredentialsProvider: opts.StreamingCredentialsProvider,
		DB:                           opts.DB,
		MaxRetries:                   opts.MaxRetries,
		MinRetryBackoff:              opts.MinRetryBackoff,
		MaxRetryBackoff:              opts.MaxRetryBackoff,
		DialTimeout:                  opts.DialTimeout,
		DialerRetries:                opts.DialerRetries,
		DialerRetryTimeout:           opts.DialerRetryTimeout,
		ReadTimeout:                  opts.ReadTimeout,
		WriteTimeout:                 opts.WriteTimeout,
		ContextTimeoutEnabled:        opts.ContextTimeoutEnabled,
		ReadBufferSize:               opts.ReadBufferSize,
		WriteBufferSize:              opts.WriteBufferSize,
		PoolFIFO:                     opts.PoolFIFO,
		PoolSize:                     opts.PoolSize,
		MaxConcurrentDials:           opts.MaxConcurrentDials,
		PoolTimeout:                  opts.PoolTimeout,
		MinIdleConns:                 opts.MinIdleConns,
		MaxIdleConns:                 opts.MaxIdleConns,
		MaxActiveConns:               opts.MaxActiveConns,
		ConnMaxIdleTime:              opts.ConnMaxIdleTime,
		ConnMaxLifetime:              opts.ConnMaxLifetime,
		ConnMaxLifetimeJitter:        opts.ConnMaxLifetimeJitter,
		TLSConfig:                    opts.TLSConfig,
		DisableIndentity:             opts.DisableIndentity,
		DisableIdentity:              opts.DisableIdentity,
		IdentitySuffix:               opts.IdentitySuffix,
		FailingTimeoutSeconds:        opts.FailingTimeoutSeconds,
		UnstableResp3:                opts.UnstableResp3,
		PushNotificationProcessor:    opts.PushNotificationProcessor,
		IsClusterMode:                clusterMode,
		MaintNotificationsConfig:     opts.MaintNotificationsConfig,
	}
}
