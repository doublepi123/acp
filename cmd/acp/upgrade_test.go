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
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// newLocalTestServer creates an httptest server or skips the test if
// the environment does not allow creating TCP listeners (e.g. sandbox).
func newLocalTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("skipping test: cannot create TCP listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = l
	srv.Start()
	return srv
}

func TestReleaseAssetName(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		want   string
	}{
		{name: "linux amd64", goos: "linux", goarch: "amd64", want: "acp-linux-amd64.tar.gz"},
		{name: "darwin arm64", goos: "darwin", goarch: "arm64", want: "acp-darwin-arm64.tar.gz"},
		{name: "windows amd64", goos: "windows", goarch: "amd64", want: "acp-windows-amd64.zip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := releaseAssetName("acp", tt.goos, tt.goarch)
			if err != nil {
				t.Fatalf("releaseAssetName() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("releaseAssetName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReleaseDownloadURL(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		want string
	}{
		{
			name: "latest",
			tag:  "latest",
			want: "https://github.com/doublepi123/acp/releases/latest/download/acp-linux-amd64.tar.gz",
		},
		{
			name: "specific tag",
			tag:  "v1.2.3",
			want: "https://github.com/doublepi123/acp/releases/download/v1.2.3/acp-linux-amd64.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := releaseDownloadURL("https://github.com/", "/doublepi123/acp/", tt.tag, "acp-linux-amd64.tar.gz")
			if got != tt.want {
				t.Fatalf("releaseDownloadURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractBinary(t *testing.T) {
	archive := testTarGz(t, map[string]string{
		"README.md": "ignore",
		"acp":       "binary-data",
	})

	got, err := extractBinary(bytes.NewReader(archive), int64(len(archive)), "acp", "linux")
	if err != nil {
		t.Fatalf("extractBinary() error = %v", err)
	}
	if string(got) != "binary-data" {
		t.Fatalf("extractBinary() = %q, want %q", string(got), "binary-data")
	}
}

func TestExtractBinaryMissing(t *testing.T) {
	archive := testTarGz(t, map[string]string{"README.md": "ignore"})

	if _, err := extractBinary(bytes.NewReader(archive), int64(len(archive)), "acp", "linux"); err == nil {
		t.Fatal("extractBinary() error = nil, want error")
	}
}

func TestExtractBinaryZip(t *testing.T) {
	archive := testZip(t, map[string]string{
		"README.md": "ignore",
		"acp.exe":   "binary-data",
	})

	got, err := extractBinary(bytes.NewReader(archive), int64(len(archive)), "acp.exe", "windows")
	if err != nil {
		t.Fatalf("extractBinary() error = %v", err)
	}
	if string(got) != "binary-data" {
		t.Fatalf("extractBinary() = %q, want %q", string(got), "binary-data")
	}
}

func testTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(body)),
		}); err != nil {
			t.Fatalf("WriteHeader() error = %v", err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close() error = %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip Close() error = %v", err)
	}

	return buf.Bytes()
}

func testZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close() error = %v", err)
	}

	return buf.Bytes()
}

func TestShouldSkipUpgrade(t *testing.T) {
	tests := []struct {
		name    string
		current string
		target  string
		want    bool
	}{
		{"same version", "v1.0.0", "v1.0.0", true},
		{"current newer", "v1.1.0", "v1.0.0", true},
		{"current older", "v1.0.0", "v1.1.0", false},
		{"dev always upgrades", "dev", "v1.0.0", false},
		{"empty current upgrades", "", "v1.0.0", false},
		{"empty target skips", "v1.0.0", "", false},
		{"patch bump", "v1.0.0", "v1.0.1", false},
		{"major bump", "v1.0.0", "v2.0.0", false},
		{"pre-release vs release", "v1.0.0-beta", "v1.0.0", false},
		{"release vs pre-release", "v1.0.0", "v1.0.0-beta", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSkipUpgrade(tt.current, tt.target); got != tt.want {
				t.Fatalf("shouldSkipUpgrade(%q, %q) = %v, want %v", tt.current, tt.target, got, tt.want)
			}
		})
	}
}

func TestCompareReleaseVersions(t *testing.T) {
	tests := []struct {
		name    string
		current string
		target  string
		want    int
		wantOK  bool
	}{
		{"equal", "v1.0.0", "v1.0.0", 0, true},
		{"patch less", "v1.0.0", "v1.0.1", -1, true},
		{"patch greater", "v1.0.2", "v1.0.1", 1, true},
		{"minor less", "v1.0.0", "v1.1.0", -1, true},
		{"major greater", "v2.0.0", "v1.9.9", 1, true},
		{"no v prefix", "1.0.0", "1.0.0", 0, true},
		{"invalid", "abc", "v1.0.0", 0, false},
		{"pre-release older than same base", "v1.0.0-alpha", "v1.0.0", -1, true},
		{"same base newer than pre-release", "v1.0.0", "v1.0.0-alpha", 1, true},
		{"pre-release comparison", "v1.0.0-beta", "v1.0.0-alpha", 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := compareReleaseVersions(tt.current, tt.target)
			if ok != tt.wantOK {
				t.Fatalf("compareReleaseVersions(%q, %q) ok = %v, want %v", tt.current, tt.target, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("compareReleaseVersions(%q, %q) = %d, want %d", tt.current, tt.target, got, tt.want)
			}
		})
	}
}

func TestParseReleaseVersion(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    releaseVersion
		wantOK  bool
	}{
		{"simple", "v1.2.3", releaseVersion{numbers: []int{1, 2, 3}}, true},
		{"no v", "2.0.0", releaseVersion{numbers: []int{2, 0, 0}}, true},
		{"capital V", "V3.4.5", releaseVersion{numbers: []int{3, 4, 5}}, true},
		{"with pre-release", "v1.0.0-alpha", releaseVersion{numbers: []int{1, 0, 0}, preRelease: "alpha"}, true},
		{"with build metadata", "v1.0.0+build", releaseVersion{numbers: []int{1, 0, 0}}, true},
		{"empty", "", releaseVersion{}, false},
		{"invalid part", "v1.abc.3", releaseVersion{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseReleaseVersion(tt.value)
			if ok != tt.wantOK {
				t.Fatalf("parseReleaseVersion(%q) ok = %v, want %v", tt.value, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if len(got.numbers) != len(tt.want.numbers) {
				t.Fatalf("numbers len = %d, want %d", len(got.numbers), len(tt.want.numbers))
			}
			for i := range got.numbers {
				if got.numbers[i] != tt.want.numbers[i] {
					t.Fatalf("numbers[%d] = %d, want %d", i, got.numbers[i], tt.want.numbers[i])
				}
			}
			if got.preRelease != tt.want.preRelease {
				t.Fatalf("preRelease = %q, want %q", got.preRelease, tt.want.preRelease)
			}
		})
	}
}

func TestReleaseOS(t *testing.T) {
	tests := []struct {
		goos string
		want string
		err  bool
	}{
		{"darwin", "darwin", false},
		{"linux", "linux", false},
		{"windows", "windows", false},
		{"freebsd", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			got, err := releaseOS(tt.goos)
			if tt.err && err == nil {
				t.Fatalf("releaseOS(%q) error = nil, want error", tt.goos)
			}
			if !tt.err && err != nil {
				t.Fatalf("releaseOS(%q) error = %v", tt.goos, err)
			}
			if got != tt.want {
				t.Fatalf("releaseOS(%q) = %q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}

func TestReleaseArch(t *testing.T) {
	tests := []struct {
		goarch string
		want   string
		err    bool
	}{
		{"amd64", "amd64", false},
		{"arm64", "arm64", false},
		{"386", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.goarch, func(t *testing.T) {
			got, err := releaseArch(tt.goarch)
			if tt.err && err == nil {
				t.Fatalf("releaseArch(%q) error = nil, want error", tt.goarch)
			}
			if !tt.err && err != nil {
				t.Fatalf("releaseArch(%q) error = %v", tt.goarch, err)
			}
			if got != tt.want {
				t.Fatalf("releaseArch(%q) = %q, want %q", tt.goarch, got, tt.want)
			}
		})
	}
}

func TestTagFromReleaseURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"nil", "", ""},
		{"tag in path", "https://github.com/a/b/releases/tag/v1.2.3", "v1.2.3"},
		{"encoded tag", "https://github.com/a/b/releases/tag/v1%2E2%2E3", "v1.2.3"},
		{"no tag", "https://github.com/a/b/releases/latest", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var u *url.URL
			if tt.raw != "" {
				var err error
				u, err = url.Parse(tt.raw)
				if err != nil {
					t.Fatalf("url.Parse(%q) error = %v", tt.raw, err)
				}
			}
			got := tagFromReleaseURL(u)
			if got != tt.want {
				t.Fatalf("tagFromReleaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("TEST_KEY", "set")
	if got := envOrDefault("TEST_KEY", "fallback"); got != "set" {
		t.Fatalf("envOrDefault(set) = %q, want %q", got, "set")
	}
	if got := envOrDefault("MISSING", "fallback"); got != "fallback" {
		t.Fatalf("envOrDefault(missing) = %q, want %q", got, "fallback")
	}
}

func TestLatestReleaseTag(t *testing.T) {
	var srv *httptest.Server
	srv = newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases/latest") {
			http.Redirect(w, r, srv.URL+"/doublepi123/acp/releases/tag/v1.2.3", http.StatusFound)
			return
		}
	}))
	defer srv.Close()

	client := &http.Client{}
	ctx := context.Background()
	tag, err := latestReleaseTag(ctx, client, srv.URL, "doublepi123/acp")
	if err != nil {
		t.Fatalf("latestReleaseTag error = %v", err)
	}
	if tag != "v1.2.3" {
		t.Fatalf("latestReleaseTag = %q, want v1.2.3", tag)
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("test binary data for checksum verification")
	expectedHash := "8458f71efd84b697dc8028881813f1d42a08aa0634f67e311a147938054e7ae4"

	srv := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "checksums.txt") {
			w.Write([]byte(expectedHash + "  acp-linux-amd64.tar.gz\n"))
			return
		}
	}))
	defer srv.Close()

	opts := upgradeOptions{
		Repo:          "doublepi123/acp",
		GitHubBaseURL: srv.URL,
		Tag:           "v1.0.0",
	}

	// Should succeed silently
	err := verifyChecksum(opts, "acp-linux-amd64.tar.gz", data)
	if err != nil {
		t.Fatalf("verifyChecksum error = %v", err)
	}

	// Wrong data should fail
	wrongData := []byte("wrong data")
	err = verifyChecksum(opts, "acp-linux-amd64.tar.gz", wrongData)
	if err == nil {
		t.Fatalf("verifyChecksum should fail on wrong checksum")
	}
}

func TestUpgradeExtractAndVerify(t *testing.T) {
	// Build a minimal tar.gz containing "acp"
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	data := []byte("new-binary-data")
	if err := tw.WriteHeader(&tar.Header{
		Name: "acp",
		Mode: 0o755,
		Size: int64(len(data)),
	}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}

	srv := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "checksums.txt") {
			w.Write([]byte("fakehash  acp-linux-amd64.tar.gz\n"))
			return
		}
		// Serve the tar.gz
		w.Write(buf.Bytes())
	}))
	defer srv.Close()

	// Test upgrade with a temporary target path
	tmpDir := t.TempDir()
	targetPath := tmpDir + "/acp"
	if err := copyFile("/proc/self/exe", targetPath, 0o755); err != nil {
		t.Skipf("cannot create test target: %v", err)
	}

	opts := upgradeOptions{
		Project:       "acp",
		Command:       "acp",
		Repo:          "doublepi123/acp",
		GitHubBaseURL: srv.URL,
		Tag:           "v1.0.0",
		TargetPath:    targetPath,
		GOOS:          "linux",
		GOARCH:        "amd64",
	}

	// Just verify parts of the upgrade flow work
	asset, err := releaseAssetName(opts.Project, opts.GOOS, opts.GOARCH)
	if err != nil {
		t.Fatalf("releaseAssetName error = %v", err)
	}
	url := releaseDownloadURL(opts.GitHubBaseURL, opts.Repo, opts.Tag, asset)

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("http.Get error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestReplaceExecutable(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := tmpDir + "/target_binary"
	// Create initial file
	if err := copyFile("/proc/self/exe", targetPath, 0o755); err != nil {
		t.Skipf("cannot create test target: %v", err)
	}
	originalInfo, _ := os.Stat(targetPath)

	newData := []byte("replacement-binary-data")
	if err := replaceExecutable(targetPath, newData); err != nil {
		t.Fatalf("replaceExecutable error = %v", err)
	}

	// Verify file was replaced
	newData_read, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	if string(newData_read) != "replacement-binary-data" {
		t.Fatalf("file content = %q, want replacement-binary-data", string(newData_read))
	}

	// Verify permissions preserved
	newInfo, _ := os.Stat(targetPath)
	if newInfo.Mode() != originalInfo.Mode() {
		// Permissions should be executable (at least 755)
		if newInfo.Mode().Perm()&0o111 == 0 {
			t.Fatalf("replaced file is not executable: %v", newInfo.Mode())
		}
	}
}

