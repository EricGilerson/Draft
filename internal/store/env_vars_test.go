package store

import "testing"

func TestEnvVarsSetListAndImportConflicts(t *testing.T) {
	s := openTemp(t)

	if err := s.SetEnvVar("n1", "FOO", "manual"); err != nil {
		t.Fatalf("SetEnvVar: %v", err)
	}
	result, err := s.ImportEnvVars("n1", "/tmp/.env", map[string]string{
		"FOO": "file",
		"BAR": "imported",
	})
	if err != nil {
		t.Fatalf("ImportEnvVars: %v", err)
	}
	if result.Imported != 1 || result.Skipped != 1 || len(result.Conflicts) != 1 {
		t.Fatalf("result = %+v, want one import and one conflict", result)
	}

	vars, err := s.ListEnvVars("n1")
	if err != nil {
		t.Fatalf("ListEnvVars: %v", err)
	}
	if len(vars) != 2 {
		t.Fatalf("vars = %+v, want 2", vars)
	}
	if vars[0].Key != "BAR" || vars[0].Value != "imported" || vars[0].Source != EnvSourceImported {
		t.Fatalf("BAR = %+v", vars[0])
	}
	if vars[1].Key != "FOO" || vars[1].Value != "manual" || vars[1].Source != EnvSourceManual {
		t.Fatalf("FOO = %+v", vars[1])
	}
}

func TestEnvImportUpdatesImportedValues(t *testing.T) {
	s := openTemp(t)

	result, err := s.ImportEnvVars("n1", "/tmp/.env", map[string]string{"FOO": "one"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 {
		t.Fatalf("first import = %+v", result)
	}

	result, err = s.ImportEnvVars("n1", "/tmp/.env", map[string]string{"FOO": "two"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 || result.Imported != 0 || len(result.Conflicts) != 0 {
		t.Fatalf("second import = %+v", result)
	}

	result, err = s.ImportEnvVars("n1", "/tmp/.env", map[string]string{"FOO": "two"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Unchanged != 1 || result.Skipped != 0 {
		t.Fatalf("third import = %+v, want unchanged", result)
	}
}
