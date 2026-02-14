package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	configFile string
	format     string

	Version = "dev"
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "paraspeech",
		Short: "ParaSpeech — unified STT/TTS service",
	}

	root.PersistentFlags().StringVar(&configFile, "config", "", "config file path")
	root.PersistentFlags().StringVar(&format, "format", "prototext", "output format: prototext, json")

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
			fmt.Fprintf(os.Stdout, "version: %q\n", Version)
		},
	}
}
