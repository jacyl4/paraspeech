package cli

import (
	"fmt"
	"os"

	"paraspeech/internal/version"

	"github.com/spf13/cobra"
)

var (
	configFile string
	format     string
	showVersion bool
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "paraspeech",
		Short: "ParaSpeech — unified STT/TTS service",
		Run: func(cmd *cobra.Command, args []string) {
			if showVersion {
				printVersion()
				return
			}
			_ = cmd.Help()
		},
	}

	root.PersistentFlags().StringVar(&configFile, "config", "", "config file path")
	root.PersistentFlags().StringVar(&format, "format", "prototext", "output format: prototext, json")
	root.PersistentFlags().BoolVarP(&showVersion, "version", "v", false, "print version")

	root.AddCommand(newServeCmd())
	root.AddCommand(newTranscribeCmd())
	root.AddCommand(newSynthesizeCmd())
	root.AddCommand(newHealthCmd())
	root.AddCommand(newVersionCmd())

	return root
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			printVersion()
		},
	}
}

func printVersion() {
	fmt.Fprintf(
		os.Stdout,
		"version: %q\ncommit: %q\nbuild_time: %q\n",
		version.Version,
		version.Commit,
		version.BuildTime,
	)
}
