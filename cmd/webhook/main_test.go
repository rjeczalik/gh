package main

import (
	"reflect"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	cases := [...]struct {
		command string
		cmd     string
		args    []string
	}{
		// i=0
		{
			`"C:/Program Files/Git/bin/git.exe" checkout HEAD~1 'a file' another\ file`,
			`C:/Program Files/Git/bin/git.exe`,
			[]string{`checkout`, `HEAD~1`, `a file`, `another\ file`},
		},
		// i=1
		{
			`git.exe`,
			`git.exe`,
			nil,
		},
		// i=2
		{
			`"git.exe"`,
			`git.exe`,
			nil,
		},
	}
	for i, cas := range cases {
		cmd, args := splitCommand(cas.command)
		if cmd != cas.cmd {
			t.Errorf("want cmd=%v; got %v (i=%d)", cas.cmd, cmd, i)
			continue
		}
		if !reflect.DeepEqual(args, cas.args) {
			t.Errorf("want args=%v; got %v (i=%d)", cas.args, args, i)
			continue
		}
	}
}
