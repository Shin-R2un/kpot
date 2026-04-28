package repl

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/Shin-R2un/kpot/internal/bundle"
	"github.com/Shin-R2un/kpot/internal/clipboard"
	"github.com/Shin-R2un/kpot/internal/crypto"
	"github.com/Shin-R2un/kpot/internal/editor"
	"github.com/Shin-R2un/kpot/internal/fields"
	"github.com/Shin-R2un/kpot/internal/notefmt"
	"github.com/Shin-R2un/kpot/internal/store"
	"github.com/Shin-R2un/kpot/internal/tty"
	"github.com/Shin-R2un/kpot/internal/vault"
)

type Session struct {
	Path  string
	Vault *store.DecryptedVault
	Key   []byte
	Hdr   *vault.Header

	// currentNote is the canonical name of the note the user has
	// `cd`-ed into, or "" when at vault root. Context-aware
	// commands (show, cp, set, unset, fields) read this to default
	// their target.
	currentNote string

	out  io.Writer
	err  io.Writer
	clip *clipboard.Manager
	p    prompter
	opts SessionOptions

	idleMu    sync.Mutex
	idleTimer *time.Timer
}

// SessionOptions tunes a Session at construction time. All fields are
// optional — the zero value reproduces v0.1 defaults.
type SessionOptions struct {
	// ClipboardTTL overrides the 30s clipboard auto-clear default.
	// Zero = use the package default.
	ClipboardTTL time.Duration

	// OnRekey, if non-nil, is called after a successful passphrase
	// rotation with the PREVIOUS vault version (1 or 2). The cmd
	// layer wires this to keychain invalidation: v1 rekey changes
	// the payload key (cache becomes stale), v2 rekey preserves the
	// DEK (cache stays valid). Errors from this callback are
	// swallowed — caching is best-effort.
	OnRekey func(prevVersion int)

	// IdleTimeout, when > 0, force-closes the session after that
	// duration of no command activity. The timer resets on every
	// dispatched command. Zero = no idle lock (the test default;
	// production cmd layer wires config.IdleTimeout in).
	IdleTimeout time.Duration
}

// NewSession builds an interactive session. Use NewSessionWith to pass
// options; this convenience preserves the old call shape so existing
// tests don't churn.
func NewSession(path string, v *store.DecryptedVault, key []byte, hdr *vault.Header) *Session {
	return NewSessionWith(path, v, key, hdr, SessionOptions{})
}

// NewSessionWith builds a session with explicit options (clipboard TTL,
// future knobs). Defaults match NewSession.
func NewSessionWith(path string, v *store.DecryptedVault, key []byte, hdr *vault.Header, opts SessionOptions) *Session {
	s := &Session{
		Path:  path,
		Vault: v,
		Key:   key,
		Hdr:   hdr,
		out:   os.Stdout,
		err:   os.Stderr,
		clip:  newClipboard(opts.ClipboardTTL),
		opts:  opts,
	}
	// Default to bufio (works for tests and piped stdin). Run() upgrades
	// to liner when stdin is a real TTY so users get TAB completion.
	s.p = newBufioPrompter(tty.SharedStdin(), s.out)
	return s
}

// newClipboard wraps Detect with a never-fail Manager. If no backend is
// available, copy commands return clipboard.ErrUnavailable lazily, so a
// REPL on a headless box still works for everything else.
func newClipboard(ttl time.Duration) *clipboard.Manager {
	cb, _ := clipboard.Detect()
	return clipboard.NewManager(cb, ttl)
}

func (s *Session) Close() {
	if s.p != nil {
		_ = s.p.Close()
	}
	if s.clip != nil {
		_ = s.clip.Close()
	}
	crypto.Zero(s.Key)
	s.Key = nil
	s.Vault = nil
}

func (s *Session) prompt() string {
	base := strings.TrimSuffix(filepath.Base(s.Path), filepath.Ext(s.Path))
	if s.currentNote != "" {
		return fmt.Sprintf("kpot:%s/%s> ", base, s.currentNote)
	}
	return fmt.Sprintf("kpot:%s> ", base)
}

func (s *Session) Run() error {
	// Upgrade to liner only when stdin is a real TTY. Otherwise leave
	// the bufio prompter in place so piped tests / `<<EOF` heredocs work.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		_ = s.p.Close()
		s.p = newLinerPrompter(s.completer())
	}

	// Idle lock: only arm when stdin is a TTY (otherwise heredoc tests
	// hit the lock during their natural delays). When armed, the timer
	// callback wipes the key and exits the process — there's no clean
	// way to interrupt liner's blocking Prompt() call from outside.
	if s.opts.IdleTimeout > 0 && term.IsTerminal(int(os.Stdin.Fd())) {
		s.armIdleLock(s.opts.IdleTimeout)
		defer s.disarmIdleLock()
	}

	fmt.Fprintf(s.out, "Opened %s (%d notes)\n", s.Path, len(s.Vault.Notes))
	fmt.Fprintln(s.out, "Type 'help' for commands, 'exit' to quit.")
	for {
		line, err := s.p.Prompt(s.prompt())
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(s.out)
				return nil
			}
			if errors.Is(err, errAbort) {
				// Ctrl-C: discard the in-progress line, keep the REPL alive.
				s.resetIdleLock()
				continue
			}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			s.resetIdleLock()
			continue
		}
		s.p.AddHistory(line)
		s.resetIdleLock()
		args := strings.Fields(line)
		cmd, args := args[0], args[1:]

		stop, err := s.dispatch(cmd, args)
		if err != nil {
			fmt.Fprintf(s.err, "error: %v\n", err)
		}
		if stop {
			return nil
		}
	}
}

