package daemon

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	netroute "github.com/libp2p/go-netroute"
	"github.com/multica-ai/multica/server/internal/daemon/p2p"
)

// watch uses the same session authentication as registry reads. Every ready
// event causes a full read, so reconnects need no unbounded event history.
func (c *p2pRegistryClient) watch(ctx context.Context, changed func() error) error {
	ctx, cancel := c.requestContext(ctx)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.sessionToken(ctx, attempt > 0)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/events", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		streamClient := *c.http
		streamClient.Timeout = 25 * time.Second
		res, err := streamClient.Do(req)
		if err != nil {
			return err
		}
		if res.StatusCode == http.StatusUnauthorized {
			res.Body.Close()
			continue
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
			return fmt.Errorf("p2p: registry watch returned %d", res.StatusCode)
		}
		scanner := bufio.NewScanner(res.Body)
		scanner.Buffer(make([]byte, 1024), 4096)
		for scanner.Scan() {
			switch scanner.Text() {
			case "event: ready", "event: devices.changed":
				if err := changed(); err != nil {
					return err
				}
			}
		}
		return scanner.Err()
	}
	return errP2PUnauthorized
}

// networkFingerprint reads local kernel state only; it sends no probe packets.
// Include gateways and source addresses to notice VPN/default-route changes
// even when the Wi-Fi interface itself stays up. A tunnel that takes no default
// route, such as the one Xcode opens to a phone on a cable, changes nothing here.
func networkFingerprint() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	var parts []string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || p2p.IsTunnelInterface(iface) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", err
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip.IsGlobalUnicast() {
				parts = append(parts, iface.Name+":"+addr.String())
			}
		}
	}
	if router, err := netroute.New(); err == nil {
		for _, target := range []string{"1.1.1.1", "2606:4700:4700::1111"} {
			if iface, gateway, source, err := router.Route(net.ParseIP(target)); err == nil && iface != nil {
				parts = append(parts, target+":"+iface.Name+":"+gateway.String()+":"+source.String())
			}
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "|"), nil
}

func watchNetwork(ctx context.Context, ticks <-chan time.Time, snapshot func() (string, error), changed func()) {
	previous, err := snapshot()
	known := err == nil
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
		}
		next, err := snapshot()
		if err != nil {
			continue
		}
		if known && next != previous && ctx.Err() == nil {
			changed()
		}
		previous, known = next, true
	}
}

func (d *Daemon) p2pWatchLoop(ctx context.Context, registry *p2pRegistryClient, svc *p2p.Service, networks <-chan struct{}) {
	delay := 500 * time.Millisecond
	for ctx.Err() == nil {
		watchCtx, cancel := context.WithCancel(ctx)
		finished := make(chan error, 1)
		go func() {
			finished <- registry.watch(watchCtx, func() error {
				return svc.PeersChanged(watchCtx)
			})
		}()
		select {
		case <-ctx.Done():
			cancel()
			<-finished
			return
		case <-networks:
			cancel()
			<-finished
			delay = 500 * time.Millisecond
			continue
		case err := <-finished:
			cancel()
			if err == nil {
				delay = 500 * time.Millisecond
			} else {
				d.logger.Debug("p2p: registry watch interrupted", "error", err, "retry_in", delay)
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-networks:
			timer.Stop()
			delay = 500 * time.Millisecond
			continue
		case <-timer.C:
		}
		delay = min(delay*2, 5*time.Second)
	}
}