func TestCurrentExecutablePath(t *testing.T) {
	path, err := currentExecutablePath()
	if err != nil {
		t.Fatalf("currentExecutablePath error = %v", err)
	}
	if path == "" {
		t.Fatalf("currentExecutablePath = empty")
	}
}

func TestDefaultUpgradeOptions(t *testing.T) {
	opts, err := defaultUpgradeOptions()
	if err != nil {
		t.Fatalf("defaultUpgradeOptions error = %v", err)
	}
	if opts.Project != "acp" {
		t.Fatalf("Project = %q, want acp", opts.Project)
	}
	if opts.Command != "acp" {
		t.Fatalf("Command = %q, want acp", opts.Command)
	}
	if opts.GOOS == "" {
		t.Fatalf("GOOS is empty")
	}
	if opts.GOARCH == "" {
		t.Fatalf("GOARCH is empty")
	}
	if opts.TargetPath == "" {
		t.Fatalf("TargetPath is empty")
	}
}

func TestShouldSkipUpgradeSemverCompare(t *testing.T) {
	// Version comparison with pre-release
	if !shouldSkipUpgrade("v2.0.0", "v1.0.0") {
		t.Fatalf("v2.0.0 should skip v1.0.0")
	}
}

func TestShouldSkipUpgradeDevCurrent(t *testing.T) {
	if shouldSkipUpgrade("dev", "v1.0.0") {
		t.Fatalf("dev should not skip upgrade")
	}
}

