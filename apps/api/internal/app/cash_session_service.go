package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danzt/daas/api/internal/adapter/exchangerate"
	"github.com/danzt/daas/api/internal/domain/cashsession"
)

// CashSessionService handles opening/closing the cash register and freezing
// the day's BS/USD exchange rate on open.
type CashSessionService struct {
	pool    *pgxpool.Pool
	fetcher exchangerate.Fetcher
}

// NewCashSessionService creates a service backed by the given pool and rate fetcher.
func NewCashSessionService(pool *pgxpool.Pool, fetcher exchangerate.Fetcher) *CashSessionService {
	return &CashSessionService{pool: pool, fetcher: fetcher}
}

// Open starts a new cash session for the tenant.
//
// Rate resolution: if manualRate is nil, it tries the fetcher first; on
// success the rate is upserted into exchange_rates with source='auto' (never
// overwriting a rate already recorded today). If the fetch fails, manualRate
// is required and is upserted with source='manual'.
func (s *CashSessionService) Open(ctx context.Context, tenantID uuid.UUID, openedBySupabaseUID string, manualRate *float64, notes string) (*cashsession.CashSession, error) {
	req := cashsession.OpenRequest{ManualRate: manualRate, Notes: notes}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	openedBy, err := s.resolveUserID(ctx, tenantID, openedBySupabaseUID)
	if err != nil {
		return nil, err
	}

	var open bool
	if err = s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM cash_sessions WHERE tenant_id=$1 AND status='open')`,
		tenantID,
	).Scan(&open); err != nil {
		return nil, fmt.Errorf("check open session: %w", err)
	}
	if open {
		return nil, cashsession.ErrSessionAlreadyOpen
	}

	rate, source, err := s.resolveRate(ctx, manualRate)
	if err != nil {
		return nil, err
	}

	today := time.Now().UTC().Format("2006-01-02")
	// ON CONFLICT DO NOTHING: a rate already recorded today wins over the one
	// just fetched/entered. RETURNING the final row keeps the session in sync
	// with whichever rate actually got persisted for the day.
	err = s.pool.QueryRow(ctx,
		`INSERT INTO exchange_rates (tenant_id, rate_date, rate, source)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (tenant_id, rate_date) DO UPDATE SET tenant_id = exchange_rates.tenant_id
		 RETURNING rate, source`,
		tenantID, today, rate, source,
	).Scan(&rate, &source)
	if err != nil {
		return nil, fmt.Errorf("upsert exchange rate: %w", err)
	}

	sess := &cashsession.CashSession{
		ID:                 uuid.New(),
		TenantID:           tenantID,
		Status:             cashsession.StatusOpen,
		ExchangeRate:       rate,
		ExchangeRateSource: source,
		OpeningNotes:       notes,
		OpenedBy:           openedBy,
		OpenedAt:           time.Now().UTC(),
	}
	err = s.pool.QueryRow(ctx,
		`INSERT INTO cash_sessions
		    (id, tenant_id, status, exchange_rate, exchange_rate_source, opening_notes, opened_by, opened_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING created_at, updated_at`,
		sess.ID, sess.TenantID, sess.Status, sess.ExchangeRate, sess.ExchangeRateSource,
		sess.OpeningNotes, sess.OpenedBy, sess.OpenedAt,
	).Scan(&sess.CreatedAt, &sess.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, cashsession.ErrSessionAlreadyOpen
		}
		return nil, fmt.Errorf("insert cash session: %w", err)
	}
	return sess, nil
}

// Close ends the tenant's currently open cash session.
func (s *CashSessionService) Close(ctx context.Context, tenantID uuid.UUID, closedBySupabaseUID string, notes string) (*cashsession.CashSession, error) {
	closedBy, err := s.resolveUserID(ctx, tenantID, closedBySupabaseUID)
	if err != nil {
		return nil, err
	}

	sess, err := s.GetOpen(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	closedAt := time.Now().UTC()
	_, err = s.pool.Exec(ctx,
		`UPDATE cash_sessions
		 SET status='closed', closed_by=$1, closed_at=$2, closing_notes=$3, updated_at=NOW()
		 WHERE id=$4`,
		closedBy, closedAt, notes, sess.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("close cash session: %w", err)
	}

	sess.Status = cashsession.StatusClosed
	sess.ClosedBy = &closedBy
	sess.ClosedAt = &closedAt
	sess.ClosingNotes = notes
	return sess, nil
}

// GetOpen returns the tenant's currently open cash session.
func (s *CashSessionService) GetOpen(ctx context.Context, tenantID uuid.UUID) (*cashsession.CashSession, error) {
	sess, err := s.scanCashSession(s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, status, exchange_rate, exchange_rate_source,
		        COALESCE(opening_notes,''), COALESCE(closing_notes,''),
		        opened_by, opened_at, closed_by, closed_at, created_at, updated_at
		 FROM cash_sessions
		 WHERE tenant_id=$1 AND status='open'`,
		tenantID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cashsession.ErrNoOpenSession
		}
		return nil, fmt.Errorf("load open cash session: %w", err)
	}
	return sess, nil
}

// resolveRate tries the fetcher when manualRate is nil; falls back to
// manualRate (required) on fetch failure.
func (s *CashSessionService) resolveRate(ctx context.Context, manualRate *float64) (float64, cashsession.RateSource, error) {
	if manualRate == nil {
		if rate, err := s.fetcher.FetchRate(ctx); err == nil && rate > 0 {
			return rate, cashsession.RateSourceAuto, nil
		}
		return 0, "", cashsession.ErrRateRequired
	}
	return *manualRate, cashsession.RateSourceManual, nil
}

func (s *CashSessionService) resolveUserID(ctx context.Context, tenantID uuid.UUID, supabaseUID string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM tenant_users WHERE tenant_id=$1 AND supabase_uid=$2`,
		tenantID, supabaseUID,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve user: %w", err)
	}
	return id, nil
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}

type cashSessionScanner interface {
	Scan(dest ...any) error
}

func (s *CashSessionService) scanCashSession(row cashSessionScanner) (*cashsession.CashSession, error) {
	var sess cashsession.CashSession
	if err := row.Scan(
		&sess.ID, &sess.TenantID, &sess.Status, &sess.ExchangeRate, &sess.ExchangeRateSource,
		&sess.OpeningNotes, &sess.ClosingNotes,
		&sess.OpenedBy, &sess.OpenedAt, &sess.ClosedBy, &sess.ClosedAt,
		&sess.CreatedAt, &sess.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &sess, nil
}
