package srtm

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// SRTM30 dataset base url
var defaultSRTMServerURL = "https://e4ftl01.cr.usgs.gov/MEASURES/SRTMGL1.003/2000.02.11/"

// Path separator
var filePathSep = strings.ReplaceAll(strconv.QuoteRune(os.PathSeparator), "'", "")

type Downloader struct {
	BasePath   string
	Dir        string
	HttpClient *http.Client
	Api        EarthdataApi
}

func (d Downloader) DownloadDemFile(pLat, pLon float64) (string, error) {
	filename := generateZipDemFileName(pLat, pLon)
	zipFilepath := d.Dir + filePathSep + filename

	demFilePath := strings.ReplaceAll(zipFilepath, ".zip", "")
	demFilePath = strings.ReplaceAll(demFilePath, ".SRTMGL1", "")

	if !d.checkIfDemFileExists(demFilePath) {
		zipPath, _, err := d.downloadZippedDemFile(pLat, pLon)

		if err != nil {
			return "", fmt.Errorf("cannot download HGT file for coordinates %f, %f. Cause %w", pLat, pLon, err)
		}

		demPath := strings.ReplaceAll(zipPath, ".zip", "")

		if d.checkIfDemFileExists(demPath) {
			return demPath, nil
		}

		files, err := d.unzip(zipPath, d.Dir)

		if err != nil {
			return "", fmt.Errorf("cannot unzip file %s. Cause %w", zipPath, err)
		}

		if len(files) < 1 {
			return "", fmt.Errorf("cannot uncompress file %s", zipPath)
		}

		log.Infof("File %s is uncompressed", zipPath)

		os.Remove(zipPath)

		return files[0], nil
	}

	return demFilePath, nil
}

func (d Downloader) downloadZippedDemFile(lat, lon float64) (string, []byte, error) {
	if d.BasePath == "" {
		d.BasePath = defaultSRTMServerURL
	}

	filename := generateZipDemFileName(lat, lon)
	filepath := d.Dir + filePathSep + filename

	if d.checkIfDemFileExists(filepath) {
		b, err := os.ReadFile(filepath)

		if err != nil {
			return "", nil, fmt.Errorf("cannot read %s file. cause: %w", filepath, err)
		}

		return filepath, b, nil
	}

	token, err := d.Api.GenerateToken()

	if err != nil {
		return "", nil, fmt.Errorf("cannot generate EarthData API token. Cause %w", err)
	}

	url := d.BasePath + "/" + filename

	client := &http.Client{
		Timeout: 60 * time.Second,
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Add("Authorization", "Bearer "+token.AccessToken)

	resp, err := client.Do(req)

	log.Infof("Downloading file %s from SRTM30m server...", filename)

	if err != nil {
		err := fmt.Errorf("cannot download hgt file %s. Cause: %w", filename, err)
		log.Errorf(err.Error())
		return "", nil, err
	}

	if resp.StatusCode != 200 {
		msg := fmt.Sprintf("received a %d error during request", resp.StatusCode)
		return "", nil, errors.New(msg)
	}

	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)

	if err != nil {
		return "", nil, fmt.Errorf("cannot download file %s. cause: %w", url, err)
	}

	err = d.saveZipHgtFile(filepath, b)

	if err != nil {
		return "", nil, fmt.Errorf("cannot save %s file. cause: %w", filepath, err)
	}

	return filepath, b, nil
}

func (d Downloader) saveZipHgtFile(path string, bytes []byte) error {
	if d.checkIfDemFileExists(path) {
		return nil
	}

	err := os.WriteFile(path, bytes, 0644)

	if err != nil {
		return fmt.Errorf("cannot save HGT file %s. cause: %w", path, err)
	}

	log.Infof("Zip file save on %s", path)

	return nil
}

func (d Downloader) checkIfDemFileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}

	return false
}

func (d Downloader) unzip(zipFile string, destFolder string) ([]string, error) {
	files := []string{}

	r, err := zip.OpenReader(zipFile)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	if err := os.MkdirAll(destFolder, 0755); err != nil {
		return nil, err
	}

	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()

		path := filepath.Join(destFolder, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
		} else {
			if err := os.MkdirAll(filepath.Dir(path), f.Mode()); err != nil {
				return nil, err
			}

			outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return nil, err
			}
			defer outFile.Close()

			files = append(files, outFile.Name())

			if _, err := io.Copy(outFile, rc); err != nil {
				return nil, err
			}
		}
	}

	return files, nil
}

func generateZipDemFileName(lat, lon float64) string {
	ns := "N"
	if lat < 0 {
		ns = "S"
	}
	ew := "E"
	if lon < 0 {
		ew = "W"
	}
	return ns + cpad(lat, 2) + ew + cpad(lon, 3) + ".SRTMGL1.hgt.zip"
}

func cpad(coord float64, length int) string {
	return pad(strconv.Itoa(int(math.Abs(math.Floor(coord)))), length, "0")
}

func pad(str string, length int, pad string) string {
	if length <= 0 || len(str) >= length || len(pad) == 0 {
		return str
	}
	return strings.Repeat(pad, length-len(str)) + str
}
