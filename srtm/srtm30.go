package srtm

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Path separator
var FILE_PATH_SEP = strings.ReplaceAll(strconv.QuoteRune(os.PathSeparator), "'", "")

type Srtm30Downloader struct {
	BasePath string // https://e4ftl01.cr.usgs.gov/DP133/SRTM/SRTMGL1.003/2000.02.11
	Dir      string
	Username string
	Password string
}

type HgtFile struct {
	Path  string
	Bytes []byte
}

func (d *Srtm30Downloader) DownloadDemFile(lat, lon float64) (HgtFile, error) {
	filename := generateDemFileName(lat, lon)
	filepath := d.Dir + FILE_PATH_SEP + filename

	hgtFile := HgtFile{
		Path: filepath,
	}

	if d.checkIfDemFileExists(filepath) {
		b, err := os.ReadFile(filepath)

		if err != nil {
			return hgtFile, fmt.Errorf("cannot read %s file. cause: %w", filepath, err)
		}

		hgtFile.Bytes = b

		return hgtFile, nil
	}

	url := d.BasePath + "/" + filename

	client := &http.Client{}
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(d.Username, d.Password)

	resp, err := client.Do(req)

	if err != nil {
		err := fmt.Errorf("cannot download hgt file %s. Cause: %w", filename, err)
		log.Errorf(err.Error())
		return hgtFile, err
	}

	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)

	if err != nil {
		return hgtFile, fmt.Errorf("cannot download file %s. cause: %w", url, err)
	}

	hgtFile.Bytes = b

	err = d.saveHgtFile(filepath, b)

	if err != nil {
		return hgtFile, fmt.Errorf("cannot save %s file. cause: %w", filepath, err)
	}

	return hgtFile, nil
}

func (d *Srtm30Downloader) saveHgtFile(path string, bytes []byte) error {
	if d.checkIfDemFileExists(path) {
		return nil
	}

	err := os.WriteFile(path, bytes, 0644)

	if err != nil {
		return fmt.Errorf("cannot save HGT file %s. cause: %w", path, err)
	}

	return nil
}

func (d *Srtm30Downloader) checkIfDemFileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}

	return false
}

func generateDemFileName(lat, lon float64) string {
	ns := "N"
	if lat < 0 {
		ns = "S"
	}
	ew := "E"
	if lon < 0 {
		ew = "W"
	}
	return ns + cpad(lat, 2) + ew + cpad(lon, 3) + ".hgt"
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
