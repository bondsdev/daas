package http

import (
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/danzt/daas/api/internal/adapter/exchangerate"
	"github.com/danzt/daas/api/internal/adapter/http/handler"
	mw "github.com/danzt/daas/api/internal/adapter/http/middleware"
	"github.com/danzt/daas/api/internal/adapter/storage"
	"github.com/danzt/daas/api/internal/app"
	"github.com/danzt/daas/api/internal/domain/notification"
)

// RouterConfig holds dependencies needed to build the Echo router.
type RouterConfig struct {
	SupabaseURL    string
	AnonKey        string
	ServiceRoleKey string
	Pool           *pgxpool.Pool
	// NotificationSvc + StorefrontURL — used for transactional emails
	// sent to customers when their order changes state. Optional; when nil,
	// state transitions still succeed but no email is dispatched.
	NotificationSvc notification.NotificationService
	StorefrontURL   string
	// Storage is used by the payment-proof upload endpoint.
	// When nil, the endpoint is still registered but upload calls will fail.
	Storage storage.Storage
	// FiscalInvoiceSvc is an optional pre-built FiscalInvoiceService.
	// When set, the router uses it directly instead of creating its own.
	// Pass this from main.go when sharing the service with the retry worker.
	FiscalInvoiceSvc *app.FiscalInvoiceService
}

// NewRouter creates a new Echo instance with standard middleware configured
// and all routes registered.
func NewRouter() *echo.Echo {
	return NewRouterWithConfig(RouterConfig{
		SupabaseURL:    os.Getenv("SUPABASE_URL"),
		ServiceRoleKey: os.Getenv("SUPABASE_SERVICE_ROLE_KEY"),
	})
}

