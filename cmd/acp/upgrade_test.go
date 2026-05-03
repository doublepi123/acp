package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"testing"
)

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