// armIdleLock starts a one-shot timer that closes the session and
// exits the process if no activity arrives within d. Using AfterFunc
// (not a select-loop) because the prompter's Prompt() call blocks on
// stdin in raw mode — we can't multiplex it cleanly.
func (s *Session) armIdleLock(d time.Duration) {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	s.idleTimer = time.AfterFunc(d, s.idleFire)
}

func (s *Session) resetIdleLock() {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	if s.idleTimer != nil {
		s.idleTimer.Reset(s.opts.IdleTimeout)
	}
}

func (s *Session) disarmIdleLock() {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
}

// idleFire runs in the timer's goroutine when the user has been idle
// past the threshold. We can't return cleanly from Run() because it's
// blocked in Prompt(); the only safe action is to wipe key material
// and exit the process. The OS reclaims everything else.
func (s *Session) idleFire() {
	fmt.Fprintf(s.err, "\n(idle timeout — vault locked)\n")
	s.Close()
	os.Exit(0)
}

// completer returns a liner.WordCompleter wired to live note names
// AND live current-note field keys. Each TAB triggers a fresh
// Vault.Names() / fields.Names(currentNote.Body) so adds, deletes,
// and cd context changes all reflect immediately.
func (s *Session) completer() func(line string, pos int) (string, []string, string) {
	return func(line string, pos int) (string, []string, string) {
		nameLister := func() []string {
			if s.Vault == nil {
				return nil
			}
			return s.Vault.Names()
		}
		fieldLister := func() []string {
			if s.Vault == nil || s.currentNote == "" {
				return nil
			}
			n, ok := s.Vault.Get(s.currentNote)
			if !ok {
				return nil
			}
			return fields.Names(n.Body)
		}
		return wordComplete(line, pos, nameLister, fieldLister)
	}
}

// Exec runs a single REPL command outside the interactive loop.
// Returned stop indicates whether the command would terminate the
// session (only "exit"/"quit" do); single-shot callers can ignore it.
func (s *Session) Exec(cmd string, args []string) (stop bool, err error) {
	return s.dispatch(cmd, args)
}

func (s *Session) dispatch(cmd string, args []string) (stop bool, err error) {
	switch cmd {
	case "exit", "quit", "q":
		return true, nil
	case "help", "?":
		s.help()
		return false, nil
	case "ls":
		s.ls()
		return false, nil
	case "read":
		// Backward-compatible alias for show. Always required a name
		// arg in v0.5; preserve that contract so existing scripts and
		// muscle memory keep working. New users are nudged toward
		// `show` via help.
		if len(args) != 1 {
			return false, errors.New("usage: read <name>")
		}
		return false, s.show(args)
	case "show":
		// show              → current note body (errors if no current note)
		// show <field>      → field value of current note
		// show <note>       → that note's body (whether or not current
		//                     note is set; note-name match wins over
		//                     field-name when both exist)
		return false, s.show(args)
	case "cd":
		if len(args) != 1 {
			return false, errors.New("usage: cd <note> | cd .. | cd /")
		}
		return false, s.cd(args[0])
	case "pwd":
		s.pwd()
		return false, nil
	case "fields":
		if len(args) != 0 {
			return false, errors.New("usage: fields")
		}
		return false, s.fields()
	case "note":
		if len(args) != 1 {
			return false, errors.New("usage: note <name>")
		}
		return false, s.note(args[0])
	case "rm":
		yes, rest, err := parseYesFlag(args)
		if err != nil {
			return false, err
		}
		if len(rest) != 1 {
			return false, errors.New("usage: rm [-y|--yes] <name>")
		}
		return false, s.rm(rest[0], yes)
	case "find":
		if len(args) < 1 {
			return false, errors.New("usage: find <query>")
		}
		s.find(strings.Join(args, " "))
		return false, nil
	case "copy":
		// Legacy form: requires an explicit note name. Kept verbatim
		// so scripts and existing tests keep passing.
		if len(args) != 1 {
			return false, errors.New("usage: copy <name>")
		}
		return false, s.copy(args[0])
	case "cp":
		// cp              → copy current note body
		// cp <field>      → copy current note's field value
		// cp <note>       → copy that note's body (note-name match
		//                   wins over field-name when both exist)
		return false, s.cp(args)
	case "set":
		// set <field> <value>   → update field (refused for known
		//                         secret fields — they leak into
		//                         REPL history as argv).
		// set <field>           → prompt for value: TTY echo-off
		//                         for secret fields, plain echo for
		//                         everything else.
		return false, s.set(args)
	case "unset":
		if len(args) != 1 {
			return false, errors.New("usage: unset <field>")
		}
		return false, s.unset(args[0])
	case "template":
		switch {
		case len(args) == 0:
			return false, s.templateEdit()
		case len(args) == 1 && args[0] == "show":
			s.templateShow()
			return false, nil
		case len(args) == 1 && args[0] == "reset":
			return false, s.templateReset()
		default:
			return false, errors.New("usage: template [show|reset]")
		}
	case "passphrase":
		if len(args) != 0 {
			return false, errors.New("usage: passphrase")
		}
		return false, s.passphrase()
	case "export":
		return false, s.export(args)
	case "import":
		return false, s.importVault(args)
	case "bundle":
		return false, s.bundle(args)
	case "import-bundle":
		return false, s.importBundle(args)
	default:
		return false, fmt.Errorf("unknown command: %s (try 'help')", cmd)
	}
}

