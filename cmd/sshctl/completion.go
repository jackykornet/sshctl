package main

import (
	"embed"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/config"
)

// completeCommand is the hidden command the completion scripts run as
// sshctl __complete <cword> <word>..., with the words after sshctl and the
// index of the one being completed; it prints the candidates for that word,
// one per line. A cword past the last word completes a new, empty word, so a
// shell that drops an empty argument still works. It is not in commands, so
// help and completion do not show it.
const completeCommand = "__complete"

// completionShells are the shells sshctl completion prints a script for,
// by the name it takes, with the file in completion/ that holds the script.
var completionShells = []struct{ name, file string }{
	{"bash", "sshctl.bash"},
	{"zsh", "sshctl.zsh"},
	{"fish", "sshctl.fish"},
	{"powershell", "sshctl.ps1"},
}

//go:embed completion
var completionScripts embed.FS

// completion prints the completion script for a shell.
func (a *app) completion(in input) error {
	if len(in.args) != 1 {
		return errUsage
	}
	for _, s := range completionShells {
		if s.name == in.args[0] {
			script, err := completionScripts.ReadFile("completion/" + s.file)
			if err != nil {
				return err
			}
			_, err = a.out.Write(script)
			return err
		}
	}
	return errUsage
}

// complete is the hidden completion command (see completeCommand). It
// prints nothing, rather than an error, for anything it cannot complete,
// so the shell falls back to file names.
func (a *app) complete(args []string) error {
	if len(args) == 0 {
		return nil
	}
	cword, err := strconv.Atoi(args[0])
	words := args[1:]
	if err != nil || cword < 0 || cword > len(words) {
		return nil
	}
	cur := ""
	if cword < len(words) {
		cur = words[cword]
	}
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return nil
	}
	for _, c := range candidates(snap, words[:cword], cur) {
		if strings.HasPrefix(c, cur) {
			fmt.Fprintln(a.out, c)
		}
	}
	return nil
}

// candidates returns what may stand for cur after words, the words after
// sshctl before it, read the way lookup reads them: subcommands and
// options from the commands table, and for an argument or option value
// what its placeholder names (see placeholderValues).
func candidates(snap catalog.Snapshot, words []string, cur string) []string {
	c := (*command)(nil)
	if len(words) > 0 {
		if c = findCommand(words[0]); c != nil {
			words = words[1:]
		}
	}
	var operands []string
	opts := map[string]string{}
	for i := 0; i < len(words); i++ {
		w := words[i]
		if w == "--" {
			// After --, a remote command or operands only.
			return nil
		}
		if !strings.HasPrefix(w, "-") || w == "-" {
			operands = append(operands, w)
			continue
		}
		var o *option
		value, hasValue := "", false
		if long, ok := strings.CutPrefix(w, "--"); ok {
			long, value, hasValue = strings.Cut(long, "=")
			o = findOption(long, 0)
		} else if len(w) == 2 {
			o = findOption("", w[1])
		}
		if o == nil || !o.value || hasValue {
			if o != nil {
				opts[o.name] = value
			}
			continue
		}
		if i+1 == len(words) {
			return optionValues(snap, o.name, opts)
		}
		i++
		opts[o.name] = words[i]
	}
	if long, ok := strings.CutPrefix(cur, "--"); ok {
		if name, _, ok := strings.Cut(long, "="); ok {
			var out []string
			for _, v := range optionValues(snap, name, opts) {
				out = append(out, "--"+name+"="+v)
			}
			return out
		}
	}
	if strings.HasPrefix(cur, "-") {
		return optionNames(c, operands)
	}
	if c == nil {
		if _, named := opts["connect"]; named || len(operands) > 0 {
			return nil
		}
		return append(slices.Clone(commandNames), aliases(snap)...)
	}
	return c.operandValues(snap, operands, opts)
}

