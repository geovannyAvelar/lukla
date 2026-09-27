package srtm

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var tokens = []map[string]string{
	{
		"access_token":    "eyJ0eXAiOiJKV1QiLCJvcmlnaW4iOiJFYXJ0aGRhdGEgTG9naW4iLCJhbGciOiJSUzI1NiJ9",
		"token_type":      "Bearer",
		"expiration_date": "08/09/2022",
	},
	{
		"access_token":    "eyJ0eXAiOiJKV1QiLCJvcmlnaW4iOiJFYXJfd355fgergrty576hgrth67tujh76574y54gg",
		"token_type":      "Bearer",
		"expiration_date": "08/09/2022",
	},
}

var server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" && strings.Contains(r.URL.Path, "/users/tokens") {
		json.NewEncoder(w).Encode(tokens)
		return
	}

	if r.Method == "POST" && strings.Contains(r.URL.Path, "/users/token") {
		json.NewEncoder(w).Encode(tokens[0])
		return
	}

	if r.Method == "GET" && strings.Contains(r.URL.Path, "N00E000.SRTMGL1.hgt.zip") {
		http.Error(w, "Not found", http.StatusNotFound)
	}

	b, err := os.ReadFile("testdata/files.zip")

	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
	}

	w.Write(b)
}))

func TestDownloadDemFile(t *testing.T) {
	t.Parallel()

	earthdataApi := &EarthdataApi{BaseUrl: server.URL, HttpClient: http.DefaultClient}

	// A dedicated temp dir avoids the file-path collision with
	// TestDownloadZippedDemFileWithCoordinates (same coordinates, so same
	// derived filename) that used to make this test flaky when run in
	// parallel with the rest of the suite.
	d := Downloader{
		BasePath:   server.URL,
		Dir:        t.TempDir(),
		Api:        earthdataApi,
		HttpClient: http.DefaultClient,
	}

	path, err := d.DownloadDemFile(27.687619, 86.731679)

	os.Remove(path)

	if err != nil {
		t.Errorf("cannot download DEM file. Cause: %s", err)
	}
}

func TestDownloadDemFileWith404(t *testing.T) {
	t.Parallel()

	earthdataApi := &EarthdataApi{BaseUrl: server.URL, HttpClient: http.DefaultClient}

	d := Downloader{
		BasePath:   server.URL,
		Dir:        t.TempDir(),
		Api:        earthdataApi,
		HttpClient: http.DefaultClient,
	}

	_, err := d.DownloadDemFile(0.0, 0.0)

	if err == nil {
		t.Error("expected an error, but received success")
	}

	if err != nil && !errors.Is(errors.Unwrap(err), ErrNonExistentDemFile) {
		t.Errorf("expected %s error but received: %s", ErrNonExistentDemFile, errors.Unwrap(err))
	}
}

func TestDownloadZippedDemFileWithCoordinates(t *testing.T) {
	t.Parallel()

	earthdataApi := &EarthdataApi{BaseUrl: server.URL, HttpClient: http.DefaultClient}

	d := Downloader{
		BasePath:                 server.URL,
		Dir:                      t.TempDir(),
		Api:                      earthdataApi,
		HttpClient:               http.DefaultClient,
		nonExistentZipFiles:      &map[string]bool{},
		nonExistentZipFilesMutex: &sync.Mutex{},
		downloads:                make(map[string]*sync.Mutex),
		downloadsMutex:           &sync.Mutex{},
	}

	path, b, err := d.downloadZippedDemFileWithCoordinates(27.687619, 86.731679)

	os.Remove(path)

	if err != nil {
		t.Errorf("error during hgt file download. cause: %s", err)
	}

	payload, err := os.ReadFile("testdata/files.zip")

	if err != nil {
		t.Errorf("cannot open test zip file. Cause: %s", err)
	}

	if !bytes.Equal(b, payload) {
		t.Errorf("returned bytes are different of payload bytes")
	}
}