func (s *Session) help() {
	fmt.Fprintln(s.out, "commands:")
	fmt.Fprintln(s.out, "  ls              list note names")
	fmt.Fprintln(s.out, "  note <name>     create or edit a note in $EDITOR")
	fmt.Fprintln(s.out, "  cd <note>       enter a note context; cd .. or cd / to leave")
	fmt.Fprintln(s.out, "  pwd             print the current note context (/ if none)")
	fmt.Fprintln(s.out, "  show [<arg>]    print body of <arg> (note name) or current note;")
	fmt.Fprintln(s.out, "                  with no arg + current note set, print current note;")
	fmt.Fprintln(s.out, "                  with <field> + current note set, print that field")
	fmt.Fprintln(s.out, "  read <name>     alias of show (kept for backward compatibility)")
	fmt.Fprintln(s.out, "  fields          list field keys parsed from the current note")
	fmt.Fprintln(s.out, "  cp [<arg>]      clipboard counterpart of show (current note / field / note)")
	fmt.Fprintln(s.out, "  copy <name>     copy a note's body to the clipboard (legacy form)")
	fmt.Fprintln(s.out, "  set <f> [<v>]   update a field; secret fields force a TTY prompt")
	fmt.Fprintln(s.out, "  unset <field>   remove a field line from the current note")
	fmt.Fprintln(s.out, "  find <query>    search note names and bodies (case-insensitive)")
	fmt.Fprintln(s.out, "  rm [-y] <name>  remove a note (asks for confirmation unless -y)")
	fmt.Fprintln(s.out, "  template        edit the new-note template in $EDITOR")
	fmt.Fprintln(s.out, "  template show   print the current template")
	fmt.Fprintln(s.out, "  template reset  restore the built-in default template")
	fmt.Fprintln(s.out, "  passphrase      rotate this vault's passphrase")
	fmt.Fprintln(s.out, "  export [-o p] [--force]")
	fmt.Fprintln(s.out, "                  print decrypted JSON to stdout (or write to a file)")
	fmt.Fprintln(s.out, "  import <json> [--mode merge|replace] [-y]")
	fmt.Fprintln(s.out, "                  pull notes from a previously exported JSON")
	fmt.Fprintln(s.out, "  bundle <name>... -o <path> [--force]")
	fmt.Fprintln(s.out, "                  encrypt selected notes into a portable .kpb file")
	fmt.Fprintln(s.out, "  import-bundle <path> [-y]")
	fmt.Fprintln(s.out, "                  decrypt a .kpb (asks for source passphrase) and merge in")
	fmt.Fprintln(s.out, "  help            show this help")
	fmt.Fprintln(s.out, "  exit            close the vault and quit")
	fmt.Fprintln(s.out)
	fmt.Fprintf(s.out, "template placeholders (expanded once on new-note create): %s\n",
		strings.Join(notefmt.SupportedPlaceholders, " "))
}

func (s *Session) ls() {
	names := s.Vault.Names()
	if len(names) == 0 {
		fmt.Fprintln(s.out, "(empty)")
		return
	}
	for _, n := range names {
		fmt.Fprintln(s.out, n)
	}
}

func (s *Session) read(name string) error {
	canon, err := store.NormalizeName(name)
	if err != nil {
		return err
	}
	n, ok := s.Vault.Get(canon)
	if !ok {
		return fmt.Errorf("note %q not found. Try 'ls'", canon)
	}
	fmt.Fprintln(s.out, n.Body)
	return nil
}

// note opens $EDITOR with a frontmatter (created/updated timestamps)
// plus either the existing body or, for a brand-new entry, a starter
// template (id/url/password/api_key/memo). On save the frontmatter is
// stripped — JSON metadata stays the source of truth — and the result
// replaces the note. An unmodified template, or a body that's only
// whitespace once stripped, leaves the vault untouched.
func (s *Session) note(name string) error {
	canon, err := store.NormalizeName(name)
	if err != nil {
		return err
	}
	var (
		bodyForEditor string
		created       time.Time
		updated       time.Time
		isNew         bool
	)
	if existing, ok := s.Vault.Get(canon); ok {
		bodyForEditor = existing.Body
		created = existing.CreatedAt
		updated = existing.UpdatedAt
	} else {
		now := time.Now().UTC()
		tmpl := s.Vault.Template
		if tmpl == "" {
			tmpl = notefmt.DefaultBody
		}
		bodyForEditor = notefmt.ApplyPlaceholders(tmpl, notefmt.Placeholders{
			Name: canon,
			Now:  now,
		})
		created = now
		updated = now
		isNew = true
	}

	rendered := notefmt.Render(created, updated, bodyForEditor)
	edited, err := editor.Edit(rendered, canon)
	if err != nil {
		return err
	}

	body := notefmt.Strip(edited)
	if strings.TrimSpace(body) == "" {
		fmt.Fprintln(s.out, "(empty content; not saved)")
		return nil
	}
	// Compare against the expanded template so a brand-new note with the
	// untouched starter is treated as "no edits". For existing notes the
	// editor body equals existing.Body when nothing changed.
	if isNew && body == bodyForEditor {
		fmt.Fprintln(s.out, "(template unchanged; not saved)")
		return nil
	}

	if _, err := s.Vault.Put(canon, body); err != nil {
		return err
	}
	return s.persist()
}

