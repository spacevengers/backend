package testutil

import (
	"context"
	"os"
	"path/filepath"

	"firms-harmonizer/internal/db"
	"firms-harmonizer/internal/store"
)

// OpenStore creates a migrated temporary SQLite database. The returned cleanup
// removes the database directory.
func OpenStore() (*store.Store, func(), error) {
	dir, err := os.MkdirTemp("", "firms-test-*")
	if err != nil {
		return nil, nil, err
	}
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, nil, err
	}
	if err := db.Migrate(context.Background(), database); err != nil {
		database.Close()
		os.RemoveAll(dir)
		return nil, nil, err
	}
	repository := store.New(database)
	cleanup := func() {
		repository.Close()
		os.RemoveAll(dir)
	}
	return repository, cleanup, nil
}
