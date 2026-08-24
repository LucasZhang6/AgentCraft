package sqliteutil

import (
	"database/sql"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const DriverName = "sqlite"

func Open(path string, foreignKeys bool) (*sql.DB, error) {
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "journal_mode(WAL)")
	if foreignKeys {
		query.Add("_pragma", "foreign_keys(1)")
	}
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(path),
		RawQuery: query.Encode(),
	}).String()
	return sql.Open(DriverName, dsn)
}
