package srtm

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	path, err := d.DownloadDemFile(context.Background(), 27.687619, 86.731679)

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

	_, err := d.DownloadDemFile(context.Background(), 0.0, 0.0)

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

	path, b, err := d.downloadZippedDemFileWithCoordinates(context.Background(), 27.687619, 86.731679)

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

	_, _, err := d.downloadZippedDemFileWithCoordinates(context.Background(), 0.0, 0.0)

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

// TestExtractZipEntryRejectsEntryExceedingBudget is a regression test for
// extractZipEntry silently truncating an entry that exceeds its size budget:
// io.LimitReader used to stop at the cap and return a nil error, so the
// truncated file was written to disk and treated as a successful extraction.
func TestExtractZipEntryRejectsEntryExceedingBudget(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	entry, err := zw.Create("big.txt")
	if err != nil {
		t.Fatalf("cannot create zip entry. Cause: %s", err)
	}

	content := bytes.Repeat([]byte("a"), 100)
	if _, err := entry.Write(content); err != nil {
		t.Fatalf("cannot write zip entry. Cause: %s", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("cannot close zip writer. Cause: %s", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("cannot open zip reader. Cause: %s", err)
	}

	if len(zr.File) != 1 {
		t.Fatalf("expected 1 entry in fixture zip, got %d", len(zr.File))
	}

	destPath := filepath.Join(t.TempDir(), "big.txt")

	// The entry's real content is 100 bytes; only budget 10.
	written, err := extractZipEntry(zr.File[0], destPath, 10)

	if err == nil {
		t.Fatal("expected an error for an entry exceeding its budget, got none")
	}

	if written != 10 {
		t.Errorf("expected 10 bytes written before truncation was detected, got %d", written)
	}

	if _, statErr := os.Stat(destPath); statErr == nil {
		t.Error("a truncated file was left on disk instead of being removed")
	}
}

func TestExtractZipEntryWithinBudgetSucceeds(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	entry, err := zw.Create("small.txt")
	if err != nil {
		t.Fatalf("cannot create zip entry. Cause: %s", err)
	}

	content := []byte("hello world")
	if _, err := entry.Write(content); err != nil {
		t.Fatalf("cannot write zip entry. Cause: %s", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("cannot close zip writer. Cause: %s", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("cannot open zip reader. Cause: %s", err)
	}

	destPath := filepath.Join(t.TempDir(), "small.txt")

	written, err := extractZipEntry(zr.File[0], destPath, int64(len(content)))

	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if written != int64(len(content)) {
		t.Errorf("expected %d bytes written, got %d", len(content), written)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("cannot read extracted file. Cause: %s", err)
	}

	if !bytes.Equal(got, content) {
		t.Errorf("extracted content does not match: got %q, want %q", got, content)
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

	path, _, err := d.downloadZippedDemFileWithCoordinates(context.Background(), 27.687619, 86.731679)
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

	path, _, err := d.downloadZippedDemFileWithCoordinates(context.Background(), 27.687619, 86.731679)
	os.Remove(path)

	if err != nil {
		t.Fatalf("unexpected error with a nil HttpClient: %s", err)
	}
}

// brokenBodyTransport is an http.RoundTripper that always answers with a 200
// response whose Body errors on every Read, to simulate a body-read failure
// independent from the request itself succeeding.
type brokenBodyTransport struct{}

func (brokenBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Body:       io.NopCloser(brokenBodyReader{}),
	}, nil
}

type brokenBodyReader struct{}

func (brokenBodyReader) Read(p []byte) (int, error) {
	return 0, errors.New("simulated body read failure")
}

// TestDownloadZippedDemFileReleasesMutexOnAllPaths is a regression test for a
// mutex leak: downloadZippedDemFile used to return without unlocking the
// per-file mutex on the cache-hit and token-generation-failure paths, which
// would deadlock any later call for the same file.
func TestDownloadZippedDemFileReleasesMutexOnAllPaths(t *testing.T) {
	t.Parallel()

	brokenTokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "token service down", http.StatusInternalServerError)
	}))
	defer brokenTokenServer.Close()

	badFileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/users/tokens") {
			json.NewEncoder(w).Encode(tokens)
			return
		}

		if r.Method == "POST" && strings.Contains(r.URL.Path, "/users/token") {
			json.NewEncoder(w).Encode(tokens[0])
			return
		}

		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer badFileServer.Close()

	unwritableDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(unwritableDir, []byte("x"), 0644); err != nil {
		t.Fatalf("cannot create save-failure fixture. Cause: %s", err)
	}

	tests := []struct {
		name       string
		lat, lon   float64
		dir        string
		apiBaseUrl string
		basePath   string
		httpClient *http.Client
	}{
		{"token generation failure", 1.0, 1.0, t.TempDir(), brokenTokenServer.URL, server.URL, http.DefaultClient},
		{"non-200 file response", 2.0, 2.0, t.TempDir(), badFileServer.URL, badFileServer.URL, http.DefaultClient},
		{"404 file response", 0.0, 0.0, t.TempDir(), server.URL, server.URL, http.DefaultClient},
		{"body read failure", 3.0, 3.0, t.TempDir(), server.URL, server.URL, &http.Client{Transport: brokenBodyTransport{}}},
		{"save failure", 4.0, 4.0, unwritableDir, server.URL, server.URL, http.DefaultClient},
		{"success", 5.0, 5.0, t.TempDir(), server.URL, server.URL, http.DefaultClient},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			earthdataApi := &EarthdataApi{BaseUrl: tt.apiBaseUrl, HttpClient: http.DefaultClient}

			d := Downloader{
				BasePath:                 tt.basePath,
				Dir:                      tt.dir,
				Api:                      earthdataApi,
				HttpClient:               tt.httpClient,
				nonExistentZipFiles:      &map[string]bool{},
				nonExistentZipFilesMutex: &sync.Mutex{},
				downloads:                make(map[string]*sync.Mutex),
				downloadsMutex:           &sync.Mutex{},
			}

			path, _, _ := d.downloadZippedDemFileWithCoordinates(context.Background(), tt.lat, tt.lon)
			if path != "" {
				os.Remove(path)
			}

			filename := generateZipDemFileName(tt.lat, tt.lon)

			d.downloadsMutex.Lock()
			mutex, ok := d.downloads[filename]
			d.downloadsMutex.Unlock()

			if !ok {
				t.Fatalf("no per-file mutex was registered for %s", filename)
			}

			if !mutex.TryLock() {
				t.Errorf("mutex for %s was left locked after downloadZippedDemFile returned", filename)
			} else {
				mutex.Unlock()
			}
		})
	}
}

