package client

import (
	"gorsync/internal/client"
	"log"

	"github.com/spf13/cobra"
)

var sourcePath, destAddr string
var recursive bool

var ClientCmd = &cobra.Command{
	Use:   "client",
	Short: "Start the file synchronization client",
	Run: func(cmd *cobra.Command, args []string) {
		if err := client.Start(sourcePath, destAddr, recursive); err != nil {
			log.Fatalf("Client error: %v", err)
		}
	},
}

func init() {
	ClientCmd.Flags().StringVar(&sourcePath, "source", "", "Source path to sync (file or directory)")
	ClientCmd.Flags().StringVar(&destAddr, "dest", "", "Destination address (e.g, 192.168.1.5:8080)")
	ClientCmd.Flags().BoolVarP(&recursive, "recursive", "R", false, "Sync a directory recursively")
	_ = ClientCmd.MarkFlagRequired("source")
	_ = ClientCmd.MarkFlagRequired("dest")
}
