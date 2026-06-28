// Package store owns Draft's local SQLite database: connection, pragmas, and
// schema migration. It uses GORM over the pure-Go glebarez/modernc driver, so
// the build stays CGO-free.
//
// During development the schema is applied with GORM's AutoMigrate: define a
// model struct, register it in registeredModels, and the table is created or
// extended on the next Open — no migration files. AutoMigrate is additive only
// (it won't drop/rename/retype columns); when the schema stabilizes and real
// user data is in play, we swap this mechanism for Atlas-generated versioned
// migrations. The model definitions won't change when we do.
//
// No data models exist yet — registeredModels is intentionally empty until the
// schema takes shape.
package store

import (
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// registeredModels lists every model AutoMigrate manages. Add new model structs
// here as the schema is designed.
var registeredModels = []any{}

// Store wraps the GORM handle to the local database.
type Store struct {
	DB *gorm.DB
}

// Open connects to the SQLite database at dsn and applies the schema. Build the
// dsn with FileDSN (or MemoryDSN in tests) so the standard pragmas are set.
func Open(dsn string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	s := &Store{DB: db}
	if err := s.Migrate(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// Migrate applies the registered schema. Safe to call repeatedly.
func (s *Store) Migrate() error {
	if len(registeredModels) == 0 {
		return nil
	}
	return s.DB.AutoMigrate(registeredModels...)
}

// Close releases the underlying database connection.
func (s *Store) Close() error {
	sqlDB, err := s.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// pragmas are appended to every DSN: WAL for concurrent readers + a single
// writer, a busy timeout so concurrent goroutines wait rather than error, and
// enforced foreign keys.
const pragmas = "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"

// FileDSN builds a DSN for an on-disk database at path.
func FileDSN(path string) string {
	return path + pragmas
}

// MemoryDSN builds a DSN for a private in-memory database (handy for tests).
func MemoryDSN() string {
	return "file::memory:" + pragmas
}

// DefaultPath returns the dev-time database location under the user config dir
// (e.g. %AppData%\Draft\draft.db on Windows), creating the directory if needed.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "Draft")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "draft.db"), nil
}
