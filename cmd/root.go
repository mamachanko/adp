package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// SetupLogger configures and sets the default logger for the application
func SetupLogger() {
	logger := log.NewWithOptions(os.Stderr, log.Options{
		ReportCaller:    false,
		ReportTimestamp: true,
		Prefix:          "ADP",
	})
	log.SetDefault(logger)
}

// Execute sets up and runs the root command
func Execute() {
	// Initialize configuration
	config := NewConfig()

	// Set up logging
	SetupLogger()

	// Create root command
	rootCmd := NewRootCmd(config)

	// Add subcommands
	rootCmd.AddCommand(NewDownloadCmd(config))
	rootCmd.AddCommand(NewProcessCmd(config))

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// NewRootCmd creates and configures the root command
func NewRootCmd(config Config) *cobra.Command {
	// Create root command
	rootCmd := &cobra.Command{
		Use:   "adp",
		Short: "ADP document downloader and processor",
		Long: `A tool to download and process documents from ADP.
It can download PDFs from adpworld.adp.com and process them locally.`,
	}

	// Add subcommands
	rootCmd.AddCommand(NewDownloadCmd(config))
	rootCmd.AddCommand(NewProcessCmd(config))

	return rootCmd
}
