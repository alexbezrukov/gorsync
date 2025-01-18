package cmd

import (
	"gorsync/cmd/client"
	"gorsync/cmd/server"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gorsync",
	Short: "File synchronisation tool over TCP",
	Long:  "A CLI tool for synchronisation files and directories over a TCP connection",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(server.ServerCmd)
	rootCmd.AddCommand(client.ClientCmd)
}
