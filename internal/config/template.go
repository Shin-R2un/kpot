package config

// StarterTemplate is the comment-rich config.toml that `kpot config
// init` writes for first-time users. Every key is shown either as a
// default-matching live value or commented out with an example, so
// `kpot config show` and the file's contents stay aligned.
//
// Keep this in sync with the Configuration section of README.md and
// the user-visible defaults in this package — the file is the
// canonical reference users edit.
const StarterTemplate = `# kpot config — full reference at:
#   https://github.com/Shin-R2un/kpot#configuration

# Where bare-name vault arguments resolve.
# kpot personal -> <vault_dir>/personal.kpot
# Tilde is expanded at load time.
vault_dir = "~/.kpot"

# Opened by bare 'kpot' with no positional argument.
# Goes through the same name resolution as a CLI argument.
# Empty / absent: bare 'kpot' prints usage instead.
# default_vault = "personal"

# REPL auto-closes after N minutes of no command activity.
# Single-shot subcommands (kpot vault.kpot ls, etc.) are unaffected.
idle_lock_minutes = 10

# OS keychain caching of the vault decryption key:
#   "auto"   — prompt once per vault on first cache miss (default)
#   "always" — cache silently after every successful open
#   "never"  — never read or write the keychain
keychain = "auto"

# Clipboard auto-clear seconds. 30 is a sane default; raise if you
# routinely paste into slow apps.
clipboard_clear_seconds = 30

# Editor preferred over $EDITOR / $VISUAL when 'note <name>' opens
# an entry. Empty / absent: fall back to environment variables and
# then the built-in candidates (nano / vim / vi on Unix, notepad
# on Windows).
# editor = "vim"
`
