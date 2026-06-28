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

func TestMigrateWithNoModelsIsNoop(t *testing.T) {
	// With no registered models, Open + Migrate must succeed and create nothing.
	s := openTemp(t)
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate (no models): %v", err)
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