func TestDownloadZipFile404(t *testing.T) {
	t.Parallel()

	earthdataApi := &EarthdataApi{BaseUrl: server.URL, HttpClient: http.DefaultClient}

	d := Downloader{
		BasePath:                 server.URL,
		Dir:                      t.TempDir(),
		Api:                      earthdataApi,
		HttpClient:               http.DefaultClient,
		nonExistentZipFiles:      &map[string]bool{},
		nonExistentZipFilesMutex: &sync.Mutex{},
		downloads:                make(map[string]*sync.Mutex),
		downloadsMutex:           &sync.Mutex{},
	}

	_, _, err := d.downloadZippedDemFileWithCoordinates(0.0, 0.0)

	if err != nil && !errors.Is(errors.Unwrap(err), ErrNonExistentDemFile) {
		t.Errorf("expected %s error but received: %s", ErrNonExistentDemFile, errors.Unwrap(err))
	}
}

func TestUnzip(t *testing.T) {
	t.Parallel()

	d := Downloader{}

	files, err := d.unzip("testdata/files.zip", "testdata")

	for _, f := range files {
		os.Remove(f)
	}

	if err != nil {
		t.Errorf("cannot unzip test file. Cause: %s", err)
	}
}

func TestUnzipInvalidFile(t *testing.T) {
	t.Parallel()

	d := Downloader{}

	_, err := d.unzip("testdata/does-not-exist.zip", t.TempDir())

	if err == nil {
		t.Error("expected an error for a non-existent zip file, got none")
	}
}

// TestUnzipRejectsPathTraversal is a regression test for a Zip Slip
// vulnerability: an archive entry named with "../" segments used to be
// extracted relative to destFolder, allowing it to escape the destination
// directory.
func TestUnzipRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	entry, err := zw.Create("../../evil.txt")
	if err != nil {
		t.Fatalf("cannot create zip entry. Cause: %s", err)
	}

	if _, err := entry.Write([]byte("pwned")); err != nil {
		t.Fatalf("cannot write zip entry. Cause: %s", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("cannot close zip writer. Cause: %s", err)
	}

	maliciousZipPath := filepath.Join(dir, "evil.zip")
	if err := os.WriteFile(maliciousZipPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("cannot write malicious zip fixture. Cause: %s", err)
	}

	destDir := filepath.Join(dir, "dest")

	d := Downloader{}
	_, err = d.unzip(maliciousZipPath, destDir)

	if err == nil {
		t.Fatal("expected an error for a path traversal entry, got none")
	}

	if _, statErr := os.Stat(filepath.Join(dir, "evil.txt")); statErr == nil {
		t.Error("path traversal entry was written outside the destination folder")
	}
}

// recordingTransport wraps an http.RoundTripper and counts how many requests
// went through it, to verify a Downloader actually uses the HttpClient it was
// configured with instead of an unconfigured one created ad hoc.
type recordingTransport struct {
	used int32
	base http.RoundTripper
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(&r.used, 1)
	return r.base.RoundTrip(req)
}

func TestDownloadZippedDemFileUsesConfiguredHttpClient(t *testing.T) {
	t.Parallel()

	transport := &recordingTransport{base: http.DefaultTransport}
	client := &http.Client{Transport: transport}

	earthdataApi := &EarthdataApi{BaseUrl: server.URL, HttpClient: http.DefaultClient}

	d := Downloader{
		BasePath:                 server.URL,
		Dir:                      t.TempDir(),
		Api:                      earthdataApi,
		HttpClient:               client,
		nonExistentZipFiles:      &map[string]bool{},
		nonExistentZipFilesMutex: &sync.Mutex{},
		downloads:                make(map[string]*sync.Mutex),
		downloadsMutex:           &sync.Mutex{},
	}

	path, _, err := d.downloadZippedDemFileWithCoordinates(27.687619, 86.731679)
	os.Remove(path)

	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if atomic.LoadInt32(&transport.used) == 0 {
		t.Error("expected the Downloader's configured HttpClient to be used, but it was not")
	}
}

func TestDownloadZippedDemFileFallsBackWhenHttpClientIsNil(t *testing.T) {
	t.Parallel()

	earthdataApi := &EarthdataApi{BaseUrl: server.URL, HttpClient: http.DefaultClient}

	d := Downloader{
		BasePath:                 server.URL,
		Dir:                      t.TempDir(),
		Api:                      earthdataApi,
		HttpClient:               nil,
		nonExistentZipFiles:      &map[string]bool{},
		nonExistentZipFilesMutex: &sync.Mutex{},
		downloads:                make(map[string]*sync.Mutex),
		downloadsMutex:           &sync.Mutex{},
	}

	path, _, err := d.downloadZippedDemFileWithCoordinates(27.687619, 86.731679)
	os.Remove(path)

	if err != nil {
		t.Fatalf("unexpected error with a nil HttpClient: %s", err)
	}
}

