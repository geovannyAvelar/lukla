package srtm

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

var payload = []byte{1, 2, 3}
var server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Write(payload)
}))

func TestDownloadDemFile(t *testing.T) {
	t.Parallel()

	d := Srtm30Downloader{
		BasePath: server.URL,
		Dir:      "testdata/dem",
	}

	hgt, err := d.DownloadDemFile(27.687619, 86.731679)

	if err != nil {
		t.Errorf("error during hgt file download. cause: %s", err)
	}

	if !bytes.Equal(hgt.Bytes, payload) {
		t.Errorf("returned bytes are different of payload bytes")
	}

	os.Remove(hgt.Path)
}
