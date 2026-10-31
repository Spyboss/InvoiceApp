package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spyboss/invoiceapp/internal/buildinfo"
	"github.com/spyboss/invoiceapp/internal/config"
	"github.com/spyboss/invoiceapp/internal/db"
	"github.com/spyboss/invoiceapp/internal/notify"
	"github.com/spyboss/invoiceapp/internal/pdf"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/tenant"
	"github.com/spyboss/invoiceapp/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Local development only: deployments have no .env and real environment
	// variables win over the file either way.
	if err := config.LoadDotEnv(".env"); err != nil {
		return fmt.Errorf("read .env: %w", err)
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := migrate(ctx, cfg, log); err != nil {
		return err
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	st := store.New(pool)
	resolver := tenant.NewResolver(st, cfg.DevTenantSlug)

	// Alert delivery. Both channels are optional; with neither set the alerts
	// still queue and still show on the /alerts page, they just leave the
	// building by no route.
	alertSender := &notify.Sender{Store: st}
	if mailer := (notify.SMTPMailer{
		Host:     cfg.AlertSMTPHost,
		Port:     cfg.AlertSMTPPort,
		Username: cfg.AlertSMTPUsername,
		Password: cfg.AlertSMTPPassword,
		From:     cfg.AlertSMTPFrom,
	}); mailer.Available() {
		alertSender.Mail = mailer
	} else if cfg.AlertSMTPHost != "" {
		// Config.Load refuses a half set relay, so this is belt and braces
		// rather than a second opinion.
		log.Warn("alert email is configured but incomplete; the channel stays off",
			"host", cfg.AlertSMTPHost)
	}
	if telegram := (notify.TelegramNotifier{Token: cfg.AlertTelegramToken}); telegram.Available() {
		alertSender.Notifier = telegram
	}

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: web.New(web.Deps{
			Log:           log,
			Store:         st,
			Ping:          pool.Ping,
			ResolveTenant: resolver.ByHost,
			ForgetTenant:  resolver.Forget,
			SecureCookies: cfg.Env != config.EnvDev,
			PDF:           pdf.New(cfg.GotenbergURL, cfg.GotenbergUsername, cfg.GotenbergPassword),
			Alerts:        alertSender,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("server listening",
			slog.String("addr", cfg.HTTPAddr),
			slog.String("env", string(cfg.Env)),
			slog.String("version", buildinfo.Version),
			slog.String("commit", buildinfo.Commit()),
		)
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
		return nil
	case <-ctx.Done():
		log.Info("shutdown requested", slog.String("signal", ctx.Err().Error()))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("shutdown complete")
	return nil
}

// migrate applies pending migrations with the admin credentials, then closes
// that pool. The server never serves a request on the admin connection.
func migrate(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	admin, err := db.OpenAdmin(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("migration connection: %w", err)
	}
	defer admin.Close()

	applied, err := db.Migrate(ctx, admin)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	log.Info("migrations applied", slog.Int("count", applied))
	return nil
}