func (s *Session) rm(name string, autoYes bool) error {
	canon, err := store.NormalizeName(name)
	if err != nil {
		return err
	}
	if _, ok := s.Vault.Get(canon); !ok {
		return fmt.Errorf("note %q not found. Try 'ls'", canon)
	}
	if !autoYes {
		ok, err := s.confirm(fmt.Sprintf("remove note %q? [y/N]: ", canon))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(s.out, "cancelled")
			return nil
		}
	}
	if err := s.Vault.Delete(canon); err != nil {
		return err
	}
	if err := s.persist(); err != nil {
		return err
	}
	fmt.Fprintf(s.out, "removed %s\n", canon)
	return nil
}

// parseYesFlag pulls -y / --yes out of args (in any position) and
// returns the remaining non-flag arguments. Unknown flags surface as
// errors so typos don't silently disable confirmations.
func parseYesFlag(args []string) (yes bool, rest []string, err error) {
	rest = make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "-y", "--yes":
			yes = true
		default:
			if strings.HasPrefix(a, "-") {
				return false, nil, fmt.Errorf("unknown flag: %s", a)
			}
			rest = append(rest, a)
		}
	}
	return yes, rest, nil
}

func (s *Session) find(query string) {
	matches := s.Vault.Find(query)
	if len(matches) == 0 {
		fmt.Fprintln(s.out, "(no matches)")
		return
	}
	for _, m := range matches {
		tag := tagFor(m)
		if m.Snippet != "" {
			fmt.Fprintf(s.out, "%-32s %s  %s\n", m.Name, tag, m.Snippet)
		} else {
			fmt.Fprintf(s.out, "%-32s %s\n", m.Name, tag)
		}
	}
}

func tagFor(m store.Match) string {
	switch {
	case m.NameMatch && m.BodyMatch:
		return "(name+body)"
	case m.NameMatch:
		return "(name)"
	default:
		return "(body)"
	}
}

func (s *Session) copy(name string) error {
	canon, err := store.NormalizeName(name)
	if err != nil {
		return err
	}
	n, ok := s.Vault.Get(canon)
	if !ok {
		return fmt.Errorf("note %q not found. Try 'ls'", canon)
	}
	if err := s.clip.Copy([]byte(n.Body)); err != nil {
		return fmt.Errorf("clipboard: %w", err)
	}
	s.announceClipboard(fmt.Sprintf("copied %s", canon))
	return nil
}

// announceClipboard prints the standard "copied X via <backend>
// (auto-clears in N)" line so cp/copy share the same wording.
func (s *Session) announceClipboard(what string) {
	backend := "clipboard"
	if b := s.clip.Backend(); b != nil {
		backend = b.Name()
	}
	fmt.Fprintf(s.out, "%s via %s (auto-clears in %s)\n", what, backend, s.clip.ClearAfter())
}

// cd navigates the REPL into a note context. Resolution order:
//
//  1. ".." or "/" — clear context, return to root.
//  2. exact note-name match — set s.currentNote.
//  3. prefix-group match (any note starts with target+"/") — print
//     the candidates and leave context unchanged.
//  4. neither — error.
//
// MVP: ".." goes straight to root rather than walking up one
// directory level. The spec accepts this.
func (s *Session) cd(target string) error {
	if target == ".." || target == "/" {
		s.currentNote = ""
		return nil
	}
	canon, err := store.NormalizeName(target)
	if err != nil {
		return err
	}
	if _, ok := s.Vault.Get(canon); ok {
		s.currentNote = canon
		return nil
	}
	matches := s.prefixMatches(canon)
	if len(matches) > 0 {
		fmt.Fprintf(s.out, "%q is a group, not a note.\n\n", canon)
		fmt.Fprintln(s.out, "Matching notes:")
		for _, m := range matches {
			fmt.Fprintf(s.out, "  %s\n", m)
		}
		fmt.Fprintln(s.out)
		fmt.Fprintln(s.out, "Use:")
		fmt.Fprintf(s.out, "  cd %s\n", matches[0])
		return nil
	}
	return fmt.Errorf("note %q not found. Try 'ls'", canon)
}

// prefixMatches returns the canonical names of every note whose
// name starts with prefix+"/". Empty when no such notes exist.
// Names are returned in the same order as Vault.Names() (which is
// already sorted, so callers don't need to re-sort).
func (s *Session) prefixMatches(prefix string) []string {
	if prefix == "" {
		return nil
	}
	p := prefix + "/"
	var out []string
	for _, n := range s.Vault.Names() {
		if strings.HasPrefix(n, p) {
			out = append(out, n)
		}
	}
	return out
}

// pwd prints the current path. "/" when at root, "/<note>"
// otherwise. Mirrors the shell idiom.
func (s *Session) pwd() {
	if s.currentNote == "" {
		fmt.Fprintln(s.out, "/")
		return
	}
	fmt.Fprintf(s.out, "/%s\n", s.currentNote)
}

