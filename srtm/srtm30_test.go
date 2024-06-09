package srtm

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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

// TODO This is test is flaky. It's possible to notice that when you run the entire test suite
// Probably cause by the absence of the zip file, which is not downloaded some times.
func TestDownloadDemFile(t *testing.T) {
	t.Parallel()

	earthdataApi := &EarthdataApi{BaseUrl: server.URL, HttpClient: http.DefaultClient}

	d := Downloader{
		BasePath:   server.URL,
		Dir:        "testdata/dem",
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
		Dir:        "testdata/dem",
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

func TestDownloadZippedDemFile(t *testing.T) {
	t.Parallel()

	earthdataApi := &EarthdataApi{BaseUrl: server.URL, HttpClient: http.DefaultClient}

	d := Downloader{
		BasePath:   server.URL,
		Dir:        "testdata/dem",
		Api:        earthdataApi,
		HttpClient: http.DefaultClient,
	}

	path, b, err := d.downloadZippedDemFile(27.687619, 86.731679)

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
		BasePath:   server.URL,
		Dir:        "testdata/dem",
		Api:        earthdataApi,
		HttpClient: http.DefaultClient,
	}

	_, _, err := d.downloadZippedDemFile(0.0, 0.0)

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
