package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/mcp"
)

const (
	defaultCustomerServiceMCPURL = "http://customer-service-mcp.internal:3000/mcp"
	customerServiceMCPTimeout    = 45 * time.Second
)

type customerServiceMCPConfig struct {
	URL            string
	BearerToken    string
	CFAccessID     string
	CFAccessSecret string
	CFAccessJWT    string
}

func loadCustomerServiceMCPConfig() customerServiceMCPConfig {
	url := strings.TrimSpace(os.Getenv("CUSTOMER_SERVICE_MCP_URL"))
	if url == "" {
		url = defaultCustomerServiceMCPURL
	}
	return customerServiceMCPConfig{
		URL:            strings.TrimRight(url, "/"),
		BearerToken:    strings.TrimSpace(os.Getenv("CUSTOMER_SERVICE_MCP_AUTH_TOKEN")),
		CFAccessID:     strings.TrimSpace(os.Getenv("CUSTOMER_SERVICE_MCP_CF_ACCESS_CLIENT_ID")),
		CFAccessSecret: strings.TrimSpace(os.Getenv("CUSTOMER_SERVICE_MCP_CF_ACCESS_CLIENT_SECRET")),
		CFAccessJWT:    strings.TrimSpace(os.Getenv("CUSTOMER_SERVICE_MCP_CF_ACCESS_JWT")),
	}
}

type CustomerServiceMCPTool struct {
	name        string
	description string
	inputSchema map[string]any
	cfg         customerServiceMCPConfig
	client      *http.Client
}

func (t *CustomerServiceMCPTool) Name() string { return t.name }

func (t *CustomerServiceMCPTool) Description() string { return t.description }

func (t *CustomerServiceMCPTool) InputSchema() map[string]any { return t.inputSchema }

