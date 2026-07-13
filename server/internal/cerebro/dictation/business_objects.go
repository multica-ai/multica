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

const envBusinessObjectsBQProject = "CEREBRO_DICTATION_GLOSSARY_BQ_PROJECT"

const (
	businessObjectsTTL      = time.Hour
	maxBusinessObjectTerms  = 200
	businessObjectsLocation = "EU"
	businessObjectsTimeout  = 10 * time.Second
)

var businessObjectsCache struct {
	sync.Mutex
	value   string
	expires time.Time
}

// businessObjectGlossary reads brand, category and supplier names from the
// shared BigQuery business-object models. Failures are cached and ignored so a
// data source can never block dictation.
func businessObjectGlossary(ctx context.Context) string {
	project := strings.TrimSpace(os.Getenv(envBusinessObjectsBQProject))
	if project == "" {
		return ""
	}
	businessObjectsCache.Lock()
	defer businessObjectsCache.Unlock()
	if time.Now().Before(businessObjectsCache.expires) {
		return businessObjectsCache.value
	}
	businessObjectsCache.value = fetchBusinessObjects(ctx, project)
	businessObjectsCache.expires = time.Now().Add(businessObjectsTTL)
	return businessObjectsCache.value
}

func fetchBusinessObjects(ctx context.Context, project string) string {
	ctx, cancel := context.WithTimeout(ctx, businessObjectsTimeout)
	defer cancel()
	client, err := bigquery.NewClient(ctx, project)
	if err != nil {
		slog.Warn("dictation business objects: client failed", "error", err)
		return ""
	}
	defer client.Close()

	query := client.Query(fmt.Sprintf(`
SELECT name FROM (
  SELECT brand_dimensions.brand_name AS name FROM `+"`%[1]s.business_objects.object_brand`"+`
  UNION DISTINCT
  SELECT category_dimensions.category_name FROM `+"`%[1]s.business_objects.object_category`"+`
  UNION DISTINCT
  SELECT supplier_dimensions.supplier_name FROM `+"`%[1]s.business_objects.object_supplier`"+`
)
WHERE name IS NOT NULL AND name != ''
LIMIT @max`, project))
	query.Location = businessObjectsLocation
	query.Parameters = []bigquery.QueryParameter{{Name: "max", Value: maxBusinessObjectTerms}}
	rows, err := query.Read(ctx)
	if err != nil {
		slog.Warn("dictation business objects: query failed", "error", err)
		return ""
	}

	terms := make([]string, 0, maxBusinessObjectTerms)
	seen := map[string]struct{}{}
	for {
		var row struct {
			Name string `bigquery:"name"`
		}
		if err := rows.Next(&row); err == iterator.Done {
			break
		} else if err != nil {
			slog.Warn("dictation business objects: row failed", "error", err)
			break
		}
		term := strings.TrimSpace(row.Name)
		key := strings.ToLower(term)
		if term == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		terms = append(terms, term)
	}
	return strings.Join(terms, ", ")
}
