package srtm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

var earthdataServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" && strings.Contains(r.URL.Path, "/users/tokens") {
		json.NewEncoder(w).Encode([]map[string]string{})
		return
	}

	if r.Method == "POST" && strings.Contains(r.URL.Path, "/users/token") {
		json.NewEncoder(w).Encode(tokens[0])
		return
	}

	b, err := os.ReadFile("testdata/files.zip")

	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
	}

	w.Write(b)
}))

func TestGenerateToken(t *testing.T) {
	t.Parallel()

	earthdataApi := EarthdataApi{BaseUrl: earthdataServer.URL}

	_, err := earthdataApi.GenerateToken()

	if err != nil {
		t.Errorf("error during Earthdata API token generation. Cause: %s", err)
	}
}