func (t *CustomerServiceMCPTool) Call(ctx context.Context, args map[string]any) (string, error) {
	cfg := t.cfg
	if cfg.URL == "" {
		cfg = loadCustomerServiceMCPConfig()
	}
	if cfg.BearerToken == "" {
		return "", fmt.Errorf("customer service MCP is missing CUSTOMER_SERVICE_MCP_AUTH_TOKEN")
	}
	client := t.client
	if client == nil {
		client = &http.Client{Timeout: customerServiceMCPTimeout}
	}

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      t.name,
			"arguments": args,
		},
	})
	if err != nil {
		return "", fmt.Errorf("customer service MCP marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("customer service MCP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	if cfg.CFAccessID != "" && cfg.CFAccessSecret != "" {
		req.Header.Set("CF-Access-Client-Id", cfg.CFAccessID)
		req.Header.Set("CF-Access-Client-Secret", cfg.CFAccessSecret)
	}
	if cfg.CFAccessJWT != "" {
		req.Header.Set("Cf-Access-Jwt-Assertion", cfg.CFAccessJWT)
	} else if cfg.CFAccessID == "" && cfg.CFAccessSecret == "" {
		req.Header.Set("Cf-Access-Jwt-Assertion", "internal")
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("customer service MCP call %s: %w", t.name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", fmt.Errorf("customer service MCP read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("customer service MCP call %s returned HTTP %d: %s", t.name, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	raw, err := decodeCustomerServiceMCPEnvelope(body, resp.Header.Get("Content-Type"))
	if err != nil {
		return "", err
	}
	var envelope struct {
		Result mcp.CallToolResult `json:"result"`
		Error  *mcp.RPCError      `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", fmt.Errorf("customer service MCP decode response: %w", err)
	}
	if envelope.Error != nil {
		return "", fmt.Errorf("customer service MCP call %s failed: %s", t.name, envelope.Error.Message)
	}
	return callToolResultText(envelope.Result), nil
}

func decodeCustomerServiceMCPEnvelope(body []byte, contentType string) ([]byte, error) {
	if !strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		return body, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 2<<20)
	var data strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("customer service MCP parse event stream: %w", err)
	}
	raw := strings.TrimSpace(data.String())
	if raw == "" {
		return nil, fmt.Errorf("customer service MCP returned an empty event stream")
	}
	return []byte(raw), nil
}

func callToolResultText(result mcp.CallToolResult) string {
	parts := make([]string, 0, len(result.Content))
	for _, block := range result.Content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
			continue
		}
		if raw, err := json.Marshal(block); err == nil && string(raw) != "{}" {
			parts = append(parts, string(raw))
		}
	}
	if len(parts) == 0 && len(result.StructuredContent) > 0 {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, result.StructuredContent, "", "  "); err == nil {
			return pretty.String()
		}
		return string(result.StructuredContent)
	}
	if len(parts) == 0 && result.IsError {
		return "customer service MCP returned an error without text"
	}
	return strings.Join(parts, "\n")
}

func customerServiceMCPTools() []Tool {
	cfg := loadCustomerServiceMCPConfig()
	defs := []struct {
		name        string
		description string
		schema      map[string]any
	}{
		{
			name:        "draft_reply",
			description: "Indsaet en kundebesked og faa et grounded svarudkast fra Firtals kundeservice-MCP.",
			schema: objectSchema([]string{"message"}, map[string]any{
				"message":     stringProp("Kundens besked som raak tekst."),
				"brand":       stringProp("helsebixen | jala | well | made4men | firtal."),
				"orderNumber": stringProp("Ordrenummer hvis kunden har oplyst det."),
				"channel":     stringProp("email | chat | phone."),
			}),
		},
		{
			name:        "refine_reply",
			description: "Rediger et eksisterende kundeservice-svarudkast via sessionId fra draft_reply.",
			schema: objectSchema([]string{"sessionId", "instruction"}, map[string]any{
				"sessionId":   stringProp("sessionId fra draft_reply."),
				"instruction": stringProp("Hvad der skal aendres i udkastet."),
			}),
		},
		{
			name:        "get_draft",
			description: "Hent det nuvaerende kundeservice-udkast, kilder og antal turns for en draft-session.",
			schema: objectSchema([]string{"sessionId"}, map[string]any{
				"sessionId": stringProp("sessionId fra draft_reply."),
			}),
		},
		{
			name:        "search_knowledge",
			description: "Soeg i Firtals interne FAQ/procesvidensbase.",
			schema: objectSchema([]string{"query"}, map[string]any{
				"query":    stringProp("Soegeord eller spoergsmaal."),
				"category": stringProp("Valgfri kategori."),
				"carrier":  stringProp("dao | gls | postnord | instabox | burd | dhl | helthjem."),
				"brand":    stringProp("Valgfrit brand."),
				"limit":    numberProp("Maks antal resultater."),
			}),
		},
		{
			name:        "search_past_answers",
			description: "Soeg i tidligere menneske-ratede kundeservice-svar.",
			schema: objectSchema([]string{"query"}, map[string]any{
				"query": stringProp("Soegeord eller spoergsmaal."),
				"limit": numberProp("Maks antal resultater."),
			}),
		},
		{
			name:        "lookup_order",
			description: "Slaa en ordre op i Selveo via ordrenummer.",
			schema: objectSchema([]string{"orderNumber"}, map[string]any{
				"orderNumber": stringProp("Ordrenummeret."),
			}),
		},
		{
			name:        "get_tracking",
			description: "Hent leveringsstatus via trackingnummer eller ordrenummer.",
			schema: objectSchema([]string{}, map[string]any{
				"trackingNumber": stringProp("Trackingnummer."),
				"orderNumber":    stringProp("Ordrenummer."),
			}),
		},
		{
			name:        "list_templates",
			description: "Vis kendte Dixa-templates med id, navn, type, brand og tekstuddrag.",
			schema: objectSchema([]string{}, map[string]any{
				"brand":  stringProp("helsebixen | jala | well | made4men."),
				"type":   stringProp("EmailAutoReply | QuickResponse."),
				"intent": stringProp("Filtrer paa intent, fx retur."),
				"limit":  numberProp("Maks antal resultater."),
			}),
		},
		{
			name:        "get_template",
			description: "Hent fuld tekst og subject for en Dixa-template via id.",
			schema: objectSchema([]string{"id"}, map[string]any{
				"id": stringProp("Dixa template UUID."),
			}),
		},
		{
			name:        "list_categories",
			description: "Vis Firtals intent-kategorier grupperet i familier.",
			schema:      objectSchema([]string{}, map[string]any{}),
		},
		// Dixa realtime tools (added TECH-3020)
		{
			name:        "dixa_get_conversation",
			description: "Hent Dixa-samtalemetadata: status, kanal, assignee og timestamps.",
			schema: objectSchema([]string{"conversationId"}, map[string]any{
				"conversationId": stringProp("Dixa samtale-ID."),
			}),
		},
		{
			name:        "dixa_list_messages",
			description: "Hent alle beskeder (ind- og udgående + noter) i en Dixa-samtale.",
			schema: objectSchema([]string{"conversationId"}, map[string]any{
				"conversationId": stringProp("Dixa samtale-ID."),
				"limit":          numberProp("Maks antal beskeder (standard 50)."),
			}),
		},
		{
			name:        "send_customer_message",
			description: "Send et svar direkte til kunden i Dixa. Kræver eksplicit godkendelse — brug kun efter menneskelig bekræftelse.",
			schema: objectSchema([]string{"conversationId", "message"}, map[string]any{
				"conversationId": stringProp("Dixa samtale-ID."),
				"message":        stringProp("Beskedtekst der sendes til kunden."),
			}),
		},
		// Selveo order and product tools (added TECH-3020)
		{
			name:        "get_customer_orders_by_email",
			description: "Hent ordrehistorik for en kunde baseret på billing-email (ikke kræver ordrenummer).",
			schema: objectSchema([]string{"email"}, map[string]any{
				"email": stringProp("Kundens email-adresse."),
				"limit": numberProp("Maks antal ordrer."),
			}),
		},
		{
			name:        "search_products",
			description: "Søg efter Selveo-produkter på navn.",
			schema: objectSchema([]string{"searchTerm"}, map[string]any{
				"searchTerm": stringProp("Søgeord (produktnavn)."),
				"limit":      numberProp("Maks antal resultater (standard 10)."),
			}),
		},
		{
			name:        "get_product_stock_level",
			description: "Hent lagerstatus per lager for et Selveo-produkt via SSIN.",
			schema: objectSchema([]string{"productSsin"}, map[string]any{
				"productSsin": stringProp("Selveo SSIN (produkt-ID)."),
			}),
		},
		{
			name:        "get_product_details",
			description: "Hent fuld produktinfo fra Selveo: pris, mål, kategorier, leverandør og lagerstatus.",
			schema: objectSchema([]string{"productSsin"}, map[string]any{
				"productSsin": stringProp("Selveo SSIN (produkt-ID)."),
			}),
		},
		{
			name:        "get_order_confirmation",
			description: "Hent ordrebekræftelsesmail fra intern API via increment-ID.",
			schema: objectSchema([]string{"incrementId"}, map[string]any{
				"incrementId": stringProp("Ordrenummer / increment-ID."),
			}),
		},
	}

	tools := make([]Tool, 0, len(defs))
	for _, def := range defs {
		tools = append(tools, &CustomerServiceMCPTool{
			name:        def.name,
			description: def.description,
			inputSchema: def.schema,
			cfg:         cfg,
		})
	}
	return tools
}

func customerServiceMCPToolMeta() []ToolMeta {
	tools := customerServiceMCPTools()
	meta := make([]ToolMeta, 0, len(tools))
	for _, tool := range tools {
		meta = append(meta, ToolMeta{
			Name:        tool.Name(),
			Description: tool.Description(),
			Status:      ToolStatusNewlyImplemented,
		})
	}
	return meta
}

func objectSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{
		"type":       "object",
		"required":   required,
		"properties": properties,
	}
}

func stringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func numberProp(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}
