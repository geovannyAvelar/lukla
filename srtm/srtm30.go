package srtm

import (
	"archive/zip"
	"errors"
	"fmt"
	env "github.com/geovannyAvelar/lukla/env"
	log "github.com/sirupsen/logrus"
	"github.com/spatial-go/geoos/geoencoding/geojson"
	"github.com/spatial-go/geoos/planar"
	"github.com/spatial-go/geoos/space"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrNonExistentDemFile = errors.New("cannot locate DEM file on remote repository")

var ErrTileNotInsideSrtmCoverage = errors.New("tile is not inside SRTM coverage")

// SRTM30 dataset base url
var defaultSRTMServerURL = "https://e4ftl01.cr.usgs.gov/MEASURES/SRTMGL1.003/2000.02.11/"

// Path separator
var filePathSep = strings.ReplaceAll(strconv.QuoteRune(os.PathSeparator), "'", "")

type Downloader struct {
	BasePath                 string
	Dir                      string
	HttpClient               *http.Client
	Api                      *EarthdataApi
	datasetBbox              *geojson.FeatureCollection
	nonExistentZipFiles      *map[string]bool
	nonExistentZipFilesMutex *sync.Mutex
	downloads                map[string]*sync.Mutex
	downloadsMutex           *sync.Mutex
}

func (d *Downloader) DownloadDemFile(pLat, pLon float64) (string, error) {
	if d.nonExistentZipFiles == nil {
		d.nonExistentZipFiles = &map[string]bool{}
		d.nonExistentZipFilesMutex = &sync.Mutex{}
	}

	if d.downloads == nil {
		d.downloads = make(map[string]*sync.Mutex)
		d.downloadsMutex = &sync.Mutex{}
	}

	insideSrtmArea, err := d.isPointInsideDataSet(pLat, pLon)

	if err != nil {
		return "", err
	}

	if !insideSrtmArea {
		return "", ErrTileNotInsideSrtmCoverage
	}

	filename := generateZipDemFileName(pLat, pLon)
	zipFilepath := d.Dir + filePathSep + filename

	demFilePath := strings.ReplaceAll(zipFilepath, ".zip", "")
	demFilePath = strings.ReplaceAll(demFilePath, ".SRTMGL1", "")

	if !d.checkIfDemFileExists(demFilePath) {
		zipPath, _, err := d.downloadZippedDemFileWithCoordinates(pLat, pLon)

		if err != nil {
			return "", fmt.Errorf("cannot download HGT file for coordinates %f, %f. "+
				"Cause %w", pLat, pLon, err)
		}

		return d.unzipDemFile(zipPath)
	}

	return demFilePath, nil
}

func (d *Downloader) DownloadAllDemFiles() error {
	if d.BasePath == "" {
		d.BasePath = defaultSRTMServerURL
	}

	if d.nonExistentZipFiles == nil {
		d.nonExistentZipFiles = &map[string]bool{}
		d.nonExistentZipFilesMutex = &sync.Mutex{}
	}

	if d.downloads == nil {
		d.downloads = make(map[string]*sync.Mutex)
		d.downloadsMutex = &sync.Mutex{}
	}

	err := d.loadDatasetBbox()

	if err != nil {
		return err
	}

	chunks := partitionSlice(d.datasetBbox.Features, 100)

	for _, chunk := range chunks {
		var c int64
		var wg sync.WaitGroup

		for _, feature := range chunk {
			wg.Add(1)

			go func(feature *geojson.Feature) {
				filename := feature.Properties.MustString("dataFile")
				url := d.BasePath + "/" + filename
				path, _, err := d.downloadZippedDemFile(url)

				if err != nil {
					log.Errorf("cannot download HGT file %s. Cause %s", url, err)
				}

				_, err = d.unzipDemFile(path)

				if err != nil {
					log.Errorf("cannot unzip file %s. Cause %s", url, err)
				}

				c++

				log.Infof("%d / %d file(s) downloaded", c, len(d.datasetBbox.Features))
			}(feature)
		}

		wg.Wait()
	}

	return nil
}

func (d *Downloader) downloadZippedDemFileWithCoordinates(lat, lon float64) (string, []byte, error) {
	if d.BasePath == "" {
		d.BasePath = defaultSRTMServerURL
	}

	filename := generateZipDemFileName(lat, lon)

	if d.isZipFileNonExistent(filename) {
		return "", nil, ErrNonExistentDemFile
	}

	url := d.BasePath + "/" + filename

	return d.downloadZippedDemFile(url)
}

func (d *Downloader) downloadZippedDemFile(url string) (string, []byte, error) {
	filename := filepath.Base(url)

	d.downloadsMutex.Lock()
	mutex, ok := d.downloads[filename]

	if !ok {
		mutex = &sync.Mutex{}
		d.downloads[filename] = mutex
	}

	d.downloadsMutex.Unlock()

	mutex.Lock()

	demFilepath := d.Dir + filePathSep + filename

	if d.checkIfDemFileExists(demFilepath) {
		b, err := os.ReadFile(demFilepath)

		if err != nil {
			return "", nil, fmt.Errorf("cannot read %s file. cause: %w", demFilepath, err)
		}

		return demFilepath, b, nil
	}

	token, err := d.Api.GenerateToken()

	if err != nil {
		return "", nil, fmt.Errorf("cannot generate EarthData API token. Cause %w", err)
	}

	client := &http.Client{}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Add("Authorization", "Bearer "+token.AccessToken)

	log.Infof("Downloading file %s from SRTM30m server...", filename)

	start := time.Now()

	resp, err := client.Do(req)

	if err != nil {
		err := fmt.Errorf("cannot download hgt file %s. Cause: %w", filename, err)
		log.Errorf(err.Error())
		mutex.Unlock()
		return "", nil, err
	}

	log.Infof("File %s request completed. Status: %d", filename, resp.StatusCode)

	if resp.StatusCode != 200 {
		mutex.Unlock()

		if resp.StatusCode == 404 {
			d.nonExistentZipFilesMutex.Lock()
			(*d.nonExistentZipFiles)[filename] = true
			d.nonExistentZipFilesMutex.Unlock()

			err := fmt.Errorf("received a %d error during file %s request. Cause %w",
				resp.StatusCode, url, ErrNonExistentDemFile)
			return "", nil, err
		}

		msg := fmt.Sprintf("received a %d error during request", resp.StatusCode)
		return "", nil, errors.New(msg)
	}

	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)

	if err != nil {
		mutex.Unlock()
		return "", nil, fmt.Errorf("cannot download file %s. cause: %w", url, err)
	}

	err = d.saveZipHgtFile(demFilepath, b)

	if err != nil {
		mutex.Unlock()
		return "", nil, fmt.Errorf("cannot save %s file. cause: %w", demFilepath, err)
	}

	duration := time.Since(start)

	log.Infof("File %s downloaded in %s", filename, duration)

	mutex.Unlock()
	return demFilepath, b, nil
}

