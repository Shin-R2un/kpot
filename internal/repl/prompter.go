package repl

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/peterh/liner"
)

// prompter abstracts "print a prompt, read a line of input back".
// linerPrompter (TTY) gives us TAB completion + history; bufioPrompter
// (non-TTY/tests) preserves the simple line-at-a-time behavior tests rely on.
//
// Both must be safe to call sequentially with confirm() prompts mixed in.
type prompter interface {
	Prompt(p string) (string, error)
	AddHistory(line string)
	Close() error
}

// errAbort is returned by linerPrompter.Prompt when the user hits Ctrl-C.
// REPL Run loop interprets this as "discard current line, keep going".
var errAbort = errors.New("aborted")

// bufioPrompter is the test/non-interactive backend. The prompt is
// written to out (so output ordering matches what a user would see),
// and a single line is read from in. EOF with content returns the
// content; EOF without content returns io.EOF.
type bufioPrompter struct {
	in  *bufio.Reader
	out io.Writer
}

func newBufioPrompter(in *bufio.Reader, out io.Writer) *bufioPrompter {
	return &bufioPrompter{in: in, out: out}
}

func (b *bufioPrompter) Prompt(p string) (string, error) {
	fmt.Fprint(b.out, p)
	line, err := b.in.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			if line == "" {
				return "", io.EOF
			}
			return strings.TrimRight(line, "\r\n"), nil
		}
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (b *bufioPrompter) AddHistory(string) {}
func (b *bufioPrompter) Close() error      { return nil }

// linerPrompter wraps peterh/liner to give us TAB completion, line
// editing, and an in-memory history. Only use when stdin is a real TTY.
type linerPrompter struct {
	l *liner.State
}

func newLinerPrompter(complete liner.WordCompleter) *linerPrompter {
	l := liner.NewLiner()
	l.SetCtrlCAborts(true)
	l.SetTabCompletionStyle(liner.TabPrints)
	if complete != nil {
		l.SetWordCompleter(complete)
	}
	return &linerPrompter{l: l}
}

func (lp *linerPrompter) Prompt(p string) (string, error) {
	line, err := lp.l.Prompt(p)
	if err != nil {
		if errors.Is(err, liner.ErrPromptAborted) {
			return "", errAbort
		}
		return "", err
	}
	return line, nil
}

func (lp *linerPrompter) AddHistory(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	lp.l.AppendHistory(line)
}

func (lp *linerPrompter) Close() error {
	if lp.l == nil {
		return nil
	}
	return lp.l.Close()
}
