package config

import (
	"os"
)

// Config holds all application configuration read from environment variables.
// No hardcoded values — all settings come from the environment at runtime.
type Config struct {
	Port                     string
	SupabaseURL              string
	SupabaseAnonKey          string
	SupabaseServiceRoleKey   string
	JWTSecret                string
	DatabaseURL              string
	IntegrationEncryptionKey string
	// ORSAPIKey authenticates server-side calls to OpenRouteService's
	// Directions API (rider route map). Sent as-is in the Authorization
	// header, no "Bearer" prefix — verified against the live API.
	ORSAPIKey string
	// ORSBaseURL overrides OpenRouteService's API domain. Empty uses the
	// default (api.openrouteservice.org); set only if ORS's documented
	// migration to api.heigit.org becomes necessary.
	ORSBaseURL string
}

// Load reads configuration from environment variables.
// Call this once at startup, after loading .env with godotenv.
func Load() *Config {
	return &Config{
		Port:                     getEnv("PORT", "8080"),
		SupabaseURL:              getEnv("SUPABASE_URL", ""),
		SupabaseAnonKey:          getEnv("SUPABASE_ANON_KEY", ""),
		SupabaseServiceRoleKey:   getEnv("SUPABASE_SERVICE_ROLE_KEY", ""),
		JWTSecret:                getEnv("JWT_SECRET", ""),
		DatabaseURL:              getEnv("DATABASE_URL", ""),
		IntegrationEncryptionKey: getEnv("INTEGRATION_ENCRYPTION_KEY", ""),
		ORSAPIKey:                getEnv("ORS_API_KEY", ""),
		ORSBaseURL:               getEnv("ORS_BASE_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}
