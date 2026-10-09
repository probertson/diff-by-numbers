package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"text/tabwriter"

	"github.com/probertson/diff-by-numbers/internal/config"
)

// runConfig is `dbn config [-port N]`: where the config file is, whether it is
// valid, and each setting's effective value with where it came from — so "why is
// dbn on that port?" is one command, not an investigation. -port is accepted so a
// flag's effect can be seen like any other source's.
func runConfig(args []string, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("config", flag.ContinueOnError)
	portFor := portFlag(flags, "show the port as this flag would set it")
	if err := flags.Parse(args); err != nil {
		return err
	}

	path := config.Path(os.LookupEnv)
	if path == "" {
		fmt.Fprintln(out, "config file: none (neither XDG_CONFIG_HOME nor HOME is set)")
	} else {
		fmt.Fprintf(out, "config file: %s\n", path)
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(out, "  (no file there; using defaults)")
		}
	}

	// An invalid file is reported exactly as any other command would report it,
	// and fails the same way, so this is also how to check an edit.
	settings, err := config.Load(os.LookupEnv)
	if err != nil {
		return err
	}
	port, err := portFor(settings)
	if err != nil {
		return err
	}
	warn(settings, errOut)

	fmt.Fprintln(out)
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	diff := settings.Diff
	label := func(source config.Source, env, flagName string) string {
		return sourceLabel(source, env, flagName, diff.Preset.Value)
	}
	fmt.Fprintf(table, "port\t%d\t%s\n", port.Value, label(port.Source, config.PortEnv, "-port flag"))
	fmt.Fprintf(table, "update-check\t%t\t%s\n", settings.UpdateCheck.Value, label(settings.UpdateCheck.Source, config.NoUpdateCheckEnv, ""))
	fmt.Fprintf(table, "background\t%s\t%s\n", settings.TerminalBackground.Value, label(settings.TerminalBackground.Source, "", ""))
	fmt.Fprintf(table, "diff.preset\t%s\t%s\n", diff.Preset.Value, label(diff.Preset.Source, "", ""))
	// Every colour, not only the ones set: what is drawn is the question, and a
	// preset's colours are part of the answer.
	for _, background := range []struct {
		name    string
		colours config.Colours
	}{{"dark", diff.Dark}, {"light", diff.Light}} {
		for _, role := range background.colours.Roles() {
			fmt.Fprintf(table, "diff.%s.%s\t%s\t%s\n", background.name, role.Key, role.Colour.Value, label(role.Colour.Source, "", ""))
		}
	}
	return table.Flush()
}

// sourceLabel says where a setting came from in the words the Reviewer would
// use to change it. preset names the diff preset, for a colour that comes from
// it.
func sourceLabel(source config.Source, env, flagName, preset string) string {
	switch source {
	case config.Preset:
		return preset + " preset"
	case config.File:
		return "config file"
	case config.Env:
		return "$" + env
	case config.Flag:
		return flagName
	default:
		return "default"
	}
}
