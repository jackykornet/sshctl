package main

import (
	"fmt"
	"strings"
)

// option is a command-line option (ADR-0011): its long name, its short form
// (0 for none) and whether it takes a value. Options follow the GNU
// convention on every OS: --name, --name value or --name=value, the short
// form alone as -x or -x value, anywhere after the command name, and --
// ends them.
type option struct {
	name  string
	short byte
	value bool
}

// options is every option sshctl knows. A form names the ones it takes in
// form.opts; -h (--help) works everywhere.
var options = []option{
	{"connect", 'c', true},
	{"space", 's', true},
	{"group", 'g', true},
	{"yes", 'y', false},
	{"help", 'h', false},
	{"unset", 0, false},
	{"claude", 0, false},
	{"version", 0, false},
	{fieldAlias, 0, true},
	{fieldHost, 0, true},
	{fieldUser, 0, true},
	{fieldPort, 0, true},
	{fieldIdentity, 0, true},
	{fieldDescription, 0, true},
}

// findOption returns the option a long name or short form names, or nil.
func findOption(long string, short byte) *option {
	for i := range options {
		if o := &options[i]; long != "" && o.name == long || short != 0 && o.short == short {
			return o
		}
	}
	return nil
}

// input is a call's arguments, parsed: its operands and options.
type input struct {
	// args are the operands, in order; dashed counts those given after --.
	args   []string
	dashed int
	// opts holds the options given, by long name, with "" for one that
	// takes no value.
	opts map[string]string
}

// flag reports whether the option name was given.
func (in input) flag(name string) bool {
	_, ok := in.opts[name]
	return ok
}

// value returns the value of the option name and whether it was given.
func (in input) value(name string) (string, bool) {
	v, ok := in.opts[name]
	return v, ok
}

// beforeDash returns the operands given before --.
func (in input) beforeDash() []string { return in.args[:len(in.args)-in.dashed] }

// parseArgs parses args, the words after the command name, into operands
// and options. It refuses an unknown option, a missing value, a value for an
// option that takes none, and an option given twice.
func parseArgs(args []string) (input, error) {
	in := input{opts: map[string]string{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest := args[i+1:]
			in.args = append(in.args, rest...)
			in.dashed = len(rest)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			in.args = append(in.args, arg)
			continue
		}
		var o *option
		value, hasValue := "", false
		if long, ok := strings.CutPrefix(arg, "--"); ok {
			long, value, hasValue = strings.Cut(long, "=")
			o = findOption(long, 0)
		} else if len(arg) == 2 {
			o = findOption("", arg[1])
		}
		if o == nil {
			return in, fmt.Errorf("unknown option %s", arg)
		}
		name := "--" + o.name
		if _, dup := in.opts[o.name]; dup {
			return in, fmt.Errorf("%s given twice", name)
		}
		switch {
		case !o.value && hasValue:
			return in, fmt.Errorf("%s takes no value", name)
		case o.value && !hasValue:
			if i+1 == len(args) {
				return in, fmt.Errorf("%s needs a value", name)
			}
			i++
			value = args[i]
		}
		in.opts[o.name] = value
	}
	return in, nil
}