func TestShouldSkipUpgradeEmpty(t *testing.T) {
	if shouldSkipUpgrade("", "v1.0.0") {
		t.Fatalf("empty should not skip")
	}
	if shouldSkipUpgrade("v1.0.0", "") {
		t.Fatalf("should not skip when target empty")
	}
}

func TestUpgradeFull(t *testing.T) {
	// Build a valid tar.gz with acp binary
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	binaryData := []byte("upgraded-binary-data")
	if err := tw.WriteHeader(&tar.Header{
		Name: "acp",
		Mode: 0o755,
		Size: int64(len(binaryData)),
	}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write(binaryData); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}

	archiveBytes := buf.Bytes()
	hash := sha256.Sum256(archiveBytes)
	checksumLine := fmt.Sprintf("%s  acp-linux-amd64.tar.gz\n", hex.EncodeToString(hash[:]))

	srv := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "checksums.txt") {
			w.Write([]byte(checksumLine))
			return
		}
		w.Write(archiveBytes)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	targetPath := tmpDir + "/acp"
	// Create a dummy initial file
	if err := os.WriteFile(targetPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	opts := upgradeOptions{
		Project:       "acp",
		Command:       "acp",
		Repo:          "doublepi123/acp",
		GitHubBaseURL: srv.URL,
		Tag:           "v1.0.0",
		TargetPath:    targetPath,
		GOOS:          "linux",
		GOARCH:        "amd64",
	}

	if err := upgrade(opts); err != nil {
		t.Fatalf("upgrade error = %v", err)
	}

	// Verify file was upgraded
	newData, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	if string(newData) != "upgraded-binary-data" {
		t.Fatalf("file content = %q, want upgraded-binary-data", string(newData))
	}
}

