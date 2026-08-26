package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr     string
	PublicURL      string
	AdminUsername  string
	AdminPassword  string
	APIKeys        []string
	AuthDisabled   bool
	DatabasePath   string
	MasterKeyPath  string
	MasterKey      string
	CatalogPath    string
	RequestTimeout time.Duration
	MaxBodyBytes   int64
	LogPrompts     bool
	RateLimitRPM   int
	Concurrency    int
}

func Load() (Config, error) {
	adminPassword := secretOrEnv("OPENSWIFTSCALE_ADMIN_PASSWORD")
	if adminPassword == "" {
		// Keep existing installations working while they migrate their secret file.
		adminPassword = secretOrEnv("OPENSWIFTSCALE_MANAGEMENT_TOKEN")
	}
	apiKeys := splitCSV(secretOrEnv("OPENSWIFTSCALE_API_KEYS"))
	cfg := Config{
		ListenAddr:     env("OPENSWIFTSCALE_LISTEN_ADDR", ":8080"),
		PublicURL:      strings.TrimRight(env("OPENSWIFTSCALE_PUBLIC_URL", ""), "/"),
		AdminUsername:  env("OPENSWIFTSCALE_ADMIN_USERNAME", "admin"),
		AdminPassword:  adminPassword,
		APIKeys:        apiKeys,
		DatabasePath:   env("OPENSWIFTSCALE_DATABASE_PATH", "/data/openswiftscale.db"),
		MasterKeyPath:  env("OPENSWIFTSCALE_MASTER_KEY_PATH", "/data/keys/master.key"),
		MasterKey:      secretOrEnv("OPENSWIFTSCALE_MASTER_KEY"),
		CatalogPath:    env("OPENSWIFTSCALE_CATALOG_PATH", "/etc/openswiftscale/catalog.yaml"),
		RequestTimeout: 120 * time.Second,
		MaxBodyBytes:   8 << 20,
		RateLimitRPM:   60,
		Concurrency:    8,
	}
	if cfg.PublicURL != "" {
		parsed, parseErr := url.Parse(cfg.PublicURL)
		if parseErr != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return Config{}, fmt.Errorf("OPENSWIFTSCALE_PUBLIC_URL must be an http or https origin")
		}
	}
	var err error
	if cfg.AuthDisabled, err = parseBool("OPENSWIFTSCALE_AUTH_DISABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.LogPrompts, err = parseBool("OPENSWIFTSCALE_LOG_PROMPTS", false); err != nil {
		return Config{}, err
	}
	if seconds, err := strconv.Atoi(env("OPENSWIFTSCALE_REQUEST_TIMEOUT_SECONDS", "120")); err != nil || seconds < 1 {
		return Config{}, fmt.Errorf("OPENSWIFTSCALE_REQUEST_TIMEOUT_SECONDS must be a positive integer")
	} else {
		cfg.RequestTimeout = time.Duration(seconds) * time.Second
	}
	if bytes, err := strconv.ParseInt(env("OPENSWIFTSCALE_MAX_BODY_BYTES", "8388608"), 10, 64); err != nil || bytes < 1024 {
		return Config{}, fmt.Errorf("OPENSWIFTSCALE_MAX_BODY_BYTES must be at least 1024")
	} else {
		cfg.MaxBodyBytes = bytes
	}
	if cfg.RateLimitRPM, err = parseNonNegativeInt("OPENSWIFTSCALE_RATE_LIMIT_RPM", 60); err != nil {
		return Config{}, err
	}
	if cfg.Concurrency, err = parseNonNegativeInt("OPENSWIFTSCALE_CONCURRENCY", 8); err != nil {
		return Config{}, err
	}
	if !cfg.AuthDisabled && len(cfg.APIKeys) == 0 {
		return Config{}, fmt.Errorf("set OPENSWIFTSCALE_API_KEYS or explicitly set OPENSWIFTSCALE_AUTH_DISABLED=true")
	}
	if cfg.AdminPassword == "" {
		return Config{}, fmt.Errorf("OPENSWIFTSCALE_ADMIN_PASSWORD is required")
	}
	return cfg, nil
}

func parseNonNegativeInt(name string, fallback int) (int, error) {
	value, err := strconv.Atoi(env(name, strconv.Itoa(fallback)))
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}

func ProviderKeyEnv(provider string) string {
	provider = strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(provider))
	return secretOrEnv("OPENSWIFTSCALE_" + provider + "_API_KEY")
}

func secretOrEnv(name string) string {
	if file := strings.TrimSpace(os.Getenv(name + "_FILE")); file != "" {
		if data, err := os.ReadFile(file); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return strings.TrimSpace(os.Getenv(name))
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func parseBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}
