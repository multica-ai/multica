package database

import (
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestNewRedisOptions(t *testing.T) {
	tests := []struct {
		name        string
		cfg         RedisConfig
		wantAddrs   []string
		wantUser    string
		wantPass    string
		wantDB      int
		wantTLS     bool
		wantSNI     string
		wantCluster bool
		wantPool    int
	}{
		{
			name:      "standalone",
			cfg:       RedisConfig{URL: "redis://app:secret@127.0.0.1:6380/2?pool_size=17"},
			wantAddrs: []string{"127.0.0.1:6380"},
			wantUser:  "app",
			wantPass:  "secret",
			wantDB:    2,
			wantPool:  17,
		},
		{
			name:        "elasticache serverless cluster with TLS",
			cfg:         RedisConfig{URL: "rediss://:secret@cache.example.com:6379/0", ClusterMode: true},
			wantAddrs:   []string{"cache.example.com:6379"},
			wantPass:    "secret",
			wantTLS:     true,
			wantSNI:     "cache.example.com",
			wantCluster: true,
		},
		{
			name:        "native multi-node cluster",
			cfg:         RedisConfig{URL: "redis://:secret@10.0.0.1:6379,10.0.0.2:6380,cache.internal/0", ClusterMode: true},
			wantAddrs:   []string{"10.0.0.1:6379", "10.0.0.2:6380", "cache.internal:6379"},
			wantPass:    "secret",
			wantCluster: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := NewRedisOptions(tt.cfg)
			if err != nil {
				t.Fatalf("NewRedisOptions() error = %v", err)
			}
			if len(opts.Addrs) != len(tt.wantAddrs) {
				t.Fatalf("Addrs = %v, want %v", opts.Addrs, tt.wantAddrs)
			}
			for i := range tt.wantAddrs {
				if opts.Addrs[i] != tt.wantAddrs[i] {
					t.Fatalf("Addrs = %v, want %v", opts.Addrs, tt.wantAddrs)
				}
			}
			if opts.Username != tt.wantUser || opts.Password != tt.wantPass {
				t.Fatalf("credentials = (%q, %q), want (%q, %q)", opts.Username, opts.Password, tt.wantUser, tt.wantPass)
			}
			if opts.DB != tt.wantDB {
				t.Fatalf("DB = %d, want %d", opts.DB, tt.wantDB)
			}
			if (opts.TLSConfig != nil) != tt.wantTLS {
				t.Fatalf("TLS configured = %v, want %v", opts.TLSConfig != nil, tt.wantTLS)
			}
			if tt.wantTLS && opts.TLSConfig.ServerName != tt.wantSNI {
				t.Fatalf("TLS ServerName = %q, want %q", opts.TLSConfig.ServerName, tt.wantSNI)
			}
			if opts.IsClusterMode != tt.wantCluster {
				t.Fatalf("IsClusterMode = %v, want %v", opts.IsClusterMode, tt.wantCluster)
			}
			if opts.PoolSize != tt.wantPool {
				t.Fatalf("PoolSize = %d, want %d", opts.PoolSize, tt.wantPool)
			}
			client := redis.NewUniversalClient(opts)
			defer client.Close()
			if tt.wantCluster {
				if _, ok := client.(*redis.ClusterClient); !ok {
					t.Fatalf("client type = %T, want *redis.ClusterClient", client)
				}
			} else if _, ok := client.(*redis.Client); !ok {
				t.Fatalf("client type = %T, want *redis.Client", client)
			}
		})
	}
}

func TestNewRedisClientPingsConfiguredRedis(t *testing.T) {
	redisURL := os.Getenv("REDIS_TEST_URL")
	if redisURL == "" {
		t.Skip("REDIS_TEST_URL is not configured")
	}
	client, err := NewRedisClient(RedisConfig{URL: redisURL})
	if err != nil {
		t.Fatalf("NewRedisClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
}

func TestNewRedisOptionsRejectsInvalidClusterConfiguration(t *testing.T) {
	tests := []struct {
		name string
		cfg  RedisConfig
	}{
		{
			name: "multiple addresses without cluster mode",
			cfg:  RedisConfig{URL: "redis://node-a:6379,node-b:6379/0"},
		},
		{
			name: "nonzero database in cluster mode",
			cfg:  RedisConfig{URL: "redis://node-a:6379/1", ClusterMode: true},
		},
		{
			name: "empty cluster address",
			cfg:  RedisConfig{URL: "redis://node-a:6379,,node-b:6379/0", ClusterMode: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewRedisOptions(tt.cfg); err == nil {
				t.Fatal("NewRedisOptions() error = nil")
			}
		})
	}
}
