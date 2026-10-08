package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port             string
	DatabaseURL      string
	JWTSecret        string
	JWTTTLHours      int
	MaxUploadBytes   int64
	StorageDriver    string
	LocalStoragePath string
	AllowedOrigins   []string
	SeedAdminEmail   string
	SeedAdminPass    string
	SeedAdminName    string
	S3Endpoint       string
	S3Region         string
	S3Bucket         string
	S3AccessKey      string
	S3SecretKey      string
	S3ForcePathStyle bool
	S3PublicBaseURL  string
}

func Load() Config {
	mb := envInt("MAX_UPLOAD_MB", 250)
	return Config{
		Port:             env("PORT", "8080"),
		DatabaseURL:      env("DATABASE_URL", "postgres://support:change-me@localhost:5432/support_tickets?sslmode=disable"),
		JWTSecret:        env("JWT_SECRET", "dev-only-change-this-secret"),
		JWTTTLHours:      envInt("JWT_TTL_HOURS", 8),
		MaxUploadBytes:   int64(mb) * 1024 * 1024,
		StorageDriver:    strings.ToLower(env("STORAGE_DRIVER", "local")),
		LocalStoragePath: env("LOCAL_STORAGE_PATH", "./data/uploads"),
		AllowedOrigins:   splitCSV(env("ALLOWED_ORIGINS", "http://localhost:5500")),
		SeedAdminEmail:   env("SEED_ADMIN_EMAIL", ""),
		SeedAdminPass:    env("SEED_ADMIN_PASSWORD", ""),
		SeedAdminName:    env("SEED_ADMIN_NAME", "System Administrator"),
		S3Endpoint:       env("S3_ENDPOINT", ""),
		S3Region:         env("S3_REGION", "us-east-1"),
		S3Bucket:         env("S3_BUCKET", ""),
		S3AccessKey:      env("S3_ACCESS_KEY", ""),
		S3SecretKey:      env("S3_SECRET_KEY", ""),
		S3ForcePathStyle: envBool("S3_FORCE_PATH_STYLE", true),
		S3PublicBaseURL:  env("S3_PUBLIC_BASE_URL", ""),
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return value
}

func splitCSV(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
