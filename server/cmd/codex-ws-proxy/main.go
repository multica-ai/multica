package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type direction string

const (
	dirClientToUpstream direction = "TUI -> app-server"
	dirUpstreamToClient direction = "app-server -> TUI"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9877", "address for Codex TUI to connect to")
	upstream := flag.String("upstream", "ws://127.0.0.1:9876", "upstream Codex app-server websocket URL")
	maxBody := flag.Int("max-body", 24*1024, "maximum logged message body bytes; 0 disables body logging")
	flag.Parse()

	if strings.TrimSpace(*upstream) == "" {
		log.Fatal("-upstream is required")
	}

	proxy := &proxyServer{
		upstreamURL: strings.TrimSpace(*upstream),
		maxBody:     *maxBody,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", proxy.handle)

	log.Printf("codex ws proxy listening on ws://%s", *listen)
	log.Printf("forwarding to %s", proxy.upstreamURL)
	log.Printf("connect Codex TUI with: codex --remote ws://%s", *listen)

	if err := http.ListenAndServe(*listen, mux); err != nil {
		log.Fatal(err)
	}
}

type proxyServer struct {
	upstreamURL string
	maxBody     int
	upgrader    websocket.Upgrader
}

func (p *proxyServer) handle(w http.ResponseWriter, r *http.Request) {
	requestedProtocols := websocket.Subprotocols(r)
	upgrader := p.upgrader
	upgrader.Subprotocols = requestedProtocols

	client, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade client: %v", err)
		return
	}
	defer client.Close()

	upstreamHeader := http.Header{}
	if auth := r.Header.Get("Authorization"); auth != "" {
		upstreamHeader.Set("Authorization", auth)
	}
	dialer := websocket.Dialer{Subprotocols: requestedProtocols}
	upstream, _, err := dialer.Dial(p.upstreamURL, upstreamHeader)
	if err != nil {
		log.Printf("dial upstream %s: %v", p.upstreamURL, err)
		_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, err.Error()), time.Now().Add(time.Second))
		return
	}
	defer upstream.Close()

	id := time.Now().Format("150405.000")
	log.Printf("[%s] connected client=%s path=%s upstream=%s", id, r.RemoteAddr, r.URL.RequestURI(), p.upstreamURL)

	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = client.Close()
			_ = upstream.Close()
			log.Printf("[%s] closed", id)
		})
	}

	var clientWriteMu sync.Mutex
	var upstreamWriteMu sync.Mutex
	done := make(chan struct{}, 2)

	go func() {
		p.copyMessages(id, client, upstream, &upstreamWriteMu, dirClientToUpstream)
		closeBoth()
		done <- struct{}{}
	}()
	go func() {
		p.copyMessages(id, upstream, client, &clientWriteMu, dirUpstreamToClient)
		closeBoth()
		done <- struct{}{}
	}()

	<-done
}

func (p *proxyServer) copyMessages(id string, src, dst *websocket.Conn, dstWriteMu *sync.Mutex, dir direction) {
	for {
		messageType, payload, err := src.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("[%s] %s read: %v", id, dir, err)
			}
			return
		}

		p.logMessage(id, dir, messageType, payload)

		dstWriteMu.Lock()
		err = dst.WriteMessage(messageType, payload)
		dstWriteMu.Unlock()
		if err != nil {
			log.Printf("[%s] %s write: %v", id, dir, err)
			return
		}
	}
}

func (p *proxyServer) logMessage(id string, dir direction, messageType int, payload []byte) {
	now := time.Now().Format("15:04:05.000")
	typeLabel := wsMessageType(messageType)
	summary := summarizeJSON(payload)
	if summary == "" {
		summary = fmt.Sprintf("%d bytes", len(payload))
	}

	fmt.Fprintf(os.Stdout, "\n[%s] [%s] %s %s\n", now, id, dir, summary)
	fmt.Fprintf(os.Stdout, "type=%s bytes=%d\n", typeLabel, len(payload))

	if p.maxBody == 0 {
		return
	}

	body := prettyJSON(payload)
	truncated := false
	if len(body) > p.maxBody {
		body = body[:p.maxBody]
		truncated = true
	}
	fmt.Fprintln(os.Stdout, body)
	if truncated {
		fmt.Fprintf(os.Stdout, "... truncated at %d bytes; increase -max-body to see more\n", p.maxBody)
	}
}

func wsMessageType(t int) string {
	switch t {
	case websocket.TextMessage:
		return "text"
	case websocket.BinaryMessage:
		return "binary"
	case websocket.CloseMessage:
		return "close"
	case websocket.PingMessage:
		return "ping"
	case websocket.PongMessage:
		return "pong"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

func prettyJSON(payload []byte) string {
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		return string(payload)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return string(payload)
	}
	return strings.TrimRight(buf.String(), "\n")
}

func summarizeJSON(payload []byte) string {
	var v map[string]any
	if err := json.Unmarshal(payload, &v); err != nil {
		return ""
	}

	parts := make([]string, 0, 8)
	if method, ok := stringField(v, "method"); ok {
		parts = append(parts, "method="+method)
	}
	if id, ok := v["id"]; ok {
		parts = append(parts, fmt.Sprintf("id=%v", id))
	}
	if result, ok := v["result"].(map[string]any); ok {
		if thread := nestedMap(result, "thread"); thread != nil {
			if threadID, ok := stringField(thread, "id"); ok {
				parts = append(parts, "result.thread.id="+threadID)
			}
		}
	}
	if params, ok := v["params"].(map[string]any); ok {
		if threadID, ok := stringField(params, "threadId"); ok {
			parts = append(parts, "threadId="+threadID)
		}
		if turnID, ok := stringField(params, "turnId"); ok {
			parts = append(parts, "turnId="+turnID)
		}
		if msg := nestedMap(params, "msg"); msg != nil {
			if typ, ok := stringField(msg, "type"); ok {
				parts = append(parts, "msg.type="+typ)
			}
			if turnID, ok := stringField(msg, "turn_id"); ok {
				parts = append(parts, "msg.turn_id="+turnID)
			}
		}
		if item := nestedMap(params, "item"); item != nil {
			if typ, ok := stringField(item, "type"); ok {
				parts = append(parts, "item.type="+typ)
			}
			if status, ok := stringField(item, "status"); ok {
				parts = append(parts, "item.status="+status)
			}
			if itemID, ok := stringField(item, "id"); ok {
				parts = append(parts, "item.id="+itemID)
			}
		}
		if turn := nestedMap(params, "turn"); turn != nil {
			if turnID, ok := stringField(turn, "id"); ok {
				parts = append(parts, "turn.id="+turnID)
			}
			if status, ok := stringField(turn, "status"); ok {
				parts = append(parts, "turn.status="+status)
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func nestedMap(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

func stringField(m map[string]any, key string) (string, bool) {
	v, ok := m[key].(string)
	return v, ok && v != ""
}
