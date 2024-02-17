package main

import (
	"fmt"

	"github.com/NathanBaulch/rainbow-roads/paint"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	// paintOpts are the options to make paint map
	paintOpts = &paint.Options{
		Title:   Title,
		Version: Version,
	}
	// paintCmd represents the "paint" command
	paintCmd = &cobra.Command{
		Use:   "paint",
		Short: "Track coverage in a region of interest",
		// Pre-checks to ensure value are in bounds
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if paintOpts.Width == 0 {
				return flagError("width", paintOpts.Width, "must be positive")
			}
			return nil
		},
		// Run the command
		RunE: func(_ *cobra.Command, args []string) error {
			paintOpts.Input = args
			return paint.Run(paintOpts)
		},
	}
)

func init() {
	// Add the "paint" command to the root command
	rootCmd.AddCommand(paintCmd)

	// General flags (region and output location)
	general := &pflag.FlagSet{}
	general.VarP((*CircleFlag)(&paintOpts.Region), "region", "r", "target region of interest, eg -37.8,144.9,10km")
	general.StringVarP(&paintOpts.Output, "output", "o", "out", "optional path of the generated file")
	general.VisitAll(func(f *pflag.Flag) { paintCmd.Flags().Var(f.Value, f.Name, f.Usage) })
	_ = paintCmd.MarkFlagRequired("region")

	// Rendering flags
	rendering := &pflag.FlagSet{}
	rendering.UintVarP(&paintOpts.Width, "width", "w", 1000, "width of the generated image in pixels")
	rendering.BoolVar(&paintOpts.NoWatermark, "no_watermark", false, "suppress the embedded project name and version string")
	rendering.BoolVar(&paintOpts.Minimalist, "minimal", false, "only paint the paths of the activities")
	rendering.VisitAll(func(f *pflag.Flag) { paintCmd.Flags().Var(f.Value, f.Name, f.Usage) })

	// Filtering flags
	filters := filterFlagSet(&paintOpts.Selector)
	filters.VisitAll(func(f *pflag.Flag) { paintCmd.Flags().Var(f.Value, f.Name, f.Usage) })

	// Prints the help command
	paintCmd.SetUsageFunc(func(*cobra.Command) error {
		fmt.Fprintln(paintCmd.OutOrStderr())
		fmt.Fprintln(paintCmd.OutOrStderr(), "Usage:")
		fmt.Fprintln(paintCmd.OutOrStderr(), " ", paintCmd.UseLine(), "[input]")
		fmt.Fprintln(paintCmd.OutOrStderr())
		fmt.Fprintln(paintCmd.OutOrStderr(), "General flags:")
		fmt.Fprintln(paintCmd.OutOrStderr(), general.FlagUsages())
		fmt.Fprintln(paintCmd.OutOrStderr(), "Filtering flags:")
		fmt.Fprintln(paintCmd.OutOrStderr(), filters.FlagUsages())
		fmt.Fprintln(paintCmd.OutOrStderr(), "Rendering flags:")
		fmt.Fprint(paintCmd.OutOrStderr(), rendering.FlagUsages())
		return nil
	})
}
