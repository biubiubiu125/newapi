package model

// jsonScanBytes normalizes JSON values returned by database drivers. PostgreSQL
// simple protocol commonly returns JSON columns as string, while SQLite and
// MySQL often return []byte.
func jsonScanBytes(value interface{}) []byte {
	switch typedValue := value.(type) {
	case []byte:
		return typedValue
	case string:
		return []byte(typedValue)
	default:
		return nil
	}
}
