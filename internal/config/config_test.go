package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigurationValidation(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PORT", "8080")
	t.Setenv("DB_PATH", "test.db")
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 32))
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("SESSION_TTL", "2h")
	c, err := Load()
	if err != nil || !c.CookieSecure || c.SessionTTL != 2*time.Hour {
		t.Fatal(c, err)
	}
	for _, tc := range []struct{ key, value string }{{"PORT", ""}, {"PORT", "70000"}, {"DB_PATH", ""}, {"SESSION_SECRET", "short"}, {"SESSION_SECRET", "replace-with-a-random-secret-of-at-least-32-bytes"}, {"COOKIE_SECURE", "maybe"}, {"SESSION_TTL", "0s"}, {"SESSION_TTL", "9999h"}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}
func TestDotenvAndEnvironmentPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, key := range []string{"PORT", "DB_PATH", "SESSION_SECRET", "COOKIE_SECURE", "SESSION_TTL"} {
		old, ok := os.LookupEnv(key)
		os.Unsetenv(key)
		t.Cleanup(func() {
			if ok {
				os.Setenv(key, old)
			} else {
				os.Unsetenv(key)
			}
		})
	}
	content := "PORT=8099\nDB_PATH=local.db\nSESSION_SECRET=" + strings.Repeat("r", 32) + "\nCOOKIE_SECURE=false\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORT", "8100")
	c, err := Load()
	if err != nil || c.Port != "8100" || c.DBPath != "local.db" {
		t.Fatal(c, err)
	}
}
