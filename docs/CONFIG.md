# Configuration

Settings can be defined using:
- a configuration file
- command-line flags
- environment variables

## The configuration file
dbn reads an optional config file at `$XDG_CONFIG_HOME/dbn/config.toml`, or `~/.config/dbn/config.toml` when `XDG_CONFIG_HOME` isn't set (on macOS too). If you don't have a `config.toml` all settings fall back to defaults.

Here is an example `config.toml`:

```toml
port = 7373           # the daemon's port
update-check = true   # false turns the update check off
background = "auto"   # your terminal's background: auto | dark | light

[diff]
preset = "medium"     # subtle | medium | strong: applies to both backgrounds

[diff.dark]           # optional; each key replaces one of the preset's colours on a dark background
removed-change = "#8a3535"

[diff.light]          # optional; the same for a light background
added-line = "#e8f7e8"
```

### Finding and validating the file

`dbn config` prints where the config file is, checks it, and lists each setting with where its value came from, including all eight diff colors.

- **An invalid setting stops dbn.** A file that isn't valid TOML, a bad value, or an   unparseable `DBN_PORT` makes every command exit with an error saying where the problem is: the file, line and key, or the variable. Your agent's dbn tools report the same error when called. Once you fix the file, the next call works without restarting anything.
- **An unknown key only warns** (on stderr, as a notice in the TUI, and in `dbn config`), so a file written for a newer dbn still works with an older one.
- **The file is read at launch.** Relaunch the TUI after editing it.

After making a change to `config.toml`, exit and restart the TUI to apply your preferences.

### Diff colors

```
[diff]
# values go here
```

Customize your diff colors using the `[diff]` table in the config file.

There are two different mechanisms for setting diff colors.
- Presets give you a whole "family" in one setting
- Or you can fully customize your color preferences

Note that only **diff** colors are configurable. The main TUI uses your terminal's background and text color, so it should follow whatever your settings are.

#### Presets

```
[diff]
# applies to light and dark backgrounds
preset = "medium"     # subtle | medium | strong
```

Specify a `preset` to update the full set of colors at once.

Brightness presets (`"medium"`, `"subtle"`, `"strong"`) increase or decrease the brightness of the diff colors.

#### Custom color configuration

```
[diff.dark]           # replace the preset's colors on a dark background
removed-change = "#8a3535"

[diff.light]          # the same for a light background
added-line = "#e8f7e8"
```

To specify a custom color configuration, you define override colors for a color family ("dark" or "light"):

Colors are broken down into four types:
- `added-line`: the full line is new
- `changed-line`: a portion of a line is changed; this is the "added" side
- `removed-line`: the full line is removed
- `removed-change`: a portion of a line is changed; this is the "removed" side

A changed line is tinted across its width, and the characters that changed within it get a stronger tint of the same colour. Each background has four colors: `added-line`, `added-change`, `removed-line` and `removed-change`. The `-line` color is the tint across the whole line, and the `-change` color is the stronger one. Each is a `#rrggbb` background colour.

| Preset             | Background | added-line | added-change | removed-line | removed-change |
| ------------------ | ---------- | ---------- | ------------ | ------------ | -------------- |
| `subtle`           | dark       | `#1f2b1f`  | `#2f4a2f`    | `#2e1f1f`    | `#4f2d2d`      |
| `medium` (default) | dark       | `#213a21`  | `#2f6b2f`    | `#3f2222`    | `#7a2f2f`      |
| `strong`           | dark       | `#1d4a1d`  | `#2e8b2e`    | `#5a1f1f`    | `#a33a3a`      |
| `subtle`           | light      | `#f0f9f0`  | `#c9ecc9`    | `#fbf0f0`    | `#f3c9c9`      |
| `medium` (default) | light      | `#e3f5e3`  | `#abe2ab`    | `#fae3e3`    | `#f0a8a8`      |
| `strong`           | light      | `#cdeecd`  | `#8fd68f`    | `#f6cccc`    | `#e88a8a`      |

#### Troubleshooting unreadable colors
If your diffs are hard to read (e.g. white text on a light background, or dark text on a dark background), read on:

Use the `background` setting to tell dbn about whether your terminal operates in "light mode" or "dark mode." The setting _describes_ your terminal; it doesn't change it. By default, `background` is set to `auto`, which means dbn uses the terminal's own background and text color, and picks the rest of its colors, the diff's included, to match. It asks the terminal whether that background is dark or light. Some terminals and multiplexers don't answer, so dbn then assumes dark. If yours is light and dbn's colors look like they're meant for a dark one, set `background = "light"` (or `background = "dark"` for the reverse case). In most cases, leaving `background = "auto"` is the right choice. If you set it to the opposite of your terminal, dbn will pick colors for a background you don't have, which will probably make the diffs unreadable.

## Environment variables and flags

In addition to `config.toml`, some settings can also come from flags or environment variables. The order of precedence is (highest first):

| Setting      | Flag      | Environment variable    | Config file            | Default |
| ------------ | --------- | ----------------------- | ---------------------- | ------- |
| Port         | `-port N` | `DBN_PORT=N`            | `port = N`             | `7373`  |
| Update check | —         | `DBN_NO_UPDATE_CHECK=1` | `update-check = false` | on      |

Note that the environment variable is the opposite way round from the key: `DBN_NO_UPDATE_CHECK=1` means `update-check = false`. It can only turn the check off: `0`, `false`, `no` and an empty value count as unset, and the config file decides.

Color/theme settings (`background` and `[diff]`) are set in the file only.
