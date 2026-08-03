package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempEnvFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write temp .env: %v", err)
	}
	return path
}

func TestLoadDotEnvSetsVariablesFromFile(t *testing.T) {
	t.Setenv("DOTENV_TEST_FOO", "")
	os.Unsetenv("DOTENV_TEST_FOO")
	t.Setenv("DOTENV_TEST_BAZ", "")
	os.Unsetenv("DOTENV_TEST_BAZ")

	path := writeTempEnvFile(t, "DOTENV_TEST_FOO=bar\nDOTENV_TEST_BAZ=qux\n")

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}
	if got := os.Getenv("DOTENV_TEST_FOO"); got != "bar" {
		t.Errorf("DOTENV_TEST_FOO: got %q, want %q", got, "bar")
	}
	if got := os.Getenv("DOTENV_TEST_BAZ"); got != "qux" {
		t.Errorf("DOTENV_TEST_BAZ: got %q, want %q", got, "qux")
	}
}

func TestLoadDotEnvSkipsBlankLinesCommentsAndStripsQuotes(t *testing.T) {
	t.Setenv("DOTENV_TEST_QUOTED", "")
	os.Unsetenv("DOTENV_TEST_QUOTED")

	path := writeTempEnvFile(t, "\n# a comment\n\nDOTENV_TEST_QUOTED=\"quoted value\"\n")

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}
	if got := os.Getenv("DOTENV_TEST_QUOTED"); got != "quoted value" {
		t.Errorf("DOTENV_TEST_QUOTED: got %q, want %q", got, "quoted value")
	}
}

func TestLoadDotEnvDoesNotOverrideExistingEnvVar(t *testing.T) {
	t.Setenv("DOTENV_TEST_EXISTING", "from-real-env")

	path := writeTempEnvFile(t, "DOTENV_TEST_EXISTING=from-file\n")

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}
	if got := os.Getenv("DOTENV_TEST_EXISTING"); got != "from-real-env" {
		t.Errorf("expected the real environment to win, got %q", got)
	}
}

func TestLoadDotEnvMissingFileIsNotAnError(t *testing.T) {
	if err := loadDotEnv(filepath.Join(t.TempDir(), "does-not-exist.env")); err != nil {
		t.Fatalf("expected a missing .env file to be silently ignored, got %v", err)
	}
}

func TestUserDotEnvPathLivesUnderHomeConfigDir(t *testing.T) {
	t.Setenv("HOME", "/fake/home")

	path, err := userDotEnvPath()
	if err != nil {
		t.Fatalf("userDotEnvPath: %v", err)
	}
	want := filepath.Join("/fake/home", ".config", "musicvideo-manager", ".env")
	if path != want {
		t.Errorf("got %q, want %q", path, want)
	}
}