func (d *Downloader) saveZipHgtFile(path string, bytes []byte) error {
	if d.checkIfDemFileExists(path) {
		return nil
	}

	err := os.WriteFile(path, bytes, 0644)

	if err != nil {
		return fmt.Errorf("cannot save HGT file %s. cause: %w", path, err)
	}

	log.Infof("Zip file saved on %s", path)

	return nil
}

func (d *Downloader) checkIfDemFileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}

	return false
}

func (d *Downloader) unzipDemFile(path string) (string, error) {
	if d.checkIfDemFileExists(strings.ReplaceAll(path, ".zip", "")) {
		return path, nil
	}

	files, err := d.unzip(path, d.Dir)

	if err != nil {
		return "", fmt.Errorf("cannot unzip file %s. Cause %w", path, err)
	}

	if len(files) < 1 {
		return "", fmt.Errorf("cannot uncompress file %s", path)
	}

	log.Infof("File %s is uncompressed", path)

	err = os.Remove(path)
	if err != nil {
		log.Warnf("cannot remove zip file %s. Cause: %s", path, err)
	}

	return files[0], nil
}

func (d *Downloader) unzip(zipFile string, destFolder string) ([]string, error) {
	var files []string

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

func (d *Downloader) isPointInsideDataSet(lon, lat float64) (bool, error) {
	err := d.loadDatasetBbox()

	if err != nil {
		return false, err
	}

	strategy := planar.NormalStrategy()

	for _, feature := range d.datasetBbox.Features {
		contains, err := strategy.Contains(feature.Geometry.Geometry(), space.Point{lon, lat})

		if err != nil {
			return false, err
		}

		if contains {
			return true, nil
		}
	}

	return false, nil
}

func (d *Downloader) loadDatasetBbox() error {
	if d.datasetBbox == nil {
		file, err := os.Open(env.GetBboxFilePath())

		if err != nil {
			return err
		}

		boundingBoxData, err := io.ReadAll(file)

		if err != nil {
			return err
		}

		collection, err := geojson.UnmarshalFeatureCollection(boundingBoxData)
		if err != nil {
			return err
		}

		d.datasetBbox = collection
	}

	return nil
}

func (d *Downloader) isZipFileNonExistent(filename string) bool {
	d.nonExistentZipFilesMutex.Lock()
	_, ok := (*d.nonExistentZipFiles)[filename]
	d.nonExistentZipFilesMutex.Unlock()
	return ok
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

// PartitionSlice partitions a slice into chunks of the given size.
func partitionSlice(slice []*geojson.Feature, chunkSize int) [][]*geojson.Feature {
	if chunkSize <= 0 {
		return nil
	}
	var chunks [][]*geojson.Feature
	for chunkSize < len(slice) {
		slice, chunks = slice[chunkSize:], append(chunks, slice[0:chunkSize:chunkSize])
	}
	return append(chunks, slice)
}