// show implements the unified `show` / `read` dispatch.
//
//   - 0 args  → print s.currentNote's body, error if no current note.
//   - 1 arg   → if the arg matches a note name exactly, print that
//     note's body; otherwise treat it as a field name of
//     the current note. (Note-name match wins over field-
//     name match when both exist — important when a user
//     names a note `url` and also has a `url:` field in
//     the current note: the explicit name takes priority.)
//   - more    → usage error.
func (s *Session) show(args []string) error {
	switch len(args) {
	case 0:
		if s.currentNote == "" {
			return errors.New("no current note. Use 'cd <note>' first")
		}
		n, ok := s.Vault.Get(s.currentNote)
		if !ok {
			// The current note disappeared between cd and show
			// (rm in another shell, import-replace, etc.). Reset
			// context and report.
			s.currentNote = ""
			return fmt.Errorf("current note vanished — context cleared")
		}
		fmt.Fprintln(s.out, n.Body)
		return nil
	case 1:
		canon, err := store.NormalizeName(args[0])
		// If NormalizeName rejects (e.g. "url" with weird chars),
		// fall through to field-name handling without surfacing the
		// store-layer error — the user clearly didn't mean a name.
		if err == nil {
			if n, ok := s.Vault.Get(canon); ok {
				fmt.Fprintln(s.out, n.Body)
				return nil
			}
		}
		// Treat as field name on the current note.
		if s.currentNote == "" {
			return fmt.Errorf("note %q not found. Try 'ls'", args[0])
		}
		n, ok := s.Vault.Get(s.currentNote)
		if !ok {
			s.currentNote = ""
			return fmt.Errorf("current note vanished — context cleared")
		}
		val, ok := fields.Get(n.Body, args[0])
		if !ok {
			return fmt.Errorf("field %q not found in %s", args[0], s.currentNote)
		}
		fmt.Fprintln(s.out, val)
		return nil
	default:
		return errors.New("usage: show [<note>|<field>]")
	}
}

// fields prints the field keys recovered from the current note. If
// no current note is set the user is reminded to cd first — this is
// not an error.
func (s *Session) fields() error {
	if s.currentNote == "" {
		fmt.Fprintln(s.out, "No current note. Use 'cd <note>' first.")
		return nil
	}
	n, ok := s.Vault.Get(s.currentNote)
	if !ok {
		s.currentNote = ""
		return fmt.Errorf("current note vanished — context cleared")
	}
	names := fields.Names(n.Body)
	if len(names) == 0 {
		fmt.Fprintln(s.out, "(no fields in this note)")
		return nil
	}
	for _, k := range names {
		fmt.Fprintln(s.out, k)
	}
	return nil
}

// cp is the context-aware copy. It mirrors `show`'s dispatch table
// but writes to the clipboard instead of stdout.
func (s *Session) cp(args []string) error {
	switch len(args) {
	case 0:
		if s.currentNote == "" {
			return errors.New("no current note. Use 'cd <note>' first")
		}
		n, ok := s.Vault.Get(s.currentNote)
		if !ok {
			s.currentNote = ""
			return fmt.Errorf("current note vanished — context cleared")
		}
		if err := s.clip.Copy([]byte(n.Body)); err != nil {
			return fmt.Errorf("clipboard: %w", err)
		}
		s.announceClipboard(fmt.Sprintf("copied %s", s.currentNote))
		return nil
	case 1:
		canon, err := store.NormalizeName(args[0])
		if err == nil {
			if n, ok := s.Vault.Get(canon); ok {
				if err := s.clip.Copy([]byte(n.Body)); err != nil {
					return fmt.Errorf("clipboard: %w", err)
				}
				s.announceClipboard(fmt.Sprintf("copied %s", canon))
				return nil
			}
		}
		if s.currentNote == "" {
			return fmt.Errorf("note %q not found. Try 'ls'", args[0])
		}
		n, ok := s.Vault.Get(s.currentNote)
		if !ok {
			s.currentNote = ""
			return fmt.Errorf("current note vanished — context cleared")
		}
		val, ok := fields.Get(n.Body, args[0])
		if !ok {
			return fmt.Errorf("field %q not found in %s", args[0], s.currentNote)
		}
		if err := s.clip.Copy([]byte(val)); err != nil {
			return fmt.Errorf("clipboard: %w", err)
		}
		s.announceClipboard(fmt.Sprintf("copied field %q from %s", args[0], s.currentNote))
		return nil
	default:
		return errors.New("usage: cp [<note>|<field>]")
	}
}

// set updates a field on the current note. Two argument shapes:
//
//   - set <field> <value>   plain update. Refused when <field> is a
//     well-known secret-field name to keep the
//     value out of REPL history (it lands in
//     liner's history file as argv).
//   - set <field>           prompt for the value via TTY. For secret
//     fields the read is echo-off (term.ReadPassword);
//     for non-secret fields a plain echoed line read
//     is used so the user can see what they typed.
//
// The user may type "set apikey  with  spaces" — we join the rest
// of args[1:] back together so embedded spaces are preserved in the
// stored value, matching what someone would expect from `set` in a
// shell-flavored REPL.
func (s *Session) set(args []string) error {
	if s.currentNote == "" {
		return errors.New("no current note. Use 'cd <note>' first")
	}
	if len(args) == 0 {
		return errors.New("usage: set <field> [<value>]")
	}
	field := args[0]
	rest := strings.Join(args[1:], " ")

	if fields.IsSecretField(field) && len(args) > 1 {
		return fmt.Errorf("refusing to accept a secret value as an argument (it would land in REPL history).\n  Use:  set %s\n  and supply the value at the prompt", field)
	}

	var value string
	if len(args) > 1 {
		value = rest
	} else {
		v, err := s.promptForFieldValue(field)
		if err != nil {
			return err
		}
		value = v
	}

	n, ok := s.Vault.Get(s.currentNote)
	if !ok {
		s.currentNote = ""
		return fmt.Errorf("current note vanished — context cleared")
	}
	updated := fields.Set(n.Body, field, value)
	if _, err := s.Vault.Put(s.currentNote, updated); err != nil {
		return err
	}
	if err := s.persist(); err != nil {
		return err
	}
	fmt.Fprintf(s.out, "updated field %q\n", strings.ToLower(field))
	return nil
}

