package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	defaultUpgradeProject = "acp"
	defaultUpgradeCommand = "acp"
	defaultUpgradeRepo    = "doublepi123/acp"
	defaultGitHubBaseURL  = "https://github.com"
)

type upgradeOptions struct {
	Project       string
	Command       string
	Repo          string
	GitHubBaseURL string
	Tag           string
	TargetPath    string
	GOOS          string
	GOARCH        string
}

func runUpgrade() {
	opts, err := defaultUpgradeOptions()
	if err != nil {
		logFatalUpgrade(err)
	}

	if err := upgrade(opts); err != nil {
		logFatalUpgrade(err)
	}
}

func logFatalUpgrade(err error) {
	fmt.Fprintf(os.Stderr, "upgrade failed: %v\n", err)
	os.Exit(1)
}

func defaultUpgradeOptions() (upgradeOptions, error) {
	targetPath, err := currentExecutablePath()
	if err != nil {
		return upgradeOptions{}, err
	}

	return upgradeOptions{
		Project:       envOrDefault("PROJECT_NAME", defaultUpgradeProject),
		Command:       envOrDefault("COMMAND_NAME", defaultUpgradeCommand),
		Repo:          envOrDefault("REPO", defaultUpgradeRepo),
		GitHubBaseURL: envOrDefault("GITHUB_BASE_URL", defaultGitHubBaseURL),
		Tag:           envOrDefault("TAG", "latest"),
		TargetPath:    targetPath,
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
	}, nil
}

func currentExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("detecting current executable: %w", err)
	}

	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	return filepath.Abs(exe)
}

func upgrade(opts upgradeOptions) error {
	asset, err := releaseAssetName(opts.Project, opts.GOOS, opts.GOARCH)
	if err != nil {
		return err
	}

	url := releaseDownloadURL(opts.GitHubBaseURL, opts.Repo, opts.Tag, asset)
	fmt.Printf("Downloading %s %s for %s/%s...\n", opts.Project, opts.Tag, opts.GOOS, opts.GOARCH)
	fmt.Printf("Release asset: %s\n", url)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("downloading release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading release: HTTP %d", resp.StatusCode)
	}

	newBinary, err := extractBinary(resp.Body, opts.Command)
	if err != nil {
		return err
	}

	if err := replaceExecutable(opts.TargetPath, newBinary); err != nil {
		return err
	}

	fmt.Printf("Upgraded %s at: %s\n", opts.Command, opts.TargetPath)
	return nil
}

func releaseAssetName(project, goos, goarch string) (string, error) {
	osPart, err := releaseOS(goos)
	if err != nil {
		return "", err
	}
	archPart, err := releaseArch(goarch)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%s.tar.gz", project, osPart, archPart), nil
}

func releaseOS(goos string) (string, error) {
	switch goos {
	case "darwin", "linux":
		return goos, nil
	default:
		return "", fmt.Errorf("unsupported OS: %s", goos)
	}
}

func releaseArch(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "amd64", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}
}

func releaseDownloadURL(baseURL, repo, tag, asset string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	if tag == "" || tag == "latest" {
		return fmt.Sprintf("%s/%s/releases/latest/download/%s", baseURL, repo, asset)
	}
	return fmt.Sprintf("%s/%s/releases/download/%s/%s", baseURL, repo, tag, asset)
}

func extractBinary(r io.Reader, command string) ([]byte, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("reading release archive: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading release archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(header.Name) != command {
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("reading %s from release archive: %w", command, err)
		}
		return data, nil
	}

	return nil, fmt.Errorf("release archive did not contain %s", command)
}

func replaceExecutable(targetPath string, data []byte) error {
	dir := filepath.Dir(targetPath)
	info, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("checking current executable: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".acp-upgrade-*")
	if err != nil {
		return fmt.Errorf("creating temporary binary in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temporary binary: %w", err)
	}
	if err := tmp.Chmod(info.Mode().Perm() | 0o755); err != nil {
		tmp.Close()
		return fmt.Errorf("marking temporary binary executable: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temporary binary: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("replacing %s: %w", targetPath, err)
	}

	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
