package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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

	// Fetch latest release tag if not specified
	if opts.Tag == "" || opts.Tag == "latest" {
		client := &http.Client{Timeout: 30 * 1000000000}
		latestTag, err := latestReleaseTag(context.Background(), client, opts.GitHubBaseURL, opts.Repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not fetch latest release: %v\n", err)
		} else {
			opts.Tag = latestTag
			if shouldSkipUpgrade(version, latestTag) {
				fmt.Printf("acp is already up to date (%s)\n", version)
				return
			}
			if strings.TrimSpace(version) != "" && version != "dev" {
				fmt.Printf("current: %s\n", version)
			}
			fmt.Printf("latest: %s\n", latestTag)
		}
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

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("downloading release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading release: HTTP %d", resp.StatusCode)
	}

	binaryName := opts.Command
	if opts.GOOS == "windows" {
		binaryName += ".exe"
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading release body: %w", err)
	}

	if err := verifyChecksum(opts, asset, bodyBytes); err != nil {
		return err
	}

	newBinary, err := extractBinary(bytes.NewReader(bodyBytes), int64(len(bodyBytes)), binaryName, opts.GOOS)
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
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("%s-%s-%s.%s", project, osPart, archPart, ext), nil
}

func releaseOS(goos string) (string, error) {
	switch goos {
	case "darwin", "linux", "windows":
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

func extractBinary(r io.ReaderAt, size int64, command string, goos string) ([]byte, error) {
	if goos == "windows" {
		return extractBinaryZip(r, size, command)
	}
	return extractBinaryTarGz(io.NewSectionReader(r, 0, size), command)
}

func extractBinaryTarGz(r io.Reader, command string) ([]byte, error) {
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

func extractBinaryZip(r io.ReaderAt, size int64, command string) ([]byte, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("reading release archive: %w", err)
	}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if filepath.Base(f.Name) != command {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("opening %s in archive: %w", command, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("reading %s from release archive: %w", command, err)
		}
		return data, nil
	}

	return nil, fmt.Errorf("release archive did not contain %s", command)
}

func verifyChecksum(opts upgradeOptions, asset string, data []byte) error {
	checksumAsset := "checksums.txt"
	checksumURL := releaseDownloadURL(opts.GitHubBaseURL, opts.Repo, opts.Tag, checksumAsset)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(checksumURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checksums.txt not available (HTTP %d)", resp.StatusCode)
	}

	checksumData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read checksums: %w", err)
	}

	hash := sha256.Sum256(data)
	actual := hex.EncodeToString(hash[:])

	expected := ""
	for _, line := range strings.Split(strings.TrimSpace(string(checksumData)), "\n") {
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[1]) == asset {
			expected = strings.TrimSpace(parts[0])
			break
		}
	}

	if expected == "" {
		return fmt.Errorf("%s not found in checksums.txt", asset)
	}

	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s:\n  expected: %s\n  actual:   %s", asset, expected, actual)
	}

	fmt.Println("Checksum verified.")
	return nil
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
	cleanup := func() { os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("writing temporary binary: %w", err)
	}
	if err := tmp.Chmod(info.Mode().Perm() | 0o755); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("marking temporary binary executable: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("closing temporary binary: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		cleanup()
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


// shouldSkipUpgrade returns true if the current version is already at or above the target.
func shouldSkipUpgrade(current, target string) bool {
	current = strings.TrimSpace(current)
	target = strings.TrimSpace(target)
	if current == "" || current == "dev" || target == "" {
		return false
	}
	cmp, ok := compareReleaseVersions(current, target)
	if !ok {
		return current == target
	}
	return cmp >= 0
}

// compareReleaseVersions compares two semver version strings.
// Returns -1, 0, or 1 if current is less than, equal to, or greater than target.
func compareReleaseVersions(current, target string) (int, bool) {
	currentVersion, ok := parseReleaseVersion(current)
	if !ok {
		return 0, false
	}
	targetVersion, ok := parseReleaseVersion(target)
	if !ok {
		return 0, false
	}
	for i := 0; i < len(currentVersion.numbers) || i < len(targetVersion.numbers); i++ {
		left := 0
		if i < len(currentVersion.numbers) {
			left = currentVersion.numbers[i]
		}
		right := 0
		if i < len(targetVersion.numbers) {
			right = targetVersion.numbers[i]
		}
		switch {
		case left > right:
			return 1, true
		case left < right:
			return -1, true
		}
	}
	switch {
	case currentVersion.preRelease == "" && targetVersion.preRelease != "":
		return 1, true
	case currentVersion.preRelease != "" && targetVersion.preRelease == "":
		return -1, true
	case currentVersion.preRelease > targetVersion.preRelease:
		return 1, true
	case currentVersion.preRelease < targetVersion.preRelease:
		return -1, true
	default:
		return 0, true
	}
}

type releaseVersion struct {
	numbers    []int
	preRelease string
}

func parseReleaseVersion(value string) (releaseVersion, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	value = strings.TrimPrefix(value, "V")
	if value == "" {
		return releaseVersion{}, false
	}
	if plus := strings.Index(value, "+"); plus >= 0 {
		value = value[:plus]
	}
	preRelease := ""
	if dash := strings.Index(value, "-"); dash >= 0 {
		preRelease = value[dash+1:]
		value = value[:dash]
	}
	parts := strings.Split(value, ".")
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return releaseVersion{}, false
		}
		n := 0
		for i := 0; i < len(part); i++ {
			c := part[i]
			if c < '0' || c > '9' {
				return releaseVersion{}, false
			}
			n = n*10 + int(c-'0')
		}
		numbers = append(numbers, n)
	}
	if len(numbers) == 0 {
		return releaseVersion{}, false
	}
	return releaseVersion{numbers: numbers, preRelease: preRelease}, true
}

func latestReleaseTag(ctx context.Context, client *http.Client, baseURL, repo string) (string, error) {
	latestURL := fmt.Sprintf("%s/%s/releases/latest", strings.TrimRight(strings.TrimSpace(baseURL), "/"), strings.Trim(strings.TrimSpace(repo), "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "acp/"+version)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("check latest release failed: %s", resp.Status)
	}
	tag := tagFromReleaseURL(resp.Request.URL)
	if tag == "" {
		return "", fmt.Errorf("could not determine latest release tag from %s", resp.Request.URL)
	}
	return tag, nil
}

func tagFromReleaseURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] != "tag" {
			continue
		}
		tag, err := url.PathUnescape(parts[i+1])
		if err != nil {
			return parts[i+1]
		}
		return tag
	}
	return ""
}
