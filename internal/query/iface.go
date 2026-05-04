package query

// QuerierWithLookup extends Querier with the per-cell lookup the
// /api/test/<tid>/run/<rid> endpoint needs. Kept off the base
// interface so a hosted ClickHouse impl can lazily wire this up.
type QuerierWithLookup interface {
	Querier
	LookupEntry(testID, runID string) (TestEntry, Run, bool, error)
}
