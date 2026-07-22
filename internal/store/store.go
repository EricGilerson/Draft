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
	"log"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// registeredModels lists every model AutoMigrate manages. Add new model structs
// here as the schema is designed.
var registeredModels = []any{
	&Project{},
	&Environment{},
	&SandboxProjectSettings{},
	&SandboxProfile{},
	&Sandbox{},
	&SandboxLink{},
	&SandboxRepositorySource{},
	&SandboxTestRun{},
	&CanvasNode{},
	&Route{},
	&PortLease{},
	&NodeSetting{},
	&NodeSettingStaged{},
	&Deployment{},
	&DeploymentInput{},
	&EnvVar{},
	&EnvVarStaged{},
	&ProjectEnvVar{},
	&AppSecret{},
	&AppSetting{},
	&ServiceTemplate{},
}

// Store wraps the GORM handle to the local database.
type Store struct {
	DB *gorm.DB
}

// Open connects to the SQLite database at dsn and applies the schema. Build the
// dsn with FileDSN (or MemoryDSN in tests) so the standard pragmas are set.
func Open(dsn string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.New(log.New(os.Stderr, "\r\n", log.LstdFlags), logger.Config{
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
		}),
	})
	if err != nil {
		return nil, err
	}

	s := &Store{DB: db}
	if err := s.Migrate(); err != nil {
		_ = s.Close()
		return nil, err
	}
	if err := s.SeedBuiltins(); err != nil {
		// Seeding is best-effort: a failure here shouldn't make the whole store
		// unusable. The next successful Open will retry.
		log.Printf("store: seed built-in templates: %v", err)
	}
	return s, nil
}

// Migrate applies the registered schema. Safe to call repeatedly.
func (s *Store) Migrate() error {
	if err := s.migrateDeploymentInputsPrimaryKey(); err != nil {
		return err
	}
	if len(registeredModels) == 0 {
		return nil
	}
	return s.DB.AutoMigrate(registeredModels...)
}

// migrateDeploymentInputsPrimaryKey upgrades databases created before Scope
// was part of DeploymentInput's identity. A variable with scope "both" has one
// runtime and one build input, so the old (deployment_id, key) key rejected the
// second row. SQLite cannot alter a primary key in place, hence the table copy.
func (s *Store) migrateDeploymentInputsPrimaryKey() error {
	if !s.DB.Migrator().HasTable("deployment_inputs") {
		return nil
	}

	type tableColumn struct {
		Name string
		PK   int
	}
	var columns []tableColumn
	if err := s.DB.Raw("PRAGMA table_info(deployment_inputs)").Scan(&columns).Error; err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name == "scope" && column.PK > 0 {
			return nil
		}
	}

	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`CREATE TABLE deployment_inputs_new (
			deployment_id integer NOT NULL,
			key text NOT NULL,
			scope text NOT NULL DEFAULT "runtime",
			digest text NOT NULL,
			created_at datetime,
			PRIMARY KEY (deployment_id, key, scope)
		)`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO deployment_inputs_new (deployment_id, key, scope, digest, created_at)
			SELECT deployment_id, key, scope, digest, created_at FROM deployment_inputs`).Error; err != nil {
			return err
		}
		if err := tx.Exec("DROP TABLE deployment_inputs").Error; err != nil {
			return err
		}
		return tx.Exec("ALTER TABLE deployment_inputs_new RENAME TO deployment_inputs").Error
	})
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
