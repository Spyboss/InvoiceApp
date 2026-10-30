// Package config loads and validates the process configuration from the
// environment. It fails loudly at boot rather than at the first request.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

// Env names the deployment this process is running in.
type Env string

const (
	EnvDev        Env = "dev"
	EnvStaging    Env = "staging"
	EnvProduction Env = "production"
)

// Config is everything the server needs to start.
type Config struct {
	Env         Env
	HTTPAddr    string
	DatabaseURL string
	LogLevel    slog.Level
	// DevTenantSlug lets http://localhost resolve to a tenant without hosts
	// file entries. It is refused in production.
	DevTenantSlug string
}

// Load reads configuration from getenv, which is os.Getenv in production and a
// map lookup in tests.
func Load(getenv func(string) string) (Config, error) {
	var problems []error

	env := Env(strings.TrimSpace(getenv("APP_ENV")))
	if env == "" {
		env = EnvDev
	}
	switch env {
	case EnvDev, EnvStaging, EnvProduction:
	default:
		problems = append(problems, fmt.Errorf("APP_ENV %q: want dev, staging or production", env))
	}

	level, err := parseLogLevel(getenv("LOG_LEVEL"))
	if err != nil {
		problems = append(problems, err)
	}

	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
	if databaseURL == "" {
		problems = append(problems, errors.New("DATABASE_URL is required"))
	} else if err := validateDatabaseURL(databaseURL); err != nil {
		problems = append(problems, err)
	}

	cfg := Config{
		Env:           env,
		HTTPAddr:      httpAddr(getenv),
		DatabaseURL:   databaseURL,
		LogLevel:      level,
		DevTenantSlug: strings.TrimSpace(getenv("TENANT_DEV")),
	}
	if cfg.Env == EnvProduction && cfg.DevTenantSlug != "" {
		problems = append(problems, errors.New("TENANT_DEV must be empty in production"))
	}
	if err := errors.Join(problems...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// httpAddr prefers an explicit HTTP_ADDR, then the PORT that Railway injects,
// then the local default.
func httpAddr(getenv func(string) string) string {
	if addr := strings.TrimSpace(getenv("HTTP_ADDR")); addr != "" {
		return addr
	}
	if port := strings.TrimSpace(getenv("PORT")); port != "" {
		return ":" + port
	}
	return ":8080"
}

func validateDatabaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("DATABASE_URL: %w", err)
	}
	switch u.Scheme {
	case "postgres", "postgresql":
	default:
		return fmt.Errorf("DATABASE_URL: scheme %q, want postgres", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("DATABASE_URL: missing host")
	}
	if strings.TrimPrefix(u.Path, "/") == "" {
		return errors.New("DATABASE_URL: missing database name")
	}
	return nil
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("LOG_LEVEL %q: want debug, info, warn or error", raw)
	}
}
