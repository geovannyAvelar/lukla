package cmd

import (
	"github.com/petoc/hgt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"os"
)

func CreateSrtmCommand() *cobra.Command {
	srtm := &cobra.Command{
		Use:   "srtm",
		Short: "SRTM30m dataset operations",
		Long:  "SRTM30m dataset operations",
	}

	srtm.AddCommand(CreateSrtmDownloadCommand())

	return srtm
}

func CreateSrtmDownloadCommand() *cobra.Command {
	download := &cobra.Command{
		Use:   "download",
		Short: "Download SRTM30m dataset",
		Long:  "Download SRTM30m dataset",
		Run:   downloadAllSrtmFiles,
	}

	download.Flags().StringVar(&dotenvPath, "env", "", "Dot env file path")
	download.Flags().StringVar(&demPath, "dem-path", "", "Digital Elevation Model (DEM) files path")
	download.Flags().IntVar(&httpClientTimeout, "http-client-timeout", 0, "HTTP client request timeout")
	download.Flags().StringVar(&earthdataUser, "earthdata-user", "", "Earthdata API username")
	download.Flags().StringVar(&earthdataPassword, "earthdata-password", "", "Earthdata API password")

	return download
}

func downloadAllSrtmFiles(cmd *cobra.Command, args []string) {
	if dotenvPath != "" {
		loadDotEnv(dotenvPath)
	}

	h := createHgtDataDir()
	defer func(h *hgt.DataDir) {
		err := h.Close()
		if err != nil {
			log.Errorf("Error closing hgt data dir. Cause: %s", err)
		}
	}(h)

	httpClient := createHttpClient()
	earthdataApi := createEarthdataApiClient(httpClient)
	srtmDownloader := createSrtmDownloader(httpClient, earthdataApi)

	err := srtmDownloader.DownloadAllDemFiles()

	if err != nil {
		log.Errorf("Cannot download SRTM30m dataset. Cause: %s", err)
		os.Exit(1)
	}
}
