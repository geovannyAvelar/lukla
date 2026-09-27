package srtm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	log "github.com/sirupsen/logrus"
)

const defaultEarthDataBaseUrl = "https://urs.earthdata.nasa.gov/api"

type EarthDataToken struct {
	AccessToken    string `json:"access_token"`
	ExpirationDate string `json:"expiration_date"`
}

type EarthdataApi struct {
	BaseUrl    string
	Username   string
	Password   string
	HttpClient *http.Client

	tokensMutex sync.Mutex
	tokens      []EarthDataToken
}

func (a *EarthdataApi) GenerateToken(ctx context.Context) (EarthDataToken, error) {
	if a.BaseUrl == "" {
		a.BaseUrl = defaultEarthDataBaseUrl
	}

	tokens, err := a.GetAvailableTokens(ctx)

	if err != nil {
		log.Warnf("Cannot recover available tokens from EarthData API. Cause: %s", err)
		return EarthDataToken{}, fmt.Errorf("canoot recover tokens from API. Cause: %w", err)
	}

	if len(tokens) > 0 {
		return tokens[0], nil
	}

	token, err := a.getValidToken()

	if err == nil {
		return token, nil
	}

	url := a.BaseUrl + "/users/token"

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)

	if err != nil {
		return token, fmt.Errorf("cannot build EarthData token request. Cause: %w", err)
	}

	req.SetBasicAuth(a.Username, a.Password)

	resp, err := a.HttpClient.Do(req)

	if err != nil {
		err := fmt.Errorf("cannot issue an EarthData token. Cause: %w", err)
		log.Errorf(err.Error())
		return token, err
	}

	if resp.StatusCode != 200 {
		msg := fmt.Sprintf("received a %d error during EarthData token request", resp.StatusCode)
		return token, errors.New(msg)
	}

	defer resp.Body.Close()

	responseData, err := io.ReadAll(resp.Body)

	if err != nil {
		return token, fmt.Errorf("cannot read EarthData token response. Cause: %w", err)
	}

	err = json.Unmarshal(responseData, &token)

	if err != nil {
		return token, errors.New("cannot parse EarthData token response")
	}

	return token, nil
}

func (a *EarthdataApi) GetAvailableTokens(ctx context.Context) ([]EarthDataToken, error) {
	if a.BaseUrl == "" {
		a.BaseUrl = defaultEarthDataBaseUrl
	}

	url := a.BaseUrl + "/users/tokens"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)

	if err != nil {
		return nil, fmt.Errorf("cannot build EarthData tokens request. Cause: %w", err)
	}

	req.SetBasicAuth(a.Username, a.Password)

	resp, err := a.HttpClient.Do(req)

	if err != nil {
		err := fmt.Errorf("cannot recover EarthData tokens. Cause: %w", err)
		log.Errorf(err.Error())
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("received a %d error during EarthData token request", resp.StatusCode)
	}

	defer resp.Body.Close()

	responseData, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, fmt.Errorf("cannot read EarthData API response from %s. Cause: %w", url, err)
	}

	tokens := []EarthDataToken{}
	err = json.Unmarshal(responseData, &tokens)

	if err != nil {
		return nil, errors.New("cannot parse EarthData tokens list response")
	}

	a.tokensMutex.Lock()
	a.tokens = tokens
	a.tokensMutex.Unlock()

	return tokens, nil
}

// getValidToken is the only reader of a.tokens (besides GetAvailableTokens
// itself overwriting it), guarded by the same mutex since a.tokens is shared
// mutable state: EarthdataApi is normally held as a single instance behind
// srtm.Downloader.Api and called from many concurrent goroutines (e.g.
// DownloadAllDemFiles' per-feature downloads).
func (a *EarthdataApi) getValidToken() (EarthDataToken, error) {
	a.tokensMutex.Lock()
	tokens := a.tokens
	a.tokensMutex.Unlock()

	for _, t := range tokens {
		if t.isValid() {
			return t, nil
		}
	}

	return EarthDataToken{}, errors.New("cannot get a valid token")
}

func (t EarthDataToken) isValid() bool {
	return true
}
