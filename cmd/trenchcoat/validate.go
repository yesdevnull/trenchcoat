package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yesdevnull/trenchcoat/internal/coat"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "validate <path>...",
		Short:         "Validate one or more coat files for schema correctness",
		Args:          cobra.MinimumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, errs := coat.LoadPaths(args)
			if len(errs) > 0 {
				for _, e := range errs {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", e)
				}
				return fmt.Errorf("validation failed with %d error(s)", len(errs))
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "all coat files are valid")
			return nil
		},
	}
}
