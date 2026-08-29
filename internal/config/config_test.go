package config

import "testing"

func TestLoadAdminCredentials(t *testing.T) {
	t.Setenv("OPENSWIFTSCALE_API_KEYS", "client-key")
	t.Setenv("OPENSWIFTSCALE_ADMIN_USERNAME", "console-admin")
	t.Setenv("OPENSWIFTSCALE_ADMIN_PASSWORD", "console-password")
	t.Setenv("OPENSWIFTSCALE_MANAGEMENT_TOKEN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminUsername != "console-admin" || cfg.AdminPassword != "console-password" {
		t.Fatalf("unexpected administrator credentials: username=%q password=%q", cfg.AdminUsername, cfg.AdminPassword)
	}
}

func TestLoadPublicURL(t *testing.T) {
	t.Setenv("OPENSWIFTSCALE_API_KEYS", "client-key")
	t.Setenv("OPENSWIFTSCALE_ADMIN_PASSWORD", "console-password")
	t.Setenv("OPENSWIFTSCALE_PUBLIC_URL", "http://192.168.1.25:8080/")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicURL != "http://192.168.1.25:8080" {
		t.Fatalf("unexpected public URL: %q", cfg.PublicURL)
	}
}

func TestLoadSpecificLANListenAddress(t *testing.T) {
	t.Setenv("OPENSWIFTSCALE_API_KEYS", "client-key")
	t.Setenv("OPENSWIFTSCALE_ADMIN_PASSWORD", "console-password")
	t.Setenv("OPENSWIFTSCALE_LAN_LISTEN_ADDR", "192.168.1.25:8080")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LANListenAddr != "192.168.1.25:8080" {
		t.Fatalf("unexpected LAN listen address: %q", cfg.LANListenAddr)
	}
}

func TestRejectsUnspecifiedLANListenAddress(t *testing.T) {
	t.Setenv("OPENSWIFTSCALE_API_KEYS", "client-key")
	t.Setenv("OPENSWIFTSCALE_ADMIN_PASSWORD", "console-password")
	t.Setenv("OPENSWIFTSCALE_LAN_LISTEN_ADDR", "0.0.0.0:8080")
	if _, err := Load(); err == nil {
		t.Fatal("expected unspecified LAN listen address to be rejected")
	}
}

func TestLoadMigratesManagementTokenToAdminPassword(t *testing.T) {
	t.Setenv("OPENSWIFTSCALE_API_KEYS", "client-key")
	t.Setenv("OPENSWIFTSCALE_ADMIN_USERNAME", "")
	t.Setenv("OPENSWIFTSCALE_ADMIN_PASSWORD", "")
	t.Setenv("OPENSWIFTSCALE_MANAGEMENT_TOKEN", "legacy-management-token")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminUsername != "admin" || cfg.AdminPassword != "legacy-management-token" {
		t.Fatalf("legacy credentials were not migrated: username=%q password=%q", cfg.AdminUsername, cfg.AdminPassword)
	}
}