// operandValues returns what may stand for the operand after operands, the
// ones given to c so far.
func (c *command) operandValues(snap catalog.Snapshot, operands []string, opts map[string]string) []string {
	if len(operands) == 0 {
		var out []string
		for _, f := range c.forms {
			if f.sub != "" {
				out = append(out, f.sub)
			} else if p := positionals(f.args); len(p) > 0 {
				out = append(out, placeholderValues(snap, p[0], opts)...)
			}
		}
		return out
	}
	var f *form
	n := len(operands)
	for i := range c.forms {
		if c.forms[i].sub != "" && c.forms[i].sub == operands[0] {
			f, n = &c.forms[i], n-1
		}
	}
	if f == nil {
		f = c.form(input{})
	}
	if f == nil {
		return nil
	}
	if p := positionals(f.args); n < len(p) {
		return placeholderValues(snap, p[n], opts)
	}
	return nil
}

// optionNames returns the options c takes as --name: those of the form
// operands call, or of every form while no subcommand is given. With no
// command, they are connect's and --version.
func optionNames(c *command, operands []string) []string {
	if c == nil {
		return []string{"--connect", "--help", "--version"}
	}
	var forms []*form
	for i := range c.forms {
		if f := &c.forms[i]; len(operands) == 0 || f.sub == "" || f.sub == operands[0] {
			forms = append(forms, f)
		}
	}
	var out []string
	for _, o := range options {
		if slices.ContainsFunc(forms, func(f *form) bool { return c.takes(f, o.name) }) {
			out = append(out, "--"+o.name)
		}
	}
	return out
}

// optionValues returns what may be given as the option name's value, with
// opts, the options given so far, for a Group's Space.
func optionValues(snap catalog.Snapshot, name string, opts map[string]string) []string {
	switch name {
	case "connect":
		return aliases(snap)
	case fieldSpace:
		return placeholderValues(snap, "<space>", opts)
	case fieldGroup:
		return placeholderValues(snap, "<group>", opts)
	}
	return nil
}

// placeholderValues returns what may stand for a placeholder in a form's
// arguments: Connection aliases for <alias>, Space names for <space>, Group
// names for <group> (in the Space --space names, if given), the
// Experimental features for <feature>, and the words of a literal such as
// enable|disable. Anything else, such as a path, has none.
func placeholderValues(snap catalog.Snapshot, p string, opts map[string]string) []string {
	switch p {
	case "<alias>":
		return aliases(snap)
	case "<space>":
		var out []string
		for _, s := range snap.Spaces {
			out = append(out, s.Name)
		}
		return out
	case "<group>":
		if arg, ok := opts[fieldSpace]; ok {
			if ref, err := snap.ResolveSpace(arg); err == nil {
				return snap.GroupNames(ref)
			}
		}
		var out []string
		for _, g := range snap.Groups {
			if !slices.Contains(out, g.Name) {
				out = append(out, g.Name)
			}
		}
		return out
	case "<feature>":
		return slices.Clone(config.Features)
	}
	if strings.ContainsAny(p, "<>") {
		return nil
	}
	return strings.Split(p, "|")
}

// aliases returns every Connection's alias, as sshctl list shows them.
func aliases(snap catalog.Snapshot) []string {
	out := make([]string, len(snap.Entries))
	for i, e := range snap.Entries {
		out[i] = e.Alias
	}
	return out
}

// positionals returns the operands a form's arguments name, in order, one
// placeholder or literal each: "<alias> [--<field> <value>]..." names
// <alias>, and "[<url|none> | --unset]" names <url|none>. Options, in
// brackets or not, name none.
func positionals(args string) []string {
	var out []string
	for args = strings.TrimSpace(args); args != ""; args = strings.TrimSpace(args) {
		var item string
		if args[0] == '[' {
			end, depth := len(args), 0
			for i, r := range args {
				if r == '[' {
					depth++
				} else if r == ']' {
					if depth--; depth == 0 {
						end = i
						break
					}
				}
			}
			item = args[1:min(end, len(args))]
			args = strings.TrimPrefix(args[min(end+1, len(args)):], "...")
		} else {
			item, args, _ = strings.Cut(args, " ")
		}
		if strings.HasPrefix(item, "-") {
			continue
		}
		first, _, _ := strings.Cut(item, " ")
		out = append(out, first)
	}
	return out
}
