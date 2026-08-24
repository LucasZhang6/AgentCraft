package sqliteutil_test

import (
	"context"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/sqliteutil"
)

func TestOpenConfiguresPortableSQLite(t *testing.T) {
	database, err := sqliteutil.Open(t.TempDir()+"/portable database.db", true)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.ExecContext(context.Background(), `CREATE TABLE parent(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `CREATE TABLE child(parent_id INTEGER REFERENCES parent(id))`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `INSERT INTO child(parent_id) VALUES(99)`); err == nil {
		t.Fatal("foreign key enforcement is disabled")
	}
}
