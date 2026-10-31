// Package config loads and validates the process configuration from the
// environment. It fails loudly at boot rather than at the first request.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
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
	// Gotenberg renders print documents. Empty means printing is unavailable;
	// the rest of the app still runs.
	GotenbergURL      string
	GotenbergUsername string
	GotenbergPassword string

	// Alert channels. Both are optional: with neither set, alerts still queue
	// and still show on the /alerts page, they just leave the building by no
	// route. The tenant then decides per dealership where they go, from its
	// own settings screen.
	AlertSMTPHost      string
	AlertSMTPPort      int
	AlertSMTPUsername  string
	AlertSMTPPassword  string
	AlertSMTPFrom      string
	AlertTelegramToken string
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

	gotenbergURL := strings.TrimSpace(getenv("GOTENBERG_URL"))
	if gotenbergURL != "" {
		if err := validateHTTPURL("GOTENBERG_URL", gotenbergURL); err != nil {
			problems = append(problems, err)
		}
	}
	gotenbergUser := strings.TrimSpace(getenv("GOTENBERG_USERNAME"))
	gotenbergPass := strings.TrimSpace(getenv("GOTENBERG_PASSWORD"))
	if (gotenbergUser == "") != (gotenbergPass == "") {
		problems = append(problems, errors.New("GOTENBERG_USERNAME and GOTENBERG_PASSWORD must be set together"))
	}

	// Alert channels. A half set relay is refused at boot rather than at the
	// first void, because a silently dead mailbox is worse than a refusal.
	alertSMTPHost := strings.TrimSpace(getenv("ALERT_SMTP_HOST"))
	alertSMTPPort, err := parsePort(getenv("ALERT_SMTP_PORT"))
	if err != nil {
		problems = append(problems, err)
	}
	alertSMTPUser := strings.TrimSpace(getenv("ALERT_SMTP_USERNAME"))
	alertSMTPPass := strings.TrimSpace(getenv("ALERT_SMTP_PASSWORD"))
	if (alertSMTPUser == "") != (alertSMTPPass == "") {
		problems = append(problems, errors.New("ALERT_SMTP_USERNAME and ALERT_SMTP_PASSWORD must be set together"))
	}
	if alertSMTPHost != "" && strings.TrimSpace(getenv("ALERT_SMTP_FROM")) == "" {
		problems = append(problems, errors.New("ALERT_SMTP_FROM is required when ALERT_SMTP_HOST is set"))
	}

	cfg := Config{
		Env:               env,
		HTTPAddr:          httpAddr(getenv),
		DatabaseURL:       databaseURL,
		LogLevel:          level,
		DevTenantSlug:     strings.TrimSpace(getenv("TENANT_DEV")),
		GotenbergURL:      gotenbergURL,
		GotenbergUsername: gotenbergUser,
		GotenbergPassword: gotenbergPass,

		AlertSMTPHost:      alertSMTPHost,
		AlertSMTPPort:      alertSMTPPort,
		AlertSMTPUsername:  alertSMTPUser,
		AlertSMTPPassword:  alertSMTPPass,
		AlertSMTPFrom:      strings.TrimSpace(getenv("ALERT_SMTP_FROM")),
		AlertTelegramToken: strings.TrimSpace(getenv("ALERT_TELEGRAM_TOKEN")),
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

// parsePort reads an SMTP port. An empty value is the zero port, which simply
// leaves the mail channel off; anything else must be a real port number.
func parsePort(raw string) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("ALERT_SMTP_PORT %q: want a port between 1 and 65535", raw)
	}
	return n, nil
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

func validateHTTPURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s: scheme %q, want http or https", name, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%s: missing host", name)
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
