package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Store struct {
	root string
}

func New(root string) Store {
	return Store{root: root}
}

func DefaultRoot() string {
	if pluginData := os.Getenv("PLUGIN_DATA"); pluginData != "" {
		return filepath.Join(pluginData, "state")
	}
	if configured := os.Getenv("SIGNPOSTS_STATE_DIR"); configured != "" {
		return configured
	}
	cache, err := os.UserCacheDir()
	if err == nil {
		return filepath.Join(cache, "signposts", "state")
	}
	return filepath.Join(os.TempDir(), "signposts", "state")
}

func (store Store) Reserve(repoRoot, sessionID, ruleID string) (bool, error) {
	directory := store.sessionDirectory(repoRoot, sessionID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return false, err
	}
	marker := filepath.Join(directory, digest(ruleID)+".rule")
	file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := file.WriteString(ruleID + "\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(marker)
		return false, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(marker)
		return false, err
	}
	return true, nil
}

func (store Store) Emitted(repoRoot, sessionID string) ([]string, error) {
	directory := store.sessionDirectory(repoRoot, sessionID)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	ids := []string{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".rule" {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		if id := strings.TrimSpace(string(contents)); id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (store Store) Clear(repoRoot, sessionID string) error {
	directory := store.sessionDirectory(repoRoot, sessionID)
	if directory == store.root || directory == "." || directory == string(filepath.Separator) {
		return errors.New("refusing to clear an unsafe state path")
	}
	return os.RemoveAll(directory)
}

func (store Store) sessionDirectory(repoRoot, sessionID string) string {
	return filepath.Join(store.root, digest(repoRoot), digest(sessionID))
}

func digest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
