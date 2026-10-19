package config

import (
	"strings"
	"testing"
	"time"
)

var testSecret = strings.Repeat("k", 32)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error when DATABASE_URL is empty")
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("AUTH_SECRET", testSecret)
	t.Setenv("API_ADDR", "")
	t.Setenv("API_SHUTDOWN_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want \":8080\"", cfg.Addr)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 10s", cfg.ShutdownTimeout)
	}
}

func TestLoadShutdownTimeout(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
		fails bool
	}{
		{name: "seconds", value: "30", want: 30 * time.Second},
		{name: "not a number", value: "soon", fails: true},
		{name: "zero", value: "0", fails: true},
		{name: "negative", value: "-5", fails: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
			t.Setenv("AUTH_SECRET", testSecret)
			t.Setenv("API_SHUTDOWN_TIMEOUT", tt.value)

			cfg, err := Load()
			if tt.fails {
				if err == nil {
					t.Fatalf("expected an error for %q", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.ShutdownTimeout != tt.want {
				t.Errorf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, tt.want)
			}
		})
	}
}

func TestLoadRequiresStrongAuthSecret(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		fails  bool
	}{
		{name: "missing", secret: "", fails: true},
		{name: "too short", secret: strings.Repeat("k", 31), fails: true},
		{name: "minimum length", secret: strings.Repeat("k", 32)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
			t.Setenv("AUTH_SECRET", tt.secret)

			_, err := Load()
			if tt.fails && err == nil {
				t.Error("expected an error for a weak secret")
			}
			if !tt.fails && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
