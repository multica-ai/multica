package cerebrodb

// Database exposes the database adapter already held by generated Queries.
// Cerebro modules that need both generated methods and a small custom query
// can therefore keep one connected store instead of silently dropping the
// custom-query part of their behavior.
func (q *Queries) Database() DBTX {
	if q == nil {
		return nil
	}
	return q.db
}
