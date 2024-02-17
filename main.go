package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Title is the program title
const Title = "rainbow-roads"

// Version is the current version of the program
var Version string

// rootCmd is the root command for the application
var rootCmd = &cobra.Command{
	Use:               Title,
	Version:           Version,
	Short:             Title + ": Animate your exercise maps!",
	CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
}

func main() {
	// Initialize the default help command for rootCmd
	rootCmd.InitDefaultHelpCmd()

	// Check if the command is unknown and append it to the custom wormsCmd
	if _, _, err := rootCmd.Find(os.Args[1:]); err != nil && strings.HasPrefix(err.Error(), "unknown command ") {
		rootCmd.SetArgs(append([]string{wormsCmd.Name()}, os.Args[1:]...))
	}

	// Execute the root command and exit if an error occurs
	if rootCmd.Execute() != nil {
		os.Exit(1)
	}
}
