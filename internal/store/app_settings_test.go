package store

import "testing"

func TestAppSettingsDefaultsAndSet(t *testing.T) {
	s := openTemp(t)

	all, err := s.ListAppSettings()
	if err != nil {
		t.Fatalf("ListAppSettings: %v", err)
	}
	if all[AppSettingCompactSidebar] != "false" {
		t.Errorf("compact_sidebar default = %q", all[AppSettingCompactSidebar])
	}
	if all[AppSettingLocalDomainPreference] != LocalDomainPrefAuto {
		t.Errorf("local_domain_preference default = %q", all[AppSettingLocalDomainPreference])
	}

	if err := s.SetAppSetting(AppSettingCompactSidebar, "true"); err != nil {
		t.Fatalf("SetAppSetting: %v", err)
	}
	if err := s.SetAppSetting(AppSettingLocalDomainPreference, LocalDomainPrefLocalhost); err != nil {
		t.Fatalf("SetAppSetting domain: %v", err)
	}

	got, err := s.GetAppSetting(AppSettingCompactSidebar)
	if err != nil || got != "true" {
		t.Fatalf("GetAppSetting compact = %q, %v", got, err)
	}
	got, err = s.GetAppSetting(AppSettingLocalDomainPreference)
	if err != nil || got != LocalDomainPrefLocalhost {
		t.Fatalf("GetAppSetting domain = %q, %v", got, err)
	}

	// Invalid preference falls back to auto.
	if err := s.SetAppSetting(AppSettingLocalDomainPreference, "nope"); err != nil {
		t.Fatalf("SetAppSetting invalid: %v", err)
	}
	got, _ = s.GetAppSetting(AppSettingLocalDomainPreference)
	if got != LocalDomainPrefAuto {
		t.Errorf("invalid preference stored as %q, want auto", got)
	}
}

func TestSetDefaultEnvironment(t *testing.T) {
	s := openTemp(t)
	project, err := s.CreateProject("p", "/tmp/p", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	mainEnv, err := s.GetDefaultEnvironment(project.ID)
	if err != nil {
		t.Fatalf("GetDefaultEnvironment: %v", err)
	}
	staging, err := s.CreateEnvironment(project.ID, "Staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	if err := s.SetDefaultEnvironment(staging.ID); err != nil {
		t.Fatalf("SetDefaultEnvironment: %v", err)
	}
	def, err := s.GetDefaultEnvironment(project.ID)
	if err != nil {
		t.Fatalf("GetDefaultEnvironment after: %v", err)
	}
	if def.ID != staging.ID {
		t.Errorf("default = %d, want staging %d", def.ID, staging.ID)
	}
	mainReload, err := s.GetEnvironment(mainEnv.ID)
	if err != nil {
		t.Fatalf("GetEnvironment main: %v", err)
	}
	if mainReload.IsDefault {
		t.Error("expected old default to clear IsDefault")
	}

	// Idempotent.
	if err := s.SetDefaultEnvironment(staging.ID); err != nil {
		t.Fatalf("SetDefaultEnvironment again: %v", err)
	}
}
