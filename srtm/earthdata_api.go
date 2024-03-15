package srtm

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"encoding/json"

	log "github.com/sirupsen/logrus"
)

const defaultEarthDataBaseUrl = "https://urs.earthdata.nasa.gov/api"

type EarthDataToken struct {
	AccessToken    string `json:"access_token"`
	ExpirationDate string `json:"expiration_date"`
}

type EarthdataApi struct {
	BaseUrl  string
	Username string
	Password string
	tokens   []EarthDataToken
}

func (a *EarthdataApi) GenerateToken() (EarthDataToken, error) {
	if a.BaseUrl == "" {
		a.BaseUrl = defaultEarthDataBaseUrl
	}

	tokens, err := a.GetAvailableTokens()

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

	client := &http.Client{
		Timeout: 15 * time.Second,
	}
	req, _ := http.NewRequest("POST", url, nil)
	req.SetBasicAuth(a.Username, a.Password)

	resp, err := client.Do(req)

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
		return token, nil
	}

	err = json.Unmarshal(responseData, &token)

	if err != nil {
		return token, errors.New("cannot parse EarthData token response")
	}

	return token, nil
}

func (a *EarthdataApi) GetAvailableTokens() ([]EarthDataToken, error) {
	if a.BaseUrl == "" {
		a.BaseUrl = defaultEarthDataBaseUrl
	}

	url := a.BaseUrl + "/users/tokens"

	client := &http.Client{}
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(a.Username, a.Password)

	resp, err := client.Do(req)

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
		return nil, fmt.Errorf("cannot read EarthData API response from %s", url)
	}

	tokens := []EarthDataToken{}
	err = json.Unmarshal(responseData, &tokens)

	if err != nil {
		return nil, errors.New("cannot parse EarthData tokens list response")
	}

	a.tokens = tokens

	return tokens, nil
}

func (a *EarthdataApi) getValidToken() (EarthDataToken, error) {
	for _, t := range a.tokens {
		if t.isValid() {
			return t, nil
		}
	}

	return EarthDataToken{}, errors.New("cannot get a valid token")
}

func (t EarthDataToken) isValid() bool {
	return true
}
