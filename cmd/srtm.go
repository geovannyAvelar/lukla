package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/petoc/hgt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
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

	// Ctrl+C (or a SIGTERM, e.g. from a container orchestrator) now stops the
	// download gracefully - in-flight granules finish or abort cleanly via
	// ctx, instead of the process being killed mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result, err := srtmDownloader.DownloadAllDemFiles(ctx)

	if err != nil {
		log.Errorf("Cannot download SRTM30m dataset. Cause: %s", err)
		os.Exit(1)
	}

	log.Infof("Download finished: %d/%d succeeded, %d failed, canceled=%t",
		result.Succeeded, result.Total, result.Failed, result.Canceled)

	if result.Failed > 0 {
		os.Exit(1)
	}
}
