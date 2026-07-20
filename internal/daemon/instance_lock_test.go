package daemon

import (
	"testing"
)

func TestAcquireInstanceLockExcludesSecondHolder(t *testing.T) {
	configDir := t.TempDir()
	old := userConfigDir
	userConfigDir = func() (string, error) { return configDir, nil }
	t.Cleanup(func() { userConfigDir = old })

	release, err := acquireInstanceLock()
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer release()

	if _, err := acquireInstanceLock(); err == nil {
		t.Fatal("second lock should fail while first is held")
	}

	release()
	release2, err := acquireInstanceLock()
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	release2()
}
