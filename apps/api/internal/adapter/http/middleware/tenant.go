package middleware

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

// ConnExecer abstracts the single database operation TenantMiddleware needs:
// executing a parameterized query on a dedicated connection.
// Exposing this interface allows tests to inject a mock without requiring a
// real pgxpool.
type ConnExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Release()
}

// connAcquirer is a function that acquires a ConnExecer from a connection pool.
// In production this wraps pgxpool.Pool.Acquire; in tests it injects a mock.
type connAcquirer func(ctx context.Context) (ConnExecer, error)

// TenantMiddleware reads the tenant_id from context (set by AuthMiddleware)
// and executes SELECT set_config('app.tenant_id', $1, true) on a dedicated
// database connection. This activates PostgreSQL RLS policies that filter by
// app.tenant_id.
//
// The parameterized set_config call is equivalent to SET LOCAL for the session
// (connection) lifetime and is safe against SQL injection — the tenant_id is
// never interpolated into the SQL string.
//
// The middleware acts as the second isolation barrier (Go is the first;
// RLS is the second). Any query executed after this middleware runs will
// automatically be filtered by the tenant's RLS policy.
type TenantMiddleware struct {
	acquire connAcquirer
}

// NewTenantMiddleware creates a TenantMiddleware backed by the given pgx pool.
// The pool's Acquire method is used at runtime to obtain a dedicated connection.
func NewTenantMiddleware(pool *pgxpool.Pool) *TenantMiddleware {
	return &TenantMiddleware{
		acquire: func(ctx context.Context) (ConnExecer, error) {
			return pool.Acquire(ctx)
		},
	}
}

// NewTenantMiddlewareWithConn creates a TenantMiddleware with a custom
// connection acquirer. This constructor is intended for tests: pass a function
// that returns a mock ConnExecer to capture and assert on the SQL and arguments
// sent by the middleware.
func NewTenantMiddlewareWithConn(acquire func(ctx context.Context) (ConnExecer, error)) *TenantMiddleware {
	return &TenantMiddleware{acquire: acquire}
}

// Handle returns the Echo middleware handler function.
// It acquires a connection from the pool, sets the local tenant parameter via
// a parameterized SELECT set_config call, and stores the connection on the
// context for use by handlers.
func (m *TenantMiddleware) Handle() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			tenantID, ok := c.Get(string(ContextKeyTenantID)).(string)
			if !ok || tenantID == "" {
				return problemJSON(c, http.StatusForbidden, "forbidden", "tenant context missing")
			}

			// Acquire a dedicated connection from the pool. This is required
			// because set_config with is_local=true only persists for the
			// duration of the current connection session — pgxpool would route
			// queries to different connections otherwise.
			conn, err := m.acquire(c.Request().Context())
			if err != nil {
				return problemJSON(c, http.StatusInternalServerError, "internal-error",
					"failed to acquire database connection")
			}
			defer conn.Release()

			// Use a parameterized query so the tenant_id is NEVER interpolated
			// into the SQL string. This eliminates any SQL injection risk.
			// set_config(name, value, is_local=true) scopes the setting to the
			// current connection session, matching the behavior of SET LOCAL
			// outside an explicit transaction.
			_, err = conn.Exec(c.Request().Context(),
				"SELECT set_config('app.tenant_id', $1, true)", tenantID)
			if err != nil {
				return problemJSON(c, http.StatusInternalServerError, "internal-error",
					"failed to set tenant context")
			}

			// Rider sessions additionally get app.rider_id set so the RLS
			// backstop on shop_orders (migration 000023) can restrict them to
			// their own deliveries. Owner/employee sessions never set this —
			// current_setting('app.rider_id', true) stays NULL for them, which
			// is how that policy tells a rider session apart from a staff one.
			role, _ := c.Get(string(ContextKeyRole)).(string)
			if role == "rider" {
				supabaseUID, _ := c.Get(string(ContextKeySupabaseUID)).(string)
				_, err = conn.Exec(c.Request().Context(),
					`SELECT set_config('app.rider_id', (
						SELECT r.id::text FROM riders r
						JOIN tenant_users tu ON tu.id = r.user_id
						WHERE r.tenant_id = $1 AND tu.supabase_uid = $2
					), true)`, tenantID, supabaseUID)
				if err != nil {
					return problemJSON(c, http.StatusInternalServerError, "internal-error",
						"failed to set rider context")
				}
			}

			// Store the acquired connection on the context so handlers can use
			// the same connection (and therefore the same session-scoped
			// set_config value, which activates RLS).
			c.Set("db_conn", conn)
			c.Set(string(ContextKeyTenantID), tenantID)

			return next(c)
		}
	}
}
