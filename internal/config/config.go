// Package config reads dbn's settings: the config file, the environment
// variables that predate it, and the defaults beneath both. A command-line flag
// outranks all three, but flags belong to each subcommand, so the caller applies
// them on top of what Load returns.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// The environment variables that predate the config file. Both still work, and
// outrank it.
const (
	PortEnv          = "DBN_PORT"
	NoUpdateCheckEnv = "DBN_NO_UPDATE_CHECK"
)

// Source is where a setting's value came from, so `dbn config` can say why a
// setting is what it is.
type Source string

const (
	Default Source = "default"
	File    Source = "file"
	Env     Source = "env"
	Flag    Source = "flag"
)

// Setting is one setting's effective value and where it came from.
type Setting[T comparable] struct {
	Value  T
	Source Source
}

// Settings is everything dbn can be configured with.
type Settings struct {
	// Path is where the config file is looked for, whether or not one is there.
	// Empty when there is nowhere to look: no XDG_CONFIG_HOME and no HOME.
	Path string

	Port        Setting[int]
	UpdateCheck Setting[bool]

	// unknown is the keys in the file dbn does not know. Not an error: the
	// repo-built dbn and the installed one share the file, and a key only the
	// newer one knows must not stop the older one.
	unknown []string
}

// Warning is the one line that tells the Reviewer about keys dbn ignored, or
// empty when there were none.
func (s Settings) Warning() string {
	if len(s.unknown) == 0 {
		return ""
	}
	quoted := make([]string, len(s.unknown))
	for i, key := range s.unknown {
		quoted[i] = strconv.Quote(key)
	}
	noun := "key"
	if len(s.unknown) > 1 {
		noun = "keys"
	}
	return fmt.Sprintf("unknown %s %s in %s", noun, strings.Join(quoted, ", "), s.Path)
}

// fileShape is the config file as decoded. Pointers, so a key the file leaves
// out is told apart from one set to its zero value.
type fileShape struct {
	Port        *port    `toml:"port"`
	UpdateCheck *boolean `toml:"update-check"`
}

// boolean is a true/false value checked as it is decoded, for the same reason
// as port: the decoder's own type error carries no line.
type boolean bool

func (b *boolean) UnmarshalTOML(data any) error {
	value, ok := data.(bool)
	if !ok {
		return fmt.Errorf("must be true or false, got %v", data)
	}
	*b = boolean(value)
	return nil
}

// port is a port number checked as it is decoded, so the decoder's error carries
// the line it came from.
type port int

func (p *port) UnmarshalTOML(data any) error {
	number, ok := data.(int64)
	if !ok {
		return fmt.Errorf("must be a number, got %v", data)
	}
	if err := CheckPort(int(number)); err != nil {
		return err
	}
	*p = port(number)
	return nil
}

// CheckPort reports whether n can be a port dbn listens on. Exported so a
// -port flag is held to the same rule as the file and DBN_PORT.
func CheckPort(n int) error {
	if n < 1 || n > 65535 {
		return fmt.Errorf("must be between 1 and 65535, got %d", n)
	}
	return nil
}

// Load reads the settings. lookup is how the environment is read — os.LookupEnv
// outside a test.
func Load(lookup func(string) (string, bool)) (Settings, error) {
	settings := Settings{
		Path:        Path(lookup),
		Port:        Setting[int]{Value: daemon.DefaultPort, Source: Default},
		UpdateCheck: Setting[bool]{Value: true, Source: Default},
	}

	if settings.Path != "" {
		var file fileShape
		meta, err := toml.DecodeFile(settings.Path, &file)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// No file is not a mistake: every setting keeps its default.
		case err != nil:
			return Settings{}, fileError(settings.Path, err)
		default:
			if file.Port != nil {
				settings.Port = Setting[int]{Value: int(*file.Port), Source: File}
			}
			if file.UpdateCheck != nil {
				settings.UpdateCheck = Setting[bool]{Value: bool(*file.UpdateCheck), Source: File}
			}
			settings.unknown = unknownKeys(meta.Undecoded())
		}
	}

	if raw, _ := lookup(PortEnv); raw != "" {
		number, err := strconv.Atoi(raw)
		if err != nil {
			return Settings{}, fmt.Errorf("%s=%q is not a port number", PortEnv, raw)
		}
		if err := CheckPort(number); err != nil {
			return Settings{}, fmt.Errorf("%s=%q: %w", PortEnv, raw, err)
		}
		settings.Port = Setting[int]{Value: number, Source: Env}
	}
	if raw, _ := lookup(NoUpdateCheckEnv); optsOut(raw) {
		settings.UpdateCheck = Setting[bool]{Value: false, Source: Env}
	}

	return settings, nil
}

// unknownKeys names each key dbn did not read, leaving out those inside a table
// that is itself unknown: naming the table says it all.
func unknownKeys(undecoded []toml.Key) []string {
	var names []string
	seen := map[string]bool{}
	for _, key := range undecoded {
		if len(key) > 1 && seen[key[:len(key)-1].String()] {
			seen[key.String()] = true
			continue
		}
		seen[key.String()] = true
		names = append(names, key.String())
	}
	return names
}

// fileError says what is wrong with the config file, and where: the path, then
// the line and key when the decoder knows them.
func fileError(path string, err error) error {
	var parse toml.ParseError
	if !errors.As(err, &parse) {
		return fmt.Errorf("config file %s: %w", path, err)
	}
	where := fmt.Sprintf("config file %s, line %d", path, parse.Position.Line)
	if parse.LastKey != "" {
		where += fmt.Sprintf(", %s", parse.LastKey)
	}
	return fmt.Errorf("%s: %s", where, parse.Message)
}

// optsOut reports whether a DBN_NO_UPDATE_CHECK value turns the check off. Any
// value but the ones that plainly mean "no" counts, so =1, =true and =yes all
// do what they look like they do. The variable can only turn the check off: a
// "no" leaves it to the file, since a variable named "no update check" must
// never be what switches the check on.
func optsOut(raw string) bool {
	switch raw {
	case "", "0", "false", "no":
		return false
	default:
		return true
	}
}

// Path is where the config file lives: under XDG_CONFIG_HOME when that is set,
// otherwise ~/.config — on macOS too, where terminal tools are looked for there
// rather than in ~/Library/Application Support (which os.UserConfigDir gives).
// A relative XDG_CONFIG_HOME is ignored, as the XDG spec says it must be.
func Path(lookup func(string) (string, bool)) string {
	if xdg, _ := lookup("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "dbn", "config.toml")
	}
	home, _ := lookup("HOME")
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "dbn", "config.toml")
}
