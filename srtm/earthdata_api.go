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

const DEFAULT_EARTHDATA_BASE_URL = "https://urs.earthdata.nasa.gov/api"

type EarthDataToken struct {
	AccessToken    string    `json:"access_token"`
	ExpirationDate time.Time `json:"expiration_date"`
}

type EarthdataApi struct {
	BaseUrl  string
	Username string
	Password string
	token    EarthDataToken
}

func (a *EarthdataApi) GenerateToken() (EarthDataToken, error) {
	if a.token.isValid() {
		return a.token, nil
	}

	token := EarthDataToken{}
	url := a.BaseUrl + "/users/token"

	client := &http.Client{}
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

	a.token = token

	return token, nil
}

func (t EarthDataToken) isValid() bool {
	return t.ExpirationDate.After(time.Now().UTC())
}