// promptForFieldValue reads a field value from the user. For secret
// fields with a real TTY we go straight to term.ReadPassword so the
// value never echoes (and never lands in liner's history buffer).
// For non-TTY runs (tests, piped stdin) we fall back to the
// session's prompter so withInput() can feed the value in test
// fixtures — echo control isn't meaningful there anyway.
func (s *Session) promptForFieldValue(field string) (string, error) {
	prompt := fmt.Sprintf("New value for %s: ", field)
	if fields.IsSecretField(field) && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(s.err, prompt)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(s.err)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	if s.p != nil {
		return s.p.Prompt(prompt)
	}
	// Last-resort fallback if a caller forgot to install a
	// prompter — use the shared bufio reader so behavior matches
	// the rest of the codebase.
	return tty.ReadLine(prompt)
}

// unset removes a field from the current note. No-op (silent) when
// the field doesn't exist, matching Set's "best effort" feel and
// the spec's "remove if present" intent.
func (s *Session) unset(field string) error {
	if s.currentNote == "" {
		return errors.New("no current note. Use 'cd <note>' first")
	}
	n, ok := s.Vault.Get(s.currentNote)
	if !ok {
		s.currentNote = ""
		return fmt.Errorf("current note vanished — context cleared")
	}
	if _, ok := fields.Get(n.Body, field); !ok {
		return fmt.Errorf("field %q not found in %s", field, s.currentNote)
	}
	updated := fields.Unset(n.Body, field)
	if _, err := s.Vault.Put(s.currentNote, updated); err != nil {
		return err
	}
	if err := s.persist(); err != nil {
		return err
	}
	fmt.Fprintf(s.out, "removed field %q\n", strings.ToLower(field))
	return nil
}

// passphrase rotates the vault's passphrase. The user is already
// authenticated (we have s.Key), so we only ask for the new value.
//
// v1 vaults: vault.Rekey re-derives the payload key under a new salt;
//
//	the cached key (if any) is now stale → OnRekey hook invalidates it.
//
// v2 vaults: vault.RekeyV2 rebuilds only the passphrase wrap; the DEK
//
//	and the recovery wrap are preserved → cached DEK stays valid, no
//	invalidation needed.
//
// After rekey we reopen to refresh s.Key/s.Hdr so subsequent saves in
// this session use the new key, not the stale one.
func (s *Session) passphrase() error {
	newPass, err := tty.ReadNewPassphrase("New passphrase: ", "Repeat: ")
	if err != nil {
		return err
	}
	defer crypto.Zero(newPass)

	prevVersion := s.Hdr.Version
	switch prevVersion {
	case 1:
		plaintext, err := s.Vault.ToJSON()
		if err != nil {
			return err
		}
		defer crypto.Zero(plaintext)
		if err := vault.Rekey(s.Path, plaintext, newPass); err != nil {
			return err
		}
	case 2:
		if err := vault.RekeyV2(s.Path, s.Key, newPass); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported vault version: %d", prevVersion)
	}

	_, newKey, newHdr, err := vault.Open(s.Path, newPass)
	if err != nil {
		return fmt.Errorf("rekey wrote %s but reopen failed: %w", s.Path, err)
	}
	crypto.Zero(s.Key)
	s.Key = newKey
	s.Hdr = newHdr

	if s.opts.OnRekey != nil {
		s.opts.OnRekey(prevVersion)
	}

	fmt.Fprintln(s.out, "passphrase changed; previous .bak removed")
	return nil
}

// export prints (or writes) the decrypted vault contents as plaintext
// JSON. Default destination is stdout, with a stderr warning so users
// notice they just exposed everything. Writing to a file requires
// --force when the file already exists.
func (s *Session) export(args []string) error {
	var (
		outPath string
		force   bool
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o", "--output":
			if i+1 >= len(args) {
				return errors.New("usage: export [-o path] [--force]")
			}
			outPath = args[i+1]
			i++
		case "-f", "--force":
			force = true
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	plaintext, err := s.Vault.ToJSON()
	if err != nil {
		return err
	}
	defer crypto.Zero(plaintext)

	if outPath == "" {
		fmt.Fprintln(s.err, "warning: writing decrypted vault contents to stdout")
		if _, err := s.out.Write(plaintext); err != nil {
			return err
		}
		if !bytes.HasSuffix(plaintext, []byte("\n")) {
			fmt.Fprintln(s.out)
		}
		return nil
	}

	if _, err := os.Stat(outPath); err == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", outPath)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Fprintf(s.err, "warning: writing decrypted vault contents to %s\n", outPath)
	if err := os.WriteFile(outPath, plaintext, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(s.out, "exported %d notes to %s\n", len(s.Vault.Notes), outPath)
	return nil
}

// importVault loads a JSON vault file (typically produced by export)
// and merges or replaces the current notes. Replace mode requires
// confirmation. Merge conflicts are kept under a renamed key with a
// .conflict-YYYYMMDD[-N] suffix so nothing is lost silently.
func (s *Session) importVault(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: import <json-file> [--mode merge|replace] [-y]")
	}
	inPath := args[0]
	mode := "merge"
	yes := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--mode":
			if i+1 >= len(args) {
				return errors.New("--mode requires merge or replace")
			}
			mode = args[i+1]
			i++
		case "-y", "--yes":
			yes = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			return fmt.Errorf("unexpected argument: %s", args[i])
		}
	}
	if mode != "merge" && mode != "replace" {
		return fmt.Errorf("--mode must be merge or replace, got %q", mode)
	}

	raw, err := os.ReadFile(inPath)
	if err != nil {
		return err
	}
	in, err := store.FromJSON(raw)
	if err != nil {
		return fmt.Errorf("parse %s: %w", inPath, err)
	}

	switch mode {
	case "replace":
		if !yes {
			ok, err := s.confirm(fmt.Sprintf("replace ALL %d existing notes with %d imported notes? [y/N]: ", len(s.Vault.Notes), len(in.Notes)))
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(s.out, "cancelled")
				return nil
			}
		}
		s.Vault.Notes = in.Notes
		if in.Template != "" {
			s.Vault.Template = in.Template
		}
		fmt.Fprintf(s.out, "replaced: %d notes\n", len(in.Notes))
	case "merge":
		added, conflicts := mergeNotes(s.Vault, in)
		if in.Template != "" && s.Vault.Template == "" {
			s.Vault.Template = in.Template
		}
		fmt.Fprintf(s.out, "merged: +%d new, %d conflicts renamed (.conflict-YYYYMMDD)\n", added, conflicts)
	}
	return s.persist()
}

