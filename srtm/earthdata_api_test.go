package srtm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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

	earthdataApi := EarthdataApi{BaseUrl: earthdataServer.URL, HttpClient: http.DefaultClient}

	_, err := earthdataApi.GenerateToken(context.Background())

	if err != nil {
		t.Errorf("error during Earthdata API token generation. Cause: %s", err)
	}
}

// TestGenerateTokenCancelsPromptly is a regression test for GenerateToken (and
// transitively GetAvailableTokens) not being context-aware: a token request
// must abort as soon as its context is canceled, instead of running until the
// (possibly slow or unresponsive) EarthData server responds. Same pattern as
// srtm30_test.go's TestDownloadZippedDemFileCancelsPromptly.
func TestGenerateTokenCancelsPromptly(t *testing.T) {
	t.Parallel()

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer slowServer.Close()

	earthdataApi := EarthdataApi{BaseUrl: slowServer.URL, HttpClient: http.DefaultClient}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	start := time.Now()

	go func() {
		_, err := earthdataApi.GenerateToken(ctx)
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

		if elapsed > time.Second {
			t.Errorf("GenerateToken took %s to return after cancellation; expected it to abort "+
				"promptly instead of waiting for the slow server", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GenerateToken did not return promptly after the context was canceled")
	}
}

// tokenResponseBrokenBodyTransport lets GET /users/tokens succeed normally
// (with an empty list, so GenerateToken falls through to requesting a new
// token) but makes the POST /users/token response body unreadable - isolating
// GenerateToken's own read-error handling from GetAvailableTokens's (covered
// separately by TestGetAvailableTokensSurfacesBodyReadError).
type tokenResponseBrokenBodyTransport struct {
	base http.RoundTripper
}

func (t tokenResponseBrokenBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == "POST" && strings.Contains(req.URL.Path, "/users/token") {
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

	return t.base.RoundTrip(req)
}

// TestGenerateTokenSurfacesPostResponseReadError is a regression test for the
// confirmed bug: `if err != nil { return token, nil }` after a failed
// io.ReadAll used to return a *nil* error alongside a zero-value token,
// letting the caller proceed as if it had obtained a real (but empty) bearer
// token instead of failing loudly.
func TestGenerateTokenSurfacesPostResponseReadError(t *testing.T) {
	t.Parallel()

	emptyTokensServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/users/tokens") {
			json.NewEncoder(w).Encode([]EarthDataToken{})
			return
		}

		t.Error("unexpected request reached the real server; the broken-body transport " +
			"should have intercepted POST /users/token")
	}))
	defer emptyTokensServer.Close()

	client := &http.Client{Transport: tokenResponseBrokenBodyTransport{base: http.DefaultTransport}}
	earthdataApi := EarthdataApi{BaseUrl: emptyTokensServer.URL, HttpClient: client}

	_, err := earthdataApi.GenerateToken(context.Background())

	if err == nil {
		t.Fatal("expected an error when the token response body cannot be read, got none " +
			"(a nil error here means a zero-value token would be silently treated as success)")
	}
}

// TestGetAvailableTokensSurfacesBodyReadError covers the equivalent read
// failure inside GetAvailableTokens itself.
func TestGetAvailableTokensSurfacesBodyReadError(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: brokenBodyTransport{}}
	earthdataApi := EarthdataApi{BaseUrl: "http://127.0.0.1:1", HttpClient: client}

	_, err := earthdataApi.GetAvailableTokens(context.Background())

	if err == nil {
		t.Fatal("expected an error when the tokens list response body cannot be read, got none")
	}
}

func TestGenerateTokenRejectsMalformedResponse(t *testing.T) {
	t.Parallel()

	badJSONServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/users/tokens") {
			json.NewEncoder(w).Encode([]EarthDataToken{})
			return
		}

		w.Write([]byte("not json"))
	}))
	defer badJSONServer.Close()

	earthdataApi := EarthdataApi{BaseUrl: badJSONServer.URL, HttpClient: http.DefaultClient}

	_, err := earthdataApi.GenerateToken(context.Background())

	if err == nil {
		t.Fatal("expected an error for a malformed token response, got none")
	}
}

func TestGenerateTokenRejectsNon200Status(t *testing.T) {
	t.Parallel()

	badStatusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/users/tokens") {
			json.NewEncoder(w).Encode([]EarthDataToken{})
			return
		}

		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer badStatusServer.Close()

	earthdataApi := EarthdataApi{BaseUrl: badStatusServer.URL, HttpClient: http.DefaultClient}

	_, err := earthdataApi.GenerateToken(context.Background())

	if err == nil {
		t.Fatal("expected an error for a non-200 token response, got none")
	}
}

// TestGetAvailableTokensCachePersists is a regression test for the confirmed,
// separate value-receiver bug: GetAvailableTokens used to assign a.tokens on
// a throwaway copy of the receiver, so the cache never actually persisted
// back to the caller. With pointer receivers, a token GetAvailableTokens just
// fetched is now visible to a later getValidToken() call on the same
// *EarthdataApi instance.
//
// Note on scope: GenerateToken itself always calls GetAvailableTokens fresh
// before ever consulting getValidToken (see GenerateToken's body), so this
// fix does not currently reduce the number of HTTP requests GenerateToken
// makes - it makes a.tokens correctly reflect what was fetched instead of
// silently discarding it, which is what round 3's step 7 asked for.
func TestGetAvailableTokensCachePersists(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokens)
	}))
	defer server.Close()

	earthdataApi := &EarthdataApi{BaseUrl: server.URL, HttpClient: http.DefaultClient}

	if _, err := earthdataApi.GetAvailableTokens(context.Background()); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	got, err := earthdataApi.getValidToken()

	if err != nil {
		t.Fatalf("expected getValidToken to find the token GetAvailableTokens just cached, "+
			"got error: %s", err)
	}

	if got.AccessToken != tokens[0]["access_token"] {
		t.Errorf("expected cached access token %q, got %q", tokens[0]["access_token"], got.AccessToken)
	}
}
