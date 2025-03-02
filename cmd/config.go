package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
)

// Config holds shared configuration values for commands
type Config struct {
	// Default paths
	DefaultDir string
}

// NewConfig initializes shared configuration values
func NewConfig() Config {
	home, err := homedir.Dir()
	if err != nil {
		fmt.Println("Error finding home directory:", err)
		os.Exit(1)
	}

	return Config{
		DefaultDir: filepath.Join(home, "Downloads", "adpworld.adp.com"),
	}
}
