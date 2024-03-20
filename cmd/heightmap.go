package cmd

import (
	"github.com/geovannyAvelar/lukla/heightmap"
	"github.com/spf13/cobra"
	"os"
)

type heightmapCoords struct {
	Latitude   float64
	Longitude  float64
	Side       int
	Resolution int
}

func CreateHeightMapCommand() *cobra.Command {
	heightmap := &cobra.Command{
		Use:   "heightmap",
		Short: "Heightmap creation command",
		Long:  "Heightmap creation command. Receives a coordinate in WGS84 and draw a square based on this coordinate and on side parameter",
		Run:   createHeightmap,
	}

	heightmap.Flags().Float64("latitude", 0.0, "Square initial latitude")
	heightmap.Flags().Float64("longitude", 0.0, "Square initial longitude")
	heightmap.Flags().Int("side", 1000, "Side of the square in meters")
	heightmap.Flags().Int("resolution", 256, "PNG image resolution")
	heightmap.Flags().Bool("interpolate", false, "Apply image resizing even when original image resolution is smaller than informed resolution")
	heightmap.Flags().StringP("output", "o", "heightmap.png", "PNG image output path")
	heightmap.Flags().StringVar(&dotenvPath, "env", "", "Dot env file path")
	heightmap.Flags().StringVar(&demPath, "dem-path", "", "Digital Elevation Model (DEM) files path")
	heightmap.Flags().IntVar(&httpClientTimeout, "http-client-timeout", 0, "HTTP client request timeout")
	heightmap.Flags().StringVar(&earthdataUser, "earthdata-user", "", "Earthdata API username")
	heightmap.Flags().StringVar(&earthdataPassword, "earthdata-password", "", "Earthdata API password")

	return heightmap
}

func createHeightmap(cmd *cobra.Command, args []string) {
	if dotenvPath != "" {
		loadDotEnv(dotenvPath)
	}

	coords := parseCoordinateAndResParams(cmd)

	h := createHgtDataDir()
	defer h.Close()

	httpClient := createHttpClient()
	earthdataApi := createEarthdataApiClient(httpClient)
	srtmDownloader := createSrtmDownloader(httpClient, earthdataApi)
	heightmapGen := &heightmap.Generator{
		ElevationDataset: h,
		SrtmDownloader:   srtmDownloader,
		Dir:              "./",
	}

	interpolate, err := cmd.Flags().GetBool("interpolate")

	if err != nil {
		handleErr(err)
	}

	b, err := heightmapGen.CreateHeightMapImage(coords.Latitude, coords.Longitude, coords.Side,
		heightmap.ResolutionConfig{Width: coords.Resolution, Height: coords.Resolution,
			IgnoreWhenOriginalImageIsSmaller: !interpolate})

	if err != nil {
		handleErr(err)
	}

	output, err := cmd.Flags().GetString("output")

	if err != nil {
		handleErr(err)
	}

	err = os.WriteFile(output, b, 0644)

	if err != nil {
		handleErr(err)
	}
}

func parseCoordinateAndResParams(cmd *cobra.Command) heightmapCoords {
	lat, err := cmd.Flags().GetFloat64("latitude")

	if err != nil {
		handleErr(err)
	}

	lon, err := cmd.Flags().GetFloat64("longitude")

	if err != nil {
		handleErr(err)
	}

	side, err := cmd.Flags().GetInt("side")

	if err != nil {
		handleErr(err)
	}

	res, err := cmd.Flags().GetInt("resolution")

	if err != nil {
		handleErr(err)
	}

	return heightmapCoords{
		Latitude:   lat,
		Longitude:  lon,
		Side:       side,
		Resolution: res,
	}
}