// TestDownloadZippedDemFileSecondCallAfterTokenFailureDoesNotBlock is an
// end-to-end regression test for the same mutex leak: without the fix, this
// second call would hang forever waiting for a lock the first call never
// released.
func TestDownloadZippedDemFileSecondCallAfterTokenFailureDoesNotBlock(t *testing.T) {
	t.Parallel()

	brokenTokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "token service down", http.StatusInternalServerError)
	}))
	defer brokenTokenServer.Close()

	earthdataApi := &EarthdataApi{BaseUrl: brokenTokenServer.URL, HttpClient: http.DefaultClient}

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

	const lat, lon = 6.0, 6.0

	if _, _, err := d.downloadZippedDemFileWithCoordinates(context.Background(), lat, lon); err == nil {
		t.Fatal("expected an error from the first call against a broken token server, got none")
	}

	done := make(chan struct{})

	go func() {
		defer close(done)
		_, _, _ = d.downloadZippedDemFileWithCoordinates(context.Background(), lat, lon)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("second call for the same file did not return: the per-file mutex is likely still " +
			"held from the first call's token-generation failure")
	}
}

// TestDownloadZippedDemFileCancelsPromptly is a regression test for
// http.NewRequest not being context-aware: a download's HTTP request must
// abort as soon as its context is canceled, instead of running until the
// (possibly slow or unresponsive) server responds.
func TestDownloadZippedDemFileCancelsPromptly(t *testing.T) {
	t.Parallel()

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/users/tokens") {
			json.NewEncoder(w).Encode(tokens)
			return
		}

		if r.Method == "POST" && strings.Contains(r.URL.Path, "/users/token") {
			json.NewEncoder(w).Encode(tokens[0])
			return
		}

		// Simulate an unresponsive server for the actual file download.
		time.Sleep(2 * time.Second)
	}))
	defer slowServer.Close()

	earthdataApi := &EarthdataApi{BaseUrl: slowServer.URL, HttpClient: http.DefaultClient}

	d := Downloader{
		BasePath:                 slowServer.URL,
		Dir:                      t.TempDir(),
		Api:                      earthdataApi,
		HttpClient:               http.DefaultClient,
		nonExistentZipFiles:      &map[string]bool{},
		nonExistentZipFilesMutex: &sync.Mutex{},
		downloads:                make(map[string]*sync.Mutex),
		downloadsMutex:           &sync.Mutex{},
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	start := time.Now()

	go func() {
		_, _, err := d.downloadZippedDemFileWithCoordinates(ctx, 7.0, 7.0)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond) // let the request actually reach the slow handler
	cancel()

	select {
	case err := <-done:
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected an error after canceling the context, got none")
		}

		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected the error chain to include context.Canceled, got: %s", err)
		}

		if elapsed > time.Second {
			t.Errorf("download took %s to return after cancellation; expected it to abort "+
				"promptly instead of waiting for the slow server", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("download did not return promptly after the context was canceled")
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