// NewRouterWithConfig builds the router with explicit dependencies.
// Used in production (from main.go) and in integration tests.
func NewRouterWithConfig(cfg RouterConfig) *echo.Echo { //nolint:funlen,cyclop
	e := echo.New()
	e.HideBanner = true

	// Structured JSON logger (zerolog) — outputs to stdout
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Middleware
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:   true,
		LogURI:      true,
		LogMethod:   true,
		LogLatency:  true,
		LogRemoteIP: true,
		LogError:    true,
		HandleError: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			event := logger.Info()
			if v.Error != nil {
				event = log.Error().Err(v.Error)
			}
			event.
				Str("method", v.Method).
				Str("uri", v.URI).
				Int("status", v.Status).
				Dur("latency", v.Latency).
				Str("remote_ip", v.RemoteIP).
				Time("time", time.Now()).
				Msg("request")
			return nil
		},
	}))
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowHeaders: []string{
			echo.HeaderOrigin,
			echo.HeaderContentType,
			echo.HeaderAccept,
			echo.HeaderAuthorization,
		},
	}))

	// Build handlers
	authMW := mw.NewAuthMiddleware(cfg.SupabaseURL)

	tenantSvc := app.NewTenantService(cfg.Pool, cfg.SupabaseURL, cfg.ServiceRoleKey)
	tenantHandler := handler.NewTenantHandler(tenantSvc)

	authHandler := handler.NewAuthHandler(cfg.SupabaseURL, cfg.AnonKey)
	userHandler := handler.NewUserHandler(cfg.Pool, cfg.SupabaseURL, cfg.ServiceRoleKey)
	integrationHandler := handler.NewIntegrationHandler(cfg.Pool)
	meHandler := handler.NewMeHandler(cfg.Pool)
	productHandler := handler.NewProductHandler(cfg.Pool)
	if cfg.Storage != nil {
		productHandler.SetStorage(cfg.Storage)
	}
	inventoryHandler := handler.NewInventoryHandler(cfg.Pool)
	// Create InternalInvoiceService once — shared between InvoiceHandler and auto-invoice.
	internalInvoiceSvc := app.NewInternalInvoiceService(cfg.Pool)
	invoiceHandler := handler.NewInvoiceHandlerWithService(internalInvoiceSvc)
	// POS quick-sale reuses InternalInvoiceService; it only needs a CashSessionService
	// to require an open cash session, so it gets its own instance (CashSessionHandler
	// below keeps its own, both are stateless and share the same pool).
	internalInvoiceSvc.SetCashSessionService(app.NewCashSessionService(cfg.Pool, exchangerate.NewBCVFetcher("")))
	posSaleHandler := handler.NewPosSaleHandler(internalInvoiceSvc)
	// FiscalInvoiceHandler — use the pre-built service when provided by main.go
	// (so the same instance is shared with the retry worker), otherwise fall back
	// to creating a fresh one with the mock adapter.
	var fiscalInvoiceHandler *handler.FiscalInvoiceHandler
	if cfg.FiscalInvoiceSvc != nil {
		fiscalInvoiceHandler = handler.NewFiscalInvoiceHandlerWithService(cfg.FiscalInvoiceSvc)
	} else {
		fiscalInvoiceHandler = handler.NewFiscalInvoiceHandler(cfg.Pool)
	}
	fiscalInvoiceSvc := fiscalInvoiceHandler.Service()
	supplierHandler := handler.NewSupplierHandler(cfg.Pool)
	reportHandler := handler.NewReportHandler(cfg.Pool)
	saleHandler := handler.NewSaleHandler(cfg.Pool)
	cashSessionHandler := handler.NewCashSessionHandler(cfg.Pool)

	// Build lifecycle email notifier from injected NotificationSvc.
	// nil-safe: if the notification service is not configured, the notifier
	// stays nil and ShopOrderService.SetNotifier(nil) skips all sends.
	var shopNotifier *app.ShopOrderNotifier
	if cfg.NotificationSvc != nil && cfg.Pool != nil {
		shopNotifier = app.NewShopOrderNotifier(cfg.NotificationSvc, cfg.Pool, cfg.StorefrontURL)
	}
	shopOrderHandler := handler.NewShopOrderHandler(cfg.Pool, shopNotifier, cfg.Storage)
	if cfg.Pool != nil {
		shopOrderHandler.SetInvoiceService(internalInvoiceSvc)
		shopOrderHandler.SetFiscalInvoiceService(fiscalInvoiceSvc)
	}
	paymentMethodHandler := handler.NewPaymentMethodHandler(cfg.Pool)
	brandingHandler := handler.NewBrandingHandler(cfg.Pool, cfg.Storage)
	dashboardHandler := handler.NewDashboardHandler(cfg.Pool)
	pdfHandler := handler.NewPDFHandler(cfg.Pool, internalInvoiceSvc, fiscalInvoiceSvc)

	// Routes
	registerRoutes(e, authMW, cfg.Pool, tenantHandler, authHandler, userHandler, integrationHandler, meHandler, productHandler, inventoryHandler, invoiceHandler, fiscalInvoiceHandler, supplierHandler, reportHandler, saleHandler, shopOrderHandler, paymentMethodHandler, brandingHandler, dashboardHandler, pdfHandler, cashSessionHandler, posSaleHandler)

	// Storefront public route group — no auth required.
	// Rate limiter runs first (fast reject), then tenant resolver activates RLS.
	// Handlers for catalog endpoints are wired in S6-T8.
	if cfg.Pool != nil {
		// Serve uploaded payment proofs (and any other stored files) as static assets.
		// Files are written by LocalFSAdapter under ./storage, so the URL
		// http://localhost:8080/files/payment-proofs/... maps to ./storage/payment-proofs/...
		e.Static("/files", "./storage")

		rateLimiter := mw.NewRateLimiter()
		pathResolver := mw.NewPathResolver(cfg.Pool)
		publicTenantMW := mw.NewPublicTenantMiddleware(pathResolver, cfg.Pool)
		storefrontFeatureMW := mw.NewStorefrontFeatureMiddleware(cfg.Pool)

		storefront := e.Group("/t/:tenantSlug/shop/v1")
		storefront.Use(rateLimiter.Handle())
		storefront.Use(publicTenantMW.Handle())
		storefront.Use(storefrontFeatureMW.Handle())

		// Smoke-check healthz endpoint — verifies the middleware chain is wired
		// correctly without requiring the full catalog handler (S6-T8).
		storefront.GET("/healthz", func(c echo.Context) error {
			return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
		})

		// Public catalog endpoints (S6-T8).
		shopSvc := app.NewShopService(cfg.Pool)
		shopHandler := handler.NewShopHandler(shopSvc)
		storefront.GET("/products", shopHandler.ListProducts)
		storefront.GET("/products/:id", shopHandler.GetProduct)

		// Public checkout + order tracking endpoints (S7-PR2).
		storefront.POST("/checkout", shopOrderHandler.Checkout)
		storefront.GET("/orders/:id", shopOrderHandler.GetOrder)
		storefront.POST("/orders/:id/cancel", shopOrderHandler.CancelOrder)

		// Public payment methods (S8-PR1).
		storefront.GET("/payment-methods", paymentMethodHandler.PublicList)

		// Public tenant branding (S9-T2).
		storefront.GET("/branding", brandingHandler.GetPublic)

		// Customer order history (S9-PR4).
		storefront.GET("/my-orders", shopOrderHandler.MyOrders)

		// Payment proof upload (S8-PR2). Body limit slightly above 5MB to allow
		// multipart framing overhead; the service enforces exact 5MB on the file itself.
		storefront.POST("/orders/:id/payment-proof",
			shopOrderHandler.UploadPaymentProof,
			middleware.BodyLimit("6M"),
		)

		// Portal público del proveedor (Fase 2b) — sin login, autenticado por el
		// token del link. Rate-limited igual que el storefront.
		portal := e.Group("/api/v1/supplier-portal/:token")
		portal.Use(rateLimiter.Handle())
		portal.GET("", supplierHandler.GetPortal)
		portal.POST("/catalog", supplierHandler.AddPortalCatalogItem)
		portal.DELETE("/catalog/:itemId", supplierHandler.DeletePortalCatalogItem)
		portal.POST("/catalog/import-excel", supplierHandler.ImportPortalCatalogExcel,
			middleware.BodyLimit("5M"))
	}

	return e
}

