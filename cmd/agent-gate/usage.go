package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// usageCategoryAnnotation marks a flag as belonging to a specific help-text
// section. The only value we use today is "advanced", which moves the flag
// into a trailing "Advanced Flags:" block instead of the default "Flags:"
// block. Cobra has no native support for this, so the run command sets a
// custom UsageFunc that calls renderUsage below.
const (
	usageCategoryAnnotation = "agent-gate.category"
	usageCategoryAdvanced   = "advanced"
)

// markAdvanced annotates the named flags as advanced. Panics on missing
// flags — we'd rather discover a typo at startup than ship help text that
// silently put a flag in the wrong section.
func markAdvanced(fs *pflag.FlagSet, names ...string) {
	for _, n := range names {
		if err := fs.SetAnnotation(n, usageCategoryAnnotation, []string{usageCategoryAdvanced}); err != nil {
			panic(fmt.Sprintf("markAdvanced: flag %q: %v", n, err))
		}
	}
}

// renderUsage is a drop-in cobra UsageFunc that splits a command's local
// flags into "Flags:" and "Advanced Flags:" blocks. Common flags appear
// first; advanced flags trail in their own section so users see the
// everyday options before the niche ones. Cobra's default Long/Short
// handling is preserved by HelpFunc — this only owns the usage block.
func renderUsage(cmd *cobra.Command) error {
	return writeUsage(cmd, cmd.OutOrStderr())
}

func writeUsage(cmd *cobra.Command, w io.Writer) error {
	fmt.Fprintf(w, "Usage:\n  %s\n", cmd.UseLine())
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(w, "  %s [command]\n", cmd.CommandPath())
	}

	var common, advanced []*pflag.Flag
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		if isAdvancedFlag(f) {
			advanced = append(advanced, f)
		} else {
			common = append(common, f)
		}
	})

	if len(common) > 0 {
		fmt.Fprintln(w, "\nFlags:")
		fmt.Fprint(w, flagBlockUsages(common))
	}
	if len(advanced) > 0 {
		fmt.Fprintln(w, "\nAdvanced Flags:")
		fmt.Fprint(w, flagBlockUsages(advanced))
	}

	if cmd.HasAvailableInheritedFlags() {
		fmt.Fprintln(w, "\nGlobal Flags:")
		fmt.Fprint(w, cmd.InheritedFlags().FlagUsages())
	}
	return nil
}

func isAdvancedFlag(f *pflag.Flag) bool {
	vals, ok := f.Annotations[usageCategoryAnnotation]
	return ok && len(vals) > 0 && vals[0] == usageCategoryAdvanced
}

// flagBlockUsages renders just the given flags through pflag's standard
// column-aligned formatter. We add each flag pointer to a fresh FlagSet
// purely to leverage FlagUsages() — the flags' values stay bound to the
// real command's FlagSet, so this doesn't double-register or affect
// parsing.
func flagBlockUsages(flags []*pflag.Flag) string {
	fs := pflag.NewFlagSet("", pflag.ContinueOnError)
	fs.SortFlags = true
	for _, f := range flags {
		fs.AddFlag(f)
	}
	return fs.FlagUsages()
}
