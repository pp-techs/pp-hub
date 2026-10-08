package store

// DB is the connection or transaction these queries run on, for code that
// must write rows of its own in the same transaction (extension hooks).
func (q *Queries) DB() DBTX { return q.db }
