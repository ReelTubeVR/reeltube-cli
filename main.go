package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

// Define these variables during the build process using -ldflags
var (
	version = "dev" // Default to 'dev' if not set during build
	commit  = "none"
	date    = "unknown"
)

var apiKey, baseURL string
var client *Client
var debug bool

var rootCmd = &cobra.Command{
	Use:   "reeltube",
	Short: "Reeltube is a CLI for interacting with the Reeltube API",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if apiKey == "" && cmd.Use != "version" {
			apiKey = os.Getenv("REELTUBE_API_KEY")
			if apiKey == "" {
				fmt.Println("API key must be provided via --api-key flag or REELTUBE_API_KEY environment variable")
				os.Exit(1)
			}
		}

		client = NewClient(baseURL, apiKey, debug)
	},
}

// VersionInfo returns a string containing the version, commit, and build date
func VersionInfo() string {
	return "Version: " + version + ", Commit: " + commit + ", Build date: " + date
}

var whoamiCmd = &cobra.Command{
	Use:     "whoami",
	Aliases: []string{"me"},
	Short:   "Ping the ReelTube API to verify authentication",
	Run: func(cmd *cobra.Command, args []string) {
		// client := NewClient(baseURL, apiKey)
		data, err := client.Me()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Println(data.Profile.Handle)
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of Reeltube CLI",
	Long:  `All software has versions. This is Reeltube's.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(VersionInfo())
	},
}

func main() {
	rootCmd.PersistentFlags().StringVarP(&apiKey, "api-key", "k", "", "API key for authentication (required unless REELTUBE_API_KEY is set)")
	rootCmd.PersistentFlags().StringVarP(&baseURL, "base-url", "u", "https://api.reel.tube", "Base URL of the API")
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable debug mode")

	err := godotenv.Load()
	if debug && err != nil {
		log.Println("No .env file found. Assuming environment variables are set by the system.")
	}

	rootCmd.AddCommand(whoamiCmd)
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
