package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/imkarma/hive/internal/store"
)

const hiveDirName = ".hive"

// hivePath returns the path to a file inside .hive/.
func hivePath(parts ...string) string {
	elems := append([]string{hiveDirName}, parts...)
	return filepath.Join(elems...)
}

// mustStore opens the store, returning an error if hive is not initialized.
func mustStore() (*store.Store, error) {
	dbPath := hivePath("hive.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("hive not initialized. Run: hive init")
	}
	return openStore(dbPath)
}

// openStore opens or creates the SQLite store at the given path.
func openStore(dbPath string) (*store.Store, error) {
	return store.New(dbPath)
}
