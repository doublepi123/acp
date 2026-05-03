package main

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
)

func prepareIsolatedCodexHome() (string, func(), error) {
	root, err := acpCacheDir()
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", nil, err
	}

	dir, err := os.MkdirTemp(root, "codex-home-")
	if err != nil {
		return "", nil, err
	}

	cleanup := func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("failed to clean temporary Codex home %s: %v", dir, err)
		}
	}

	if err := copyCodexConfig(dir); err != nil {
		cleanup()
		return "", nil, err
	}

	return dir, cleanup, nil
}

func acpCacheDir() (string, error) {
	if dir := os.Getenv("ACP_CACHE_DIR"); dir != "" {
		return dir, nil
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "acp"), nil
}

func copyCodexConfig(destHome string) error {
	sourceHome := os.Getenv("CODEX_HOME")
	if sourceHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		sourceHome = filepath.Join(home, ".codex")
	}

	sourcePath := filepath.Join(sourceHome, "config.toml")
	destPath := filepath.Join(destHome, "config.toml")
	if err := copyFile(sourcePath, destPath, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func copyFile(sourcePath, destPath string, perm os.FileMode) error {
	in, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
