// Package local owns the per-user state, atomic writes and process locks.
package local

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"

	"github.com/Kydaix/Tripo-MCP/internal/fault"
)

func Root() (string, error) {
	if p := os.Getenv("TRIPO_MCP_HOME"); p != "" {
		if !filepath.IsAbs(p) {
			return "", fault.New("INVALID_ARGUMENT", "TRIPO_MCP_HOME doit être un chemin absolu.")
		}
		return filepath.Abs(p)
	}
	p, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, "Tripo-MCP"), nil
}

func Write(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replace(f.Name(), path)
}

func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return Write(path, append(b, '\n'))
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func ValidID(id string) bool { return safeID.MatchString(id) }

func JobPath(root, id string) (string, error) {
	if !ValidID(id) {
		return "", fault.New("INVALID_ARGUMENT", "Identifiant de tâche invalide.")
	}
	return filepath.Join(root, "jobs", id+".json"), nil
}
