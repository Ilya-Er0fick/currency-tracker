package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnvFile(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

func TestLoadEnvFileSetsVariables(t *testing.T) {
	path := writeEnvFile(t, `
# комментарий
DATABASE_URL=postgres://user:pass@localhost:5432/db

API_ADDR = :9000
QUOTED="значение в кавычках"
export EXPORTED=yes
BROKEN LINE WITHOUT EQUALS
`)

	for _, key := range []string{"DATABASE_URL", "API_ADDR", "QUOTED", "EXPORTED"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile: %v", err)
	}

	tests := map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
		"API_ADDR":     ":9000",
		"QUOTED":       "значение в кавычках",
		"EXPORTED":     "yes",
	}
	for key, want := range tests {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestLoadEnvFileKeepsExistingEnvironment(t *testing.T) {
	path := writeEnvFile(t, "DATABASE_URL=from-file\n")
	t.Setenv("DATABASE_URL", "from-environment")

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile: %v", err)
	}

	// Окружение задаёт контейнер, и файл разработчика не должен его подменять.
	if got := os.Getenv("DATABASE_URL"); got != "from-environment" {
		t.Errorf("DATABASE_URL = %q, want the value from the environment", got)
	}
}

func TestLoadEnvFileIgnoresMissingFile(t *testing.T) {
	if err := LoadEnvFile(filepath.Join(t.TempDir(), "absent")); err != nil {
		t.Errorf("a missing file must not be an error, got %v", err)
	}
}
