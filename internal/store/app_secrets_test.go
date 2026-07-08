package store

import "testing"

func TestAppSecretsCRUD(t *testing.T) {
	s := openTemp(t)

	if err := s.SetAppSecret("OPENAI_API_KEY", "sk-test", "OpenAI"); err != nil {
		t.Fatal(err)
	}
	secret, err := s.GetAppSecret("OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "sk-test" || secret.Description != "OpenAI" {
		t.Fatalf("unexpected secret: %+v", secret)
	}

	list, err := s.ListAppSecrets()
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}

	if err := s.DeleteAppSecret("OPENAI_API_KEY"); err != nil {
		t.Fatal(err)
	}
	exists, err := s.AppSecretExists("OPENAI_API_KEY")
	if err != nil || exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
}

