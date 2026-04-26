package repl

import (
	"reflect"
	"sort"
	"testing"
)

func names(ns ...string) nameLister { return func() []string { return ns } }

func TestCompleteEmptyLineListsAllCommands(t *testing.T) {
	head, comp, tail := wordComplete("", 0, names())
	if head != "" || tail != "" {
		t.Fatalf("head=%q tail=%q", head, tail)
	}
	sort.Strings(comp)
	want := append([]string(nil), commandNames...)
	for i, c := range want {
		want[i] = c + " "
	}
	sort.Strings(want)
	if !reflect.DeepEqual(comp, want) {
		t.Fatalf("completions = %v, want %v", comp, want)
	}
}

func TestCompleteCommandPrefix(t *testing.T) {
	head, comp, tail := wordComplete("co", 2, names())
	if head != "" || tail != "" {
		t.Fatalf("head=%q tail=%q", head, tail)
	}
	if len(comp) != 1 || comp[0] != "copy " {
		t.Fatalf("comp = %v, want [copy ]", comp)
	}
}

func TestCompleteCommandPrefixMultiple(t *testing.T) {
	_, comp, _ := wordComplete("r", 1, names())
	sort.Strings(comp)
	want := []string{"read ", "rm "}
	if !reflect.DeepEqual(comp, want) {
		t.Fatalf("comp = %v, want %v", comp, want)
	}
}

func TestCompleteNoteName(t *testing.T) {
	line := "copy ai/o"
	head, comp, tail := wordComplete(line, len(line), names("ai/openai", "ai/anthropic", "server/fw0"))
	if head != "copy " {
		t.Fatalf("head = %q, want %q", head, "copy ")
	}
	if tail != "" {
		t.Fatalf("tail = %q", tail)
	}
	if len(comp) != 1 || comp[0] != "ai/openai" {
		t.Fatalf("comp = %v, want [ai/openai]", comp)
	}
}

func TestCompleteNoteNameAllOnEmptyArg(t *testing.T) {
	line := "rm "
	head, comp, tail := wordComplete(line, len(line), names("a", "b", "c"))
	if head != "rm " || tail != "" {
		t.Fatalf("head=%q tail=%q", head, tail)
	}
	sort.Strings(comp)
	if !reflect.DeepEqual(comp, []string{"a", "b", "c"}) {
		t.Fatalf("comp = %v", comp)
	}
}

func TestCompleteNonNoteCommandReturnsNothing(t *testing.T) {
	// `find` is not in the noteNameCommands set, so its argument doesn't
	// get note-name completion (it's a free-text query).
	_, comp, _ := wordComplete("find foo", 8, names("foo", "foobar"))
	if comp != nil {
		t.Fatalf("expected no completions for find <query>, got %v", comp)
	}
}

func TestCompleteCursorMidLine(t *testing.T) {
	// Cursor in the middle of "copy openai" right after "co"; we should
	// still be completing the first word, with " openai" preserved as tail.
	line := "co openai"
	head, comp, tail := wordComplete(line, 2, names("openai"))
	if head != "" {
		t.Fatalf("head = %q", head)
	}
	if tail != " openai" {
		t.Fatalf("tail = %q", tail)
	}
	if len(comp) != 1 || comp[0] != "copy " {
		t.Fatalf("comp = %v, want [copy ]", comp)
	}
}

func TestCompleteEmptyNamesNoCrash(t *testing.T) {
	_, comp, _ := wordComplete("read ", 5, nil)
	if comp != nil {
		t.Fatalf("expected nil completions when name lister is nil, got %v", comp)
	}
}

func TestCompleteTemplateSubcommands(t *testing.T) {
	head, comp, _ := wordComplete("template ", 9, names())
	if head != "template " {
		t.Fatalf("head = %q", head)
	}
	sort.Strings(comp)
	want := []string{"reset", "show"}
	if !reflect.DeepEqual(comp, want) {
		t.Fatalf("comp = %v, want %v", comp, want)
	}
}

func TestCompleteTemplateSubcommandPrefix(t *testing.T) {
	_, comp, _ := wordComplete("template re", 11, names())
	if len(comp) != 1 || comp[0] != "reset" {
		t.Fatalf("comp = %v, want [reset]", comp)
	}
}

func TestCompleteBundleAcceptsMultipleNoteArgs(t *testing.T) {
	// `bundle ai/o<TAB>` should still complete note names because
	// bundle is in noteNameCommands.
	line := "bundle ai/o"
	head, comp, _ := wordComplete(line, len(line), names("ai/openai", "ai/anthropic"))
	if head != "bundle " {
		t.Fatalf("head = %q, want 'bundle '", head)
	}
	if len(comp) != 1 || comp[0] != "ai/openai" {
		t.Fatalf("comp = %v, want [ai/openai]", comp)
	}
}

func TestCompleteImportBundleIsKnownCommand(t *testing.T) {
	// Ensure import-bundle shows up when typing 'import' prefix.
	_, comp, _ := wordComplete("import", 6, names())
	hasImport, hasBundle := false, false
	for _, c := range comp {
		if c == "import " {
			hasImport = true
		}
		if c == "import-bundle " {
			hasBundle = true
		}
	}
	if !hasImport || !hasBundle {
		t.Errorf("expected both 'import' and 'import-bundle' in completions, got %v", comp)
	}
}

func TestCompleteTemplateNoCompletionPastSubcommand(t *testing.T) {
	_, comp, _ := wordComplete("template show extra", 19, names("a", "b"))
	if comp != nil {
		t.Fatalf("expected no completions past template subcommand, got %v", comp)
	}
}