// maxNoteNameLen mirrors store.NormalizeName's 128-char cap. Conflict
// names must stay within this so subsequent Get/Put/Delete calls
// (which all run NormalizeName) don't reject the entry we just stored.
const maxNoteNameLen = 128

// mergeNotes copies notes from src into dst. Same-key conflicts are
// preserved under <name>.conflict-<YYYYMMDD>[-N] so the user can
// resolve them manually later.
//
// If the original name is long enough that adding the conflict suffix
// would exceed maxNoteNameLen, the prefix portion is truncated. Better
// to truncate readably than to silently produce a name Get can't look
// up — the user can still read/copy/rm it via the truncated name.
func mergeNotes(dst, src *store.DecryptedVault) (added, conflicts int) {
	today := time.Now().Format("20060102")
	for name, n := range src.Notes {
		if _, exists := dst.Notes[name]; !exists {
			dst.Notes[name] = n
			added++
			continue
		}
		base := truncatedConflictBase(name, today)
		target := base
		for i := 2; ; i++ {
			if _, exists := dst.Notes[target]; !exists {
				break
			}
			candidate := fmt.Sprintf("%s-%d", base, i)
			if len(candidate) > maxNoteNameLen {
				// Trim base further to fit the -N tail; recompute candidate.
				suffix := fmt.Sprintf("-%d", i)
				room := maxNoteNameLen - len(suffix)
				if room < 1 {
					room = 1
				}
				candidate = base[:min(len(base), room)] + suffix
			}
			target = candidate
		}
		dst.Notes[target] = n
		conflicts++
	}
	return added, conflicts
}

// truncatedConflictBase returns "<name>.conflict-<YYYYMMDD>", or — if
// that would exceed maxNoteNameLen — a name-truncated variant. The
// suffix is preserved verbatim (so users can grep for it), the prefix
// is what gets shortened.
func truncatedConflictBase(name, today string) string {
	suffix := ".conflict-" + today
	if len(name)+len(suffix) <= maxNoteNameLen {
		return name + suffix
	}
	room := maxNoteNameLen - len(suffix)
	if room < 1 {
		room = 1
	}
	return name[:room] + suffix
}

// min returns the smaller of two ints. Stdlib provides one in Go 1.21+,
// but we still target 1.18 in go.mod.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// bundle exports the named notes into a self-contained encrypted
// .kpb file. The bundle is keyed by a passphrase the user types now
// (typically the same as the vault's, but doesn't have to be — they
// could pick something else to share with a recipient).
//
// Usage: bundle <name>... -o <path> [--force]
func (s *Session) bundle(args []string) error {
	names, outPath, force, err := parseBundleArgs(args)
	if err != nil {
		return err
	}
	// Refuse to clobber an existing file unless --force, matching the
	// export command's posture so users don't lose old bundles by
	// rerunning a similar command.
	if _, statErr := os.Stat(outPath); statErr == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", outPath)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	// Canonicalize names and verify all exist before any prompting.
	canon := make([]string, 0, len(names))
	for _, raw := range names {
		c, err := store.NormalizeName(raw)
		if err != nil {
			return err
		}
		if _, ok := s.Vault.Notes[c]; !ok {
			return fmt.Errorf("note %q not found", c)
		}
		canon = append(canon, c)
	}

	pass, err := tty.ReadBundlePassphrase("Bundle passphrase (recipient will need it): ")
	if err != nil {
		return err
	}
	defer crypto.Zero(pass)
	if len(pass) == 0 {
		return errors.New("bundle passphrase cannot be empty")
	}

	notes, err := bundle.FromStoreNotes(s.Vault.Notes, canon)
	if err != nil {
		return err
	}
	blob, err := bundle.Build(notes, pass)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outPath, blob, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(s.out, "wrote %d notes to %s\n", len(canon), outPath)
	fmt.Fprintln(s.out, "note: share the passphrase via a separate channel — anyone with both can read.")
	return nil
}