func registerRoutes(
	e *echo.Echo,
	authMW *mw.AuthMiddleware,
	pool *pgxpool.Pool,
	tenantHandler *handler.TenantHandler,
	authHandler *handler.AuthHandler,
	userHandler *handler.UserHandler,
	integrationHandler *handler.IntegrationHandler,
	meHandler *handler.MeHandler,
	productHandler *handler.ProductHandler,
	inventoryHandler *handler.InventoryHandler,
	invoiceHandler *handler.InvoiceHandler,
	fiscalInvoiceHandler *handler.FiscalInvoiceHandler,
	supplierHandler *handler.SupplierHandler,
	reportHandler *handler.ReportHandler,
	saleHandler *handler.SaleHandler,
	shopOrderHandler *handler.ShopOrderHandler,
	paymentMethodHandler *handler.PaymentMethodHandler,
	brandingHandler *handler.BrandingHandler,
	dashboardHandler *handler.DashboardHandler,
	pdfHandler *handler.PDFHandler,
	cashSessionHandler *handler.CashSessionHandler,
	posSaleHandler *handler.PosSaleHandler,
) {
	// Health check — unauthenticated
	e.GET("/health", healthHandler)

	// Auth routes (no JWT middleware — these create/verify identity)
	auth := e.Group("/api/v1/auth")
	auth.POST("/register", tenantHandler.Register)
	auth.POST("/login", authHandler.Login)
	auth.POST("/logout", authHandler.Logout)
	auth.POST("/recover", authHandler.RequestPasswordReset)
	auth.PUT("/password", authHandler.UpdatePassword)

	// Protected routes — require valid JWT + tenant context
	// TenantMiddleware is optional here when pool is nil (dev/test without DB).
	var apiMiddlewares []echo.MiddlewareFunc
	apiMiddlewares = append(apiMiddlewares, authMW.Handle())
	if pool != nil {
		tenantMW := mw.NewTenantMiddleware(pool)
		apiMiddlewares = append(apiMiddlewares, tenantMW.Handle())
	}

	api := e.Group("/api/v1", apiMiddlewares...)

	// Tenant
	api.GET("/tenants/me", meHandler.GetMe)
	api.PATCH("/tenants/me", meHandler.UpdateMe, mw.OwnerGuard())
	api.GET("/users", userHandler.List)
	api.POST("/users/invite", userHandler.Invite, mw.OwnerGuard())
	api.PATCH("/users/:id", userHandler.UpdateActive, mw.OwnerGuard())
	api.PUT("/tenant/integrations/fiscal", integrationHandler.UpsertFiscal, mw.OwnerGuard())
	api.GET("/tenant/integrations/fiscal", integrationHandler.GetFiscal)

	// Products — NOTE: /products/categories must be registered BEFORE /products/:id
	// so Echo's router doesn't match "categories" as a UUID parameter.
	api.GET("/products/categories", productHandler.ListCategories)
	api.POST("/products/categories", productHandler.CreateCategory)
	api.DELETE("/products/categories/:id", productHandler.DeleteCategory, mw.OwnerGuard())
	api.GET("/products", productHandler.ListProducts)
	api.POST("/products", productHandler.CreateProduct)
	api.GET("/products/:id", productHandler.GetProduct)
	api.PUT("/products/:id", productHandler.UpdateProduct)
	api.DELETE("/products/:id", productHandler.DeleteProduct)
	api.POST("/products/:id/image", productHandler.UploadProductImage, middleware.BodyLimit("6M"))
	api.DELETE("/products/:id/image", productHandler.DeleteProductImage)

	// Inventory
	api.GET("/inventory/stock", inventoryHandler.ListStock)
	api.GET("/inventory/stock/:product_id", inventoryHandler.GetProductStock)
	api.GET("/inventory/movements", inventoryHandler.ListMovements)
	api.GET("/inventory/movements/:id", inventoryHandler.GetMovement)
	api.POST("/inventory/adjustments", inventoryHandler.CreateAdjustment)

	// Internal invoices — action routes registered before /:id to avoid conflicts
	api.POST("/invoices/internal", invoiceHandler.Create)
	api.GET("/invoices/internal", invoiceHandler.List)
	api.GET("/invoices/internal/:id", invoiceHandler.GetByID)
	api.POST("/invoices/internal/:id/issue", invoiceHandler.Issue)
	api.POST("/invoices/internal/:id/cancel", invoiceHandler.Cancel)
	api.GET("/invoices/internal/:id/pdf", pdfHandler.DownloadInternal)

	// Fiscal invoices (SENIAT) — same ordering convention
	api.POST("/invoices/fiscal", fiscalInvoiceHandler.Create)
	api.GET("/invoices/fiscal", fiscalInvoiceHandler.List)
	api.GET("/invoices/fiscal/:id", fiscalInvoiceHandler.GetByID)
	api.POST("/invoices/fiscal/:id/issue", fiscalInvoiceHandler.Issue)
	api.POST("/invoices/fiscal/:id/cancel", fiscalInvoiceHandler.Cancel)
	api.POST("/invoices/fiscal/:id/retry", fiscalInvoiceHandler.Retry)
	api.GET("/invoices/fiscal/:id/pdf", pdfHandler.DownloadFiscal)

	// Sales Orders — action routes before /:id
	api.POST("/sales-orders", saleHandler.CreateOrder)
	api.GET("/sales-orders", saleHandler.ListOrders)
	api.GET("/sales-orders/:id", saleHandler.GetOrder)
	api.POST("/sales-orders/:id/confirm", saleHandler.ConfirmOrder)
	api.POST("/sales-orders/:id/invoice", saleHandler.InvoiceOrder)
	api.POST("/sales-orders/:id/cancel", saleHandler.CancelOrder)

	// Reports — JSON
	api.GET("/reports/sales", reportHandler.SalesReport)
	api.GET("/reports/inventory", reportHandler.InventoryReport)
	api.GET("/reports/purchases", reportHandler.PurchaseReport)
	api.GET("/reports/sales-book", reportHandler.SalesBook)

	// Reports — CSV export
	api.GET("/reports/sales/export", reportHandler.ExportSalesCSV)
	api.GET("/reports/inventory/export", reportHandler.ExportInventoryCSV)
	api.GET("/reports/purchases/export", reportHandler.ExportPurchasesCSV)
	// Suppliers
	api.GET("/suppliers", supplierHandler.ListSuppliers)
	api.POST("/suppliers", supplierHandler.CreateSupplier)
	api.GET("/suppliers/:id", supplierHandler.GetSupplier)
	api.PUT("/suppliers/:id", supplierHandler.UpdateSupplier)
	api.POST("/suppliers/:id/portal-token", supplierHandler.GeneratePortalToken)
	api.DELETE("/suppliers/:id/portal-token", supplierHandler.RevokePortalToken)
	api.GET("/suppliers/:id/catalog", supplierHandler.GetSupplierCatalog)
	api.POST("/suppliers/:id/catalog/:itemId/import", supplierHandler.ImportCatalogItem)

	// Purchase Orders
	api.GET("/purchase-orders", supplierHandler.ListPOs)
	api.POST("/purchase-orders", supplierHandler.CreatePO)
	api.GET("/purchase-orders/:id", supplierHandler.GetPO)
	api.POST("/purchase-orders/:id/order", supplierHandler.OrderPO)
	api.POST("/purchase-orders/:id/receive", supplierHandler.ReceivePO)
	api.POST("/purchase-orders/:id/cancel", supplierHandler.CancelPO)
	api.PUT("/purchase-orders/:id/invoice", supplierHandler.SetPurchaseInvoice)

	// Shop Orders — tenant admin lifecycle (S7-PR2)
	api.GET("/shop-orders", shopOrderHandler.AdminList)
	api.GET("/shop-orders/:id", shopOrderHandler.AdminGet)
	api.POST("/shop-orders/:id/mark-paid", shopOrderHandler.MarkPaid)
	api.POST("/shop-orders/:id/mark-fulfilled", shopOrderHandler.MarkFulfilled)
	api.POST("/shop-orders/:id/mark-delivered", shopOrderHandler.MarkDelivered)
	api.POST("/shop-orders/:id/cancel", shopOrderHandler.AdminCancel)

	// Payment Methods — tenant settings (S8-PR1)
	api.GET("/payment-methods", paymentMethodHandler.AdminList)
	api.POST("/payment-methods", paymentMethodHandler.AdminCreate)
	api.PUT("/payment-methods/:id", paymentMethodHandler.AdminUpdate)
	api.DELETE("/payment-methods/:id", paymentMethodHandler.AdminDelete)

	// Branding — storefront personalisation (S9-T2)
	api.GET("/branding", brandingHandler.GetAdmin)
	api.PUT("/branding", brandingHandler.Update)
	api.POST("/branding/logo", brandingHandler.UploadLogo, middleware.BodyLimit("3M"))
	api.DELETE("/branding/logo", brandingHandler.DeleteLogo)
	api.POST("/branding/banner", brandingHandler.UploadBanner, middleware.BodyLimit("6M"))
	api.DELETE("/branding/banner", brandingHandler.DeleteBanner)

	// Dashboard KPIs — single combined endpoint (S10-PR1)
	api.GET("/dashboard", dashboardHandler.Get)

	// Cash register (caja) sessions — action routes before /:id-less static paths
	api.POST("/cash-sessions/open", cashSessionHandler.Open)
	api.POST("/cash-sessions/close", cashSessionHandler.Close)
	api.GET("/cash-sessions/open", cashSessionHandler.GetOpen)
	api.GET("/cash-sessions/:id/summary", reportHandler.CashSessionSummary)

	// POS quick-sale — counter sale tied to the tenant's open cash session.
	api.POST("/pos/sales", posSaleHandler.Create)
}

// healthHandler responds with a simple status OK payload.
// Used by load balancers and uptime monitors.
func healthHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}
