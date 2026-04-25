package repl

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/r2un/kpot/internal/crypto"
	"github.com/r2un/kpot/internal/editor"
	"github.com/r2un/kpot/internal/store"
	"github.com/r2un/kpot/internal/tty"
	"github.com/r2un/kpot/internal/vault"
)

type Session struct {
	Path  string
	Vault *store.DecryptedVault
	Key   []byte
	Hdr   *vault.Header

	in  *bufio.Reader
	out io.Writer
	err io.Writer
}

func NewSession(path string, v *store.DecryptedVault, key []byte, hdr *vault.Header) *Session {
	return &Session{
		Path:  path,
		Vault: v,
		Key:   key,
		Hdr:   hdr,
		in:    tty.SharedStdin(),
		out:   os.Stdout,
		err:   os.Stderr,
	}
}

func (s *Session) Close() {
	crypto.Zero(s.Key)
	s.Key = nil
	s.Vault = nil
}

func (s *Session) prompt() string {
	base := strings.TrimSuffix(filepath.Base(s.Path), filepath.Ext(s.Path))
	return fmt.Sprintf("kpot:%s> ", base)
}

func (s *Session) Run() error {
	fmt.Fprintf(s.out, "Opened %s (%d notes)\n", s.Path, len(s.Vault.Notes))
	fmt.Fprintln(s.out, "Type 'help' for commands, 'exit' to quit.")
	for {
		fmt.Fprint(s.out, s.prompt())
		line, err := s.in.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(s.out)
				return nil
			}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
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
		if len(args) != 1 {
			return false, errors.New("usage: read <name>")
		}
		return false, s.read(args[0])
	case "note":
		if len(args) != 1 {
			return false, errors.New("usage: note <name>")
		}
		return false, s.note(args[0])
	default:
		return false, fmt.Errorf("unknown command: %s (try 'help')", cmd)
	}
}

func (s *Session) help() {
	fmt.Fprintln(s.out, "commands:")
	fmt.Fprintln(s.out, "  ls              list note names")
	fmt.Fprintln(s.out, "  note <name>     create or edit a note in $EDITOR")
	fmt.Fprintln(s.out, "  read <name>     print a note's body to stdout")
	fmt.Fprintln(s.out, "  help            show this help")
	fmt.Fprintln(s.out, "  exit            close the vault and quit")
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

func (s *Session) note(name string) error {
	canon, err := store.NormalizeName(name)
	if err != nil {
		return err
	}
	var initial []byte
	if existing, ok := s.Vault.Get(canon); ok {
		initial = []byte(existing.Body)
	}
	body, err := editor.Edit(initial, canon)
	if err != nil {
		return err
	}
	bodyStr := string(body)
	if strings.TrimSpace(bodyStr) == "" {
		fmt.Fprintln(s.out, "(empty content; not saved)")
		return nil
	}
	if _, err := s.Vault.Put(canon, bodyStr); err != nil {
		return err
	}
	return s.persist()
}

func (s *Session) persist() error {
	plaintext, err := s.Vault.ToJSON()
	if err != nil {
		return err
	}
	defer crypto.Zero(plaintext)
	return vault.Save(s.Path, plaintext, s.Key, s.Hdr)
}
