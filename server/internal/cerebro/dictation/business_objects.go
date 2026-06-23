package dictation

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
)

// envBusinessObjectsBQProject gates the data-catalog glossary source. When
// unset, the dictation glossary uses only the workspace's own objects. The
// catalog lives in BigQuery (dataset `business_objects`, location EU).
const envBusinessObjectsBQProject = "CEREBRO_DICTATION_GLOSSARY_BQ_PROJECT"

const (
	businessObjectsTTL      = time.Hour
	maxBusinessObjectTerms  = 200
	businessObjectsLocation = "EU"
	businessObjectsTimeout  = 10 * time.Second
)

type businessObjectsCacheT struct {
	mu      sync.Mutex
	value   string
	expires time.Time
}

// Process-wide cache: the catalog changes rarely and a dictation must never pay
// a BigQuery round-trip on the hot path beyond the first call per TTL window.
var businessObjectsCache businessObjectsCacheT

// businessObjectGlossary returns a comma-separated glossary of Firtal business
// object names (brands, categories, suppliers) pulled from the data catalog in
// BigQuery — the same catalog meeting-notes enriches its prompt from. Gated on
// CEREBRO_DICTATION_GLOSSARY_BQ_PROJECT and cached for businessObjectsTTL.
// Best-effort: any failure returns "" so the glossary falls back to the
// workspace objects and a transcription is never blocked.
func businessObjectGlossary(ctx context.Context) string {
	project := strings.TrimSpace(os.Getenv(envBusinessObjectsBQProject))
	if project == "" {
		return ""
	}
	businessObjectsCache.mu.Lock()
	defer businessObjectsCache.mu.Unlock()
	if time.Now().Before(businessObjectsCache.expires) {
		return businessObjectsCache.value
	}
	value := fetchBusinessObjects(ctx, project)
	// Cache even an empty/failed result so a flaky catalog doesn't re-query on
	// every dictation; the TTL is the retry window.
	businessObjectsCache.value = value
	businessObjectsCache.expires = time.Now().Add(businessObjectsTTL)
	return value
}

func fetchBusinessObjects(ctx context.Context, project string) string {
	ctx, cancel := context.WithTimeout(ctx, businessObjectsTimeout)
	defer cancel()

	client, err := bigquery.NewClient(ctx, project)
	if err != nil {
		slog.Warn("dictation business-objects: bigquery client failed", "error", err)
		return ""
	}
	defer client.Close()

	// Brands, categories and suppliers are the bounded, high-value proper nouns
	// people dictate; product/customer objects are unbounded or PII and skipped.
	q := fmt.Sprintf(`
SELECT name FROM (
  SELECT brand_dimensions.brand_name AS name FROM `+"`%[1]s.business_objects.object_brand`"+`
  UNION DISTINCT
  SELECT category_dimensions.category_name FROM `+"`%[1]s.business_objects.object_category`"+`
  UNION DISTINCT
  SELECT supplier_dimensions.supplier_name FROM `+"`%[1]s.business_objects.object_supplier`"+`
)
WHERE name IS NOT NULL AND name != ''
LIMIT @max`, project)

	query := client.Query(q)
	query.Location = businessObjectsLocation
	query.Parameters = []bigquery.QueryParameter{{Name: "max", Value: maxBusinessObjectTerms}}

	it, err := query.Read(ctx)
	if err != nil {
		slog.Warn("dictation business-objects: query failed", "error", err)
		return ""
	}

	seen := map[string]struct{}{}
	terms := make([]string, 0, maxBusinessObjectTerms)
	for {
		var row struct {
			Name string `bigquery:"name"`
		}
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			slog.Warn("dictation business-objects: read row failed", "error", err)
			break
		}
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		terms = append(terms, name)
	}
	return strings.Join(terms, ", ")
}
