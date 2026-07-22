package store

import (
	"path/filepath"
	"testing"
)

// openTemp opens a fresh on-disk database in the test's temp dir.
func openTemp(t *testing.T) *Store {
	t.Helper()
	dsn := FileDSN(filepath.Join(t.TempDir(), "test.db"))
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenAppliesPragmas(t *testing.T) {
	s := openTemp(t)

	var journal string
	if err := s.DB.Raw("PRAGMA journal_mode").Scan(&journal).Error; err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}

	var fk int
	if err := s.DB.Raw("PRAGMA foreign_keys").Scan(&fk).Error; err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}

	var busy int
	if err := s.DB.Raw("PRAGMA busy_timeout").Scan(&busy).Error; err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busy != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", busy)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	// Open already migrated once; running it again must be a safe no-op.
	s := openTemp(t)
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate (second run): %v", err)
	}
}

func TestMigrateDeploymentInputsPrimaryKeyIncludesScope(t *testing.T) {
	s := openTemp(t)
	if err := s.DB.Exec("DROP TABLE deployment_inputs").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Exec(`CREATE TABLE deployment_inputs (
		deployment_id integer NOT NULL,
		key text NOT NULL,
		scope text NOT NULL DEFAULT "runtime",
		digest text NOT NULL,
		created_at datetime,
		PRIMARY KEY (deployment_id, key)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Exec("INSERT INTO deployment_inputs (deployment_id, key, scope, digest) VALUES (1, 'SHARED', 'runtime', 'old-digest')").Error; err != nil {
		t.Fatal(err)
	}

	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	legacy, err := s.ListDeploymentInputs(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy) != 1 || legacy[0].Scope != "runtime" || legacy[0].Digest != "old-digest" {
		t.Fatalf("legacy input was not preserved: %+v", legacy)
	}
	if err := s.ReplaceDeploymentInputs(1, []DeploymentInput{
		{Key: "SHARED", Scope: "runtime", Digest: "runtime-digest"},
		{Key: "SHARED", Scope: "build", Digest: "build-digest"},
	}); err != nil {
		t.Fatalf("ReplaceDeploymentInputs: %v", err)
	}
	inputs, err := s.ListDeploymentInputs(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 || inputs[0].Scope != "build" || inputs[1].Scope != "runtime" {
		t.Fatalf("unexpected migrated inputs: %+v", inputs)
	}
}

// migrationProbe is a throwaway model used only to exercise the AutoMigrate
// path; it is not part of the real schema.
type migrationProbe struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func TestAutoMigrateCreatesTableAndRoundTrips(t *testing.T) {
	s := openTemp(t)

	if err := s.DB.AutoMigrate(&migrationProbe{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	if !s.DB.Migrator().HasTable(&migrationProbe{}) {
		t.Fatal("expected migration_probes table to exist after AutoMigrate")
	}

	if err := s.DB.Create(&migrationProbe{Name: "draft"}).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}

	var got migrationProbe
	if err := s.DB.First(&got).Error; err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.Name != "draft" {
		t.Errorf("Name = %q, want draft", got.Name)
	}
}

func TestMemoryDSNOpens(t *testing.T) {
	s, err := Open(MemoryDSN())
	if err != nil {
		t.Fatalf("Open in-memory: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
}