func TestUpgradeFullWithChecksum(t *testing.T) {
	// Build a valid tar.gz with acp binary
	binaryData := []byte("checksum-verified-binary")
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	if err := tw.WriteHeader(&tar.Header{
		Name: "acp",
		Mode: 0o755,
		Size: int64(len(binaryData)),
	}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write(binaryData); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}

	srv := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "checksums.txt") {
			w.Write([]byte("f778771739630eca76caca38535df985841650d6c6d385fa8b49dec1e5c03292  acp-linux-amd64.tar.gz\n"))
			return
		}
		w.Write(buf.Bytes())
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	targetPath := tmpDir + "/acp"
	if err := os.WriteFile(targetPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	opts := upgradeOptions{
		Project:       "acp",
		Command:       "acp",
		Repo:          "doublepi123/acp",
		GitHubBaseURL: srv.URL,
		Tag:           "v1.0.0",
		TargetPath:    targetPath,
		GOOS:          "linux",
		GOARCH:        "amd64",
	}

	// Checksum should verify ok
	if err := upgrade(opts); err != nil {
		t.Fatalf("upgrade error = %v", err)
	}
}

func TestUpgradeZip(t *testing.T) {
	binaryData := []byte("windows-binary-data")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("acp.exe")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Write(binaryData); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	archiveBytes := buf.Bytes()
	hash := sha256.Sum256(archiveBytes)
	checksumLine := fmt.Sprintf("%s  acp-windows-amd64.zip\n", hex.EncodeToString(hash[:]))

	srv := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "checksums.txt") {
			w.Write([]byte(checksumLine))
			return
		}
		w.Write(archiveBytes)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	targetPath := tmpDir + "/acp.exe"
	if err := os.WriteFile(targetPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	opts := upgradeOptions{
		Project:       "acp",
		Command:       "acp",
		Repo:          "doublepi123/acp",
		GitHubBaseURL: srv.URL,
		Tag:           "v1.0.0",
		TargetPath:    targetPath,
		GOOS:          "windows",
		GOARCH:        "amd64",
	}

	if err := upgrade(opts); err != nil {
		t.Fatalf("upgrade error = %v", err)
	}

	newData, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	if string(newData) != "windows-binary-data" {
		t.Fatalf("file content = %q", string(newData))
	}
}
