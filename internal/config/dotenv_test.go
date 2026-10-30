package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDotEnvLine(t *testing.T) {
	cases := []struct {
		line      string
		key       string
		value     string
		ok        bool
		wantError bool
	}{
		{line: "", ok: false},
		{line: "   # comment", ok: false},
		{line: "APP_ENV=dev", key: "APP_ENV", value: "dev", ok: true},
		{line: "QUOTED=\"a b c\"", key: "QUOTED", value: "a b c", ok: true},
		{line: "SINGLE='x=1'", key: "SINGLE", value: "x=1", ok: true},
		{line: "URL=postgres://u:p@h/db?sslmode=disable", key: "URL", value: "postgres://u:p@h/db?sslmode=disable", ok: true},
		{line: "noequals", wantError: true},
		{line: "=value", wantError: true},
	}
	for _, c := range cases {
		key, value, ok, err := parseDotEnvLine(c.line)
		if c.wantError {
			if err == nil {
				t.Errorf("parseDotEnvLine(%q): want error", c.line)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDotEnvLine(%q): %v", c.line, err)
			continue
		}
		if key != c.key || value != c.value || ok != c.ok {
			t.Errorf("parseDotEnvLine(%q) = %q, %q, %v", c.line, key, value, ok)
		}
	}
}

func TestLoadDotEnvDoesNotOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("DOTENV_TEST_KEEP=from-file\nDOTENV_TEST_NEW=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DOTENV_TEST_KEEP", "already-set")
	os.Unsetenv("DOTENV_TEST_NEW")
	t.Cleanup(func() { os.Unsetenv("DOTENV_TEST_NEW") })

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("DOTENV_TEST_KEEP"); got != "already-set" {
		t.Errorf("existing value overwritten: %q", got)
	}
	if got := os.Getenv("DOTENV_TEST_NEW"); got != "from-file" {
		t.Errorf("new value = %q, want from-file", got)
	}
}

func TestLoadDotEnvMissingFileIsFine(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "absent.env")); err != nil {
		t.Fatalf("missing file: %v", err)
	}
}
