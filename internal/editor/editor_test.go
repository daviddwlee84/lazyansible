package editor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCommandPreservesQuotedArgumentsAndLiteralShellText(t *testing.T) {
	tests := []struct {
		name, value string
		args        []string
	}{
		{"plain", "nvim", []string{"nvim"}},
		{"arguments", `code --wait --goto`, []string{"code", "--wait", "--goto"}},
		{"quoted executable", `'/Applications/My Editor/editor' --wait`, []string{"/Applications/My Editor/editor", "--wait"}},
		{"mixed quotes", `nvim '+set number' "+set tabstop=4"`, []string{"nvim", "+set number", "+set tabstop=4"}},
		{"escaped space", `/tmp/my\ editor --flag=some\ value`, []string{"/tmp/my editor", "--flag=some value"}},
		{"empty argument", `nvim '' ""`, []string{"nvim", "", ""}},
		{"literal shell", `nvim '$HOME' '$(touch marker)' ';' '>' '*.yml'`, []string{"nvim", "$HOME", "$(touch marker)", ";", ">", "*.yml"}},
		{"unicode", `nvim '+set titlestring=專案 👩🏽‍💻'`, []string{"nvim", "+set titlestring=專案 👩🏽‍💻"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := "/tmp/a file.yml"
			command, err := Command(test.value, file)
			if err != nil {
				t.Fatal(err)
			}
			want := append(test.args, file)
			if !reflect.DeepEqual(command.Args, want) {
				t.Fatalf("Args = %#v, want %#v", command.Args, want)
			}
		})
	}
}

func TestCommandRejectsMalformedEditorWithoutHandoff(t *testing.T) {
	for _, value := range []string{"", "  ", `'' --wait`, `nvim 'unfinished`, `nvim "unfinished`, "nvim \\"} {
		t.Run(value, func(t *testing.T) {
			if _, err := Command(value, "file.yml"); err == nil {
				t.Fatal("malformed command accepted")
			}
		})
	}
	t.Setenv("VISUAL", `nvim 'unfinished`)
	msg := Open("file.yml")().(DoneMsg)
	if msg.Err == nil || msg.Path != "file.yml" {
		t.Fatalf("error message = %+v", msg)
	}
}

func TestFindEditorHonorsVisualBeforeEditor(t *testing.T) {
	t.Setenv("VISUAL", `code --wait`)
	t.Setenv("EDITOR", `nvim -u 'test init.lua'`)
	if got := findEditor(); got != `code --wait` {
		t.Fatalf("VISUAL = %q", got)
	}
	t.Setenv("VISUAL", "")
	if got := findEditor(); got != `nvim -u 'test init.lua'` {
		t.Fatalf("EDITOR = %q", got)
	}
}

func TestEditorArgumentsAreExecutedWithoutShellExpansion(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	// printf receives the suspicious text as argv; no shell interprets it.
	command, err := Command(`/usr/bin/printf '%s'`, "$(touch "+marker+")")
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "$(touch ") {
		t.Fatalf("literal argument changed: %q", output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("unexpected shell side effect: %v", err)
	}
}
