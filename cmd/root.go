package cmd

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "lukla",
		Short: "Lukla is a tool to create real world heightmaps",
		Long:  "Lukla is a tool to create real world heightmaps based on Shuttle Radar Topography Mission (SRTM30m) digital elevation model.",
	}
)

func Execute() error {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(CreateRestCommand())
	rootCmd.AddCommand(CreateHeightMapCommand())
	rootCmd.AddCommand(CreateSrtmCommand())
}
