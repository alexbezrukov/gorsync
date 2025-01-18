package server

import (
	"gorsync/internal/server"
	"log"

	"github.com/spf13/cobra"
)

var (
	destDir string
)

var ServerCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the file synchronization server",
	Run: func(cmd *cobra.Command, args []string) {
		if destDir == "" {
			log.Fatalf("Error: Destination directory must be specified using the --dest flag")
		}

		if err := server.Start(":8080", destDir); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	},
}

func init() {
	// Add the destination directory flag
	ServerCmd.Flags().StringVarP(&destDir, "dest", "d", "", "Destination directory for received files")
}
