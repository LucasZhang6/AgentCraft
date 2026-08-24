package sqliteutil

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const DriverName = "sqlite"

func Open(path string, foreignKeys bool) (*sql.DB, error) {
	database, err := sql.Open(DriverName, path)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	pragmas := []string{"PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL"}
	if foreignKeys {
		pragmas = append(pragmas, "PRAGMA foreign_keys=ON")
	}
	for _, pragma := range pragmas {
		if _, err := database.Exec(pragma); err != nil {
			database.Close()
			return nil, fmt.Errorf("configure sqlite: %w", err)
		}
	}
	return database, nil
}