// importBundle reads a .kpb file, decrypts it with the source
// passphrase, shows a preview, and (after confirmation) merges the
// notes into the current vault. Same conflict-naming rules as
// `import` (.conflict-YYYYMMDD[-N] suffix).
//
// Usage: import-bundle <path> [-y]
func (s *Session) importBundle(args []string) error {
	yes, rest, err := parseYesFlag(args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("usage: import-bundle <path> [-y]")
	}
	blob, err := os.ReadFile(rest[0])
	if err != nil {
		return err
	}

	pass, err := tty.ReadBundlePassphrase("Source bundle passphrase: ")
	if err != nil {
		return err
	}
	defer crypto.Zero(pass)

	notes, err := bundle.Open(blob, pass)
	if err != nil {
		if errors.Is(err, crypto.ErrAuthFailed) {
			return errors.New("Wrong passphrase, or the bundle is corrupted")
		}
		return err
	}

	// Preview: name + first body line, capped to keep terminal sane.
	fmt.Fprintf(s.out, "bundle contains %d notes:\n", len(notes))
	for _, name := range bundle.SortedNames(notes) {
		n := notes[name]
		fmt.Fprintf(s.out, "  %-32s %s\n", name, snippetFrom(n.Body))
	}

	if !yes {
		ok, err := s.confirm(fmt.Sprintf("import %d notes into this vault? [y/N]: ", len(notes)))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(s.out, "cancelled")
			return nil
		}
	}

	// Convert bundle notes → store notes, then reuse mergeNotes.
	incoming := store.New()
	for name, bn := range notes {
		incoming.Notes[name] = &store.Note{
			Body:      bn.Body,
			CreatedAt: bn.CreatedAt,
			UpdatedAt: bn.UpdatedAt,
		}
	}
	added, conflicts := mergeNotes(s.Vault, incoming)
	fmt.Fprintf(s.out, "imported: +%d new, %d conflicts renamed (.conflict-YYYYMMDD)\n", added, conflicts)
	return s.persist()
}

// parseBundleArgs splits "bundle" arguments into note names, the
// required -o output path, and a --force flag. Order doesn't matter;
// flags can appear before or after names.
func parseBundleArgs(args []string) (names []string, outPath string, force bool, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-o", "--output":
			if i+1 >= len(args) {
				return nil, "", false, errors.New("-o requires a path")
			}
			outPath = args[i+1]
			i++
		case "-f", "--force":
			force = true
		default:
			if strings.HasPrefix(a, "-") {
				return nil, "", false, fmt.Errorf("unknown flag: %s", a)
			}
			names = append(names, a)
		}
	}
	if len(names) == 0 {
		return nil, "", false, errors.New("bundle requires at least one note name")
	}
	if outPath == "" {
		return nil, "", false, errors.New("bundle requires -o <path>")
	}
	return names, outPath, force, nil
}

// snippetFrom returns the first non-empty line of body, trimmed and
// truncated. Used by import-bundle preview to give users a hint
// without dumping the whole secret.
func snippetFrom(body string) string {
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		if len(s) > 80 {
			s = s[:77] + "..."
		}
		return s
	}
	return ""
}

// templateEdit opens the per-vault new-note template in $EDITOR.
// Saves replace vault.Template; an empty save is rejected (use
// `template reset` to restore the default).
func (s *Session) templateEdit() error {
	current := s.Vault.Template
	if current == "" {
		current = notefmt.DefaultBody
	}
	edited, err := editor.Edit([]byte(current), "template")
	if err != nil {
		return err
	}
	newTmpl := string(edited)
	if strings.TrimSpace(newTmpl) == "" {
		return errors.New("template cannot be empty (use 'template reset' to restore the default)")
	}
	if newTmpl == s.Vault.Template {
		fmt.Fprintln(s.out, "(template unchanged; not saved)")
		return nil
	}
	if s.Vault.Template == "" && newTmpl == notefmt.DefaultBody {
		fmt.Fprintln(s.out, "(matches built-in default; staying unset)")
		return nil
	}
	s.Vault.Template = newTmpl
	if err := s.persist(); err != nil {
		return err
	}
	fmt.Fprintln(s.out, "template saved")
	return nil
}

func (s *Session) templateShow() {
	current := s.Vault.Template
	source := "vault"
	if current == "" {
		current = notefmt.DefaultBody
		source = "built-in default"
	}
	fmt.Fprintf(s.out, "# template (%s)\n", source)
	fmt.Fprint(s.out, current)
	if !strings.HasSuffix(current, "\n") {
		fmt.Fprintln(s.out)
	}
	fmt.Fprintf(s.out, "# placeholders: %s\n", strings.Join(notefmt.SupportedPlaceholders, " "))
}

func (s *Session) templateReset() error {
	if s.Vault.Template == "" {
		fmt.Fprintln(s.out, "(already using built-in default)")
		return nil
	}
	s.Vault.Template = ""
	if err := s.persist(); err != nil {
		return err
	}
	fmt.Fprintln(s.out, "template reset to built-in default")
	return nil
}

// confirm asks via the active prompter and returns true only on y/yes
// (case-insensitive). EOF / Ctrl-C default to false so an interrupted
// prompt is treated as "cancel".
func (s *Session) confirm(prompt string) (bool, error) {
	line, err := s.p.Prompt(prompt)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, errAbort) {
			return false, nil
		}
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func (s *Session) persist() error {
	plaintext, err := s.Vault.ToJSON()
	if err != nil {
		return err
	}
	defer crypto.Zero(plaintext)
	return vault.Save(s.Path, plaintext, s.Key, s.Hdr)
}
