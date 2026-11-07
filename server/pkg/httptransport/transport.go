package httptransport

import (
	"net/http"
	"time"
)

func CloneDefault() http.RoundTripper {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		cloned := transport.Clone()
		if cloned.HTTP2 == nil {
			cloned.HTTP2 = &http.HTTP2Config{}
		}
		cloned.HTTP2.SendPingTimeout = 15 * time.Second
		cloned.HTTP2.PingTimeout = 10 * time.Second
		return cloned
	}
	return http.DefaultTransport
}
