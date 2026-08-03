package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// userDotEnvPath returns the location of the user-level .env file that
// provider credentials (e.g. FANARTTV_API_KEY) are loaded from. This is
// deliberately outside any library's .musicvideo-manager/ directory --
// per ADR 0008/0002, a library's per-library state travels with the
// library (backups, copies, sharing), and a credential shouldn't travel
// with it. One file here covers every library the tool is pointed at.
func userDotEnvPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "musicvideo-manager", ".env"), nil
}

// loadDotEnv reads simple KEY=VALUE lines from path into the process
// environment, skipping blank lines and lines starting with '#'. A key
// already set in the environment is left untouched -- the real
// environment always wins over the file. A missing file is not an error;
// the .env file is optional.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)

		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	return scanner.Err()
}
