package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	fiscaladapter "github.com/danzt/daas/api/internal/adapter/fiscal"
	adapterhttp "github.com/danzt/daas/api/internal/adapter/http"
	notifadapter "github.com/danzt/daas/api/internal/adapter/notification"
	"github.com/danzt/daas/api/internal/adapter/storage"
	"github.com/danzt/daas/api/internal/app"
	"github.com/danzt/daas/api/internal/config"
	"github.com/danzt/daas/api/internal/domain/notification"
)

func main() {
	// Configure zerolog for structured JSON output
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Load .env file if present (ignored in production where env vars are injected)
	if err := godotenv.Load(); err != nil {
		log.Debug().Msg("no .env file found, using environment variables")
	}

	cfg := config.Load()

	// Connect to PostgreSQL via pgxpool
	ctx := context.Background()
	var pool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		var err error
		poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to parse database URL")
		}
		// Prefer IPv4 to avoid VPN/routing issues with IPv6-only Supabase hosts.
		// Falls back to IPv6 if no A record exists (e.g. Supabase direct DB host
		// only publishes AAAA). Dialer has a 10s timeout per attempt.
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		poolCfg.ConnConfig.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("split host port: %w", err)
			}
			lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			// Try IPv4 first.
			if ips, err := net.DefaultResolver.LookupNetIP(lookupCtx, "ip4", host); err == nil && len(ips) > 0 {
				var firstErr error
				for _, ip := range ips {
					target := net.JoinHostPort(ip.String(), port)
					if conn, err := dialer.DialContext(ctx, "tcp4", target); err == nil {
						return conn, nil
					} else if firstErr == nil {
						firstErr = err
					}
				}
				// IPv4 addresses found but all unreachable — don't fall through to IPv6.
				return nil, fmt.Errorf("dial %s: all IPv4 addresses unreachable: %w", host, firstErr)
			}

			// No IPv4 record — fall back to IPv6 (e.g. Supabase AAAA-only hosts).
			if ips, err := net.DefaultResolver.LookupNetIP(lookupCtx, "ip6", host); err == nil && len(ips) > 0 {
				var firstErr error
				for _, ip := range ips {
					target := net.JoinHostPort(ip.String(), port)
					if conn, err := dialer.DialContext(ctx, "tcp6", target); err == nil {
						return conn, nil
					} else if firstErr == nil {
						firstErr = err
					}
				}
				return nil, fmt.Errorf("dial %s: all IPv6 addresses unreachable: %w", host, firstErr)
			}

			// Last resort: let the OS resolve normally.
			return dialer.DialContext(ctx, "tcp", addr)
		}
		pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create database connection pool")
		}
		defer pool.Close()
		// Ping with a 15s deadline so a slow/unreachable DB fails fast.
		pingCtx, pingCancel := context.WithTimeout(ctx, 15*time.Second)
		defer pingCancel()
		if err := pool.Ping(pingCtx); err != nil {
			log.Fatal().Err(err).Msg("failed to ping database")
		}
		log.Info().Msg("database connection pool ready")
	} else {
		log.Warn().Msg("DATABASE_URL not set — database features will be unavailable")
	}

	// Email adapter — activates when RESEND_API_KEY is set.
	var emailSvc notification.NotificationService
	if resendKey := os.Getenv("RESEND_API_KEY"); resendKey != "" {
		from := os.Getenv("RESEND_FROM_EMAIL")
		if from == "" {
			from = "noreply@daas.app"
		}
		emailSvc = notifadapter.NewEmailAdapter(resendKey, from)
		log.Info().Str("from", from).Msg("notification: email channel active (Resend)")
	} else {
		emailSvc = notifadapter.NewNoopAdapter()
		log.Info().Msg("notification: email channel inactive (RESEND_API_KEY not set)")
	}

	// WhatsApp adapter — activates when both Meta Business credentials are set.
	var whatsappSvc notification.NotificationService
	waPhoneID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID")
	waToken := os.Getenv("WHATSAPP_TOKEN")
	if waPhoneID != "" && waToken != "" {
		whatsappSvc = notifadapter.NewWhatsAppAdapter(waPhoneID, waToken)
		log.Info().Msg("notification: WhatsApp channel active")
	} else {
		whatsappSvc = notifadapter.NewNoopAdapter()
		log.Info().Msg("notification: WhatsApp channel inactive (WHATSAPP_PHONE_NUMBER_ID/WHATSAPP_TOKEN not set)")
	}

	// Fan-out to both channels. MultiChannelNotifier routes each message to the
	// correct adapter based on msg.Channel, running both concurrently.
	notifSvc := app.NewMultiChannelNotifier(emailSvc, "email", whatsappSvc, "whatsapp")

	// Storefront base URL used by lifecycle emails to build the customer
	// tracking links. Defaults to localhost for dev.
	storefrontURL := os.Getenv("PUBLIC_STOREFRONT_URL")
	if storefrontURL == "" {
		storefrontURL = "http://localhost:3000"
	}

	// Storage adapter — SupabaseStorageAdapter when all three Supabase env vars are
	// present; LocalFSAdapter otherwise (dev default).
	var storageAdapter storage.Storage
	supabaseProjectURL := os.Getenv("SUPABASE_PROJECT_URL")
	supabaseServiceKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	supabaseBucketName := os.Getenv("SUPABASE_STORAGE_BUCKET")
	if supabaseProjectURL != "" && supabaseServiceKey != "" && supabaseBucketName != "" {
		storageAdapter = storage.NewSupabaseStorageAdapter(supabaseProjectURL, supabaseServiceKey, supabaseBucketName)
		log.Info().Str("bucket", supabaseBucketName).Msg("storage adapter: SupabaseStorageAdapter")
	} else {
		storageRootDir := os.Getenv("STORAGE_ROOT_DIR")
		if storageRootDir == "" {
			storageRootDir = "./storage"
		}
		storagePublicBase := os.Getenv("STORAGE_PUBLIC_BASE")
		if storagePublicBase == "" {
			storagePublicBase = "http://localhost:8080/files"
		}
		storageAdapter = storage.NewLocalFSAdapter(storageRootDir, storagePublicBase)
		log.Info().
			Str("root_dir", storageRootDir).
			Str("public_base", storagePublicBase).
			Msg("storage adapter: LocalFSAdapter")
	}

	// Fiscal invoice service — shared between HTTP handler and background retry worker.
	// Uses MockAdapter in dev; swap for a real SENIAT TCP adapter in production.
	var fiscalInvoiceSvc *app.FiscalInvoiceService
	if pool != nil {
		fiscalInvoiceSvc = app.NewFiscalInvoiceService(pool, fiscaladapter.NewMockAdapter())
		retryWorker := app.NewFiscalRetryWorker(fiscalInvoiceSvc)
		go retryWorker.Start(ctx)
	}

	router := adapterhttp.NewRouterWithConfig(adapterhttp.RouterConfig{
		SupabaseURL:      cfg.SupabaseURL,
		AnonKey:          cfg.SupabaseAnonKey,
		ServiceRoleKey:   cfg.SupabaseServiceRoleKey,
		Pool:             pool,
		NotificationSvc:  notifSvc,
		StorefrontURL:    storefrontURL,
		Storage:          storageAdapter,
		FiscalInvoiceSvc: fiscalInvoiceSvc,
		ORSAPIKey:        cfg.ORSAPIKey,
		ORSBaseURL:       cfg.ORSBaseURL,
	})

	addr := ":" + cfg.Port
	log.Info().Str("addr", addr).Msg("starting daas api server")

	if err := router.Start(addr); err != nil {
		log.Fatal().Err(err).Msg("server stopped unexpectedly")
	}
}
