package config

import (
	"log/slog"
	"strings"
	"testing"
)

func env(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func TestLoad(t *testing.T) {
	const dsn = "postgres://user:pass@127.0.0.1:5432/db?sslmode=disable"

	t.Run("minimum dev config", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{"DATABASE_URL": dsn}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Env != EnvDev {
			t.Errorf("Env = %q, want dev", cfg.Env)
		}
		if cfg.HTTPAddr != ":8080" {
			t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
		}
		if cfg.LogLevel != slog.LevelInfo {
			t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
		}
	})

	t.Run("PORT is used when HTTP_ADDR is empty", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{"DATABASE_URL": dsn, "PORT": "9090"}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.HTTPAddr != ":9090" {
			t.Errorf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
		}
	})

	t.Run("HTTP_ADDR wins over PORT", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{"DATABASE_URL": dsn, "PORT": "9090", "HTTP_ADDR": "127.0.0.1:3000"}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.HTTPAddr != "127.0.0.1:3000" {
			t.Errorf("HTTPAddr = %q, want 127.0.0.1:3000", cfg.HTTPAddr)
		}
	})

	t.Run("errors are collected, not just the first", func(t *testing.T) {
		_, err := Load(env(map[string]string{"APP_ENV": "qa", "LOG_LEVEL": "loud"}))
		if err == nil {
			t.Fatal("Load: want error, got nil")
		}
		for _, want := range []string{"APP_ENV", "LOG_LEVEL", "DATABASE_URL"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %s", err, want)
			}
		}
	})

	t.Run("rejects non-postgres dsn", func(t *testing.T) {
		_, err := Load(env(map[string]string{"DATABASE_URL": "mysql://host/db"}))
		if err == nil || !strings.Contains(err.Error(), "scheme") {
			t.Fatalf("Load: want scheme error, got %v", err)
		}
	})

	t.Run("rejects dsn without database name", func(t *testing.T) {
		_, err := Load(env(map[string]string{"DATABASE_URL": "postgres://host"}))
		if err == nil || !strings.Contains(err.Error(), "database name") {
			t.Fatalf("Load: want database name error, got %v", err)
		}
	})
}

func TestParseLogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"":       slog.LevelInfo,
		"info":   slog.LevelInfo,
		"DEBUG":  slog.LevelDebug,
		" warn ": slog.LevelWarn,
		"error":  slog.LevelError,
	}
	for raw, want := range cases {
		got, err := parseLogLevel(raw)
		if err != nil {
			t.Errorf("parseLogLevel(%q): %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("parseLogLevel(%q) = %v, want %v", raw, got, want)
		}
	}
	if _, err := parseLogLevel("chatty"); err == nil {
		t.Error("parseLogLevel(chatty): want error, got nil")
	}
}