// buildBboxFixture writes a minimal SRTM bounding-box GeoJSON FeatureCollection
// with `count` features, so DownloadAllDemFiles tests do not depend on the
// full ~3.6MB production dataset.
func buildBboxFixture(t *testing.T, dir string, count int) string {
	t.Helper()

	var sb strings.Builder
	sb.WriteString(`{"type":"FeatureCollection","features":[`)

	for i := 0; i < count; i++ {
		if i > 0 {
			sb.WriteString(",")
		}

		lat := i % 60
		lon := i % 180

		fmt.Fprintf(&sb, `{"type":"Feature","geometry":{"type":"Polygon","coordinates":`+
			`[[[%d,%d],[%d,%d],[%d,%d],[%d,%d],[%d,%d]]]},"properties":{"dataFile":"N%02dE%03d.SRTMGL1.hgt.zip"}}`,
			lon, lat, lon+1, lat, lon+1, lat+1, lon, lat+1, lon, lat, lat, lon)
	}

	sb.WriteString(`]}`)

	path := filepath.Join(dir, "bbox.json")

	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		t.Fatalf("cannot write bbox fixture. Cause: %s", err)
	}

	return path
}

// TestDownloadAllDemFilesCountsEveryFeature is a regression test for a data
// race on the download counter (now protected by a mutex) and, indirectly,
// for the batches actually completing.
func TestDownloadAllDemFilesCountsEveryFeature(t *testing.T) {
	dir := t.TempDir()

	const featureCount = 130 // spans more than one 100-item batch
	bboxPath := buildBboxFixture(t, dir, featureCount)
	t.Setenv("LUKLA_SRTM30M_BBOX_FILE", bboxPath)

	var downloadCount int64

	testSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.Contains(r.URL.Path, "/users/token") {
			json.NewEncoder(w).Encode(tokens[0])
			return
		}

		atomic.AddInt64(&downloadCount, 1)

		b, err := os.ReadFile("testdata/files.zip")
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Write(b)
	}))
	defer testSrv.Close()

	earthdataApi := &EarthdataApi{BaseUrl: testSrv.URL, HttpClient: http.DefaultClient}

	d := Downloader{
		BasePath:   testSrv.URL,
		Dir:        dir,
		Api:        earthdataApi,
		HttpClient: http.DefaultClient,
	}

	if err := d.DownloadAllDemFiles(); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if atomic.LoadInt64(&downloadCount) != featureCount {
		t.Errorf("expected %d downloads, got %d", featureCount, downloadCount)
	}
}

// TestDownloadAllDemFilesWaitsOnFailures is a regression test for a missing
// wg.Done() call: if a goroutine returned early on a download failure without
// calling wg.Done(), wg.Wait() would block forever and this test would time
// out instead of returning.
func TestDownloadAllDemFilesWaitsOnFailures(t *testing.T) {
	dir := t.TempDir()

	bboxPath := buildBboxFixture(t, dir, 5)
	t.Setenv("LUKLA_SRTM30M_BBOX_FILE", bboxPath)

	failingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.Contains(r.URL.Path, "/users/token") {
			json.NewEncoder(w).Encode(tokens[0])
			return
		}

		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer failingSrv.Close()

	earthdataApi := &EarthdataApi{BaseUrl: failingSrv.URL, HttpClient: http.DefaultClient}

	d := Downloader{
		BasePath:   failingSrv.URL,
		Dir:        dir,
		Api:        earthdataApi,
		HttpClient: http.DefaultClient,
	}

	done := make(chan error, 1)

	go func() {
		done <- d.DownloadAllDemFiles()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("DownloadAllDemFiles did not return: a batch's WaitGroup is likely stuck " +
			"waiting on a goroutine that never called wg.Done()")
	}

	// No file should have been extracted, since every download failed.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot list dem dir. Cause: %s", err)
	}

	for _, e := range entries {
		if e.Name() == filepath.Base(bboxPath) {
			continue
		}

		t.Errorf("unexpected file extracted after only failed downloads: %s", e.Name())
	}
}
