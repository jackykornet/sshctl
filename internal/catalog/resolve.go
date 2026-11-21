package catalog

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The lookups of a Group or Space the user typed, by name or number, which
// the CLI and the Workspace share. A Group or Space name is never only
// digits, so all digits is always a number.

var (
	// ErrUnknownGroupNumber is returned for a Group number no Group of the
	// Space has.
	ErrUnknownGroupNumber = errors.New("no group with that number")
	// ErrGroupInSeveralSpaces is returned by FindGroup when what was typed
	// could be a Group of more than one Space, so a Space must be picked.
	ErrGroupInSeveralSpaces = errors.New("several spaces could have that group")
)

// lookupError is a failed lookup: a message saying what was looked up and
// what exists, which errors.Is matches against kind.
type lookupError struct {
	msg  string
	kind error
}

func (e *lookupError) Error() string { return e.msg }
func (e *lookupError) Unwrap() error { return e.kind }

// isNumber reports whether input is all digits, so it is a number rather
// than a name.
func isNumber(input string) bool {
	return input != "" && strings.Trim(input, "0123456789") == ""
}

// ResolveSpace returns how Groups and Connections refer to the Space input
// names ("" for the default Space): its Space number or its name. It
// refuses, with ErrUnknownSpace naming the Spaces there are, a number or
// name no Space has.
func (s Snapshot) ResolveSpace(input string) (string, error) {
	if isNumber(input) {
		if n, err := strconv.Atoi(input); err == nil {
			if sp, ok := s.SpaceNumbered(n); ok {
				ref, _ := s.SpaceRef(sp.Name)
				return ref, nil
			}
		}
		return "", &lookupError{fmt.Sprintf("no Space %s; %s", input, s.spacesList()), ErrUnknownSpace}
	}
	if input != "" {
		if ref, ok := s.SpaceRef(input); ok {
			return ref, nil
		}
	}
	return "", &lookupError{fmt.Sprintf("no Space %q; %s", input, s.spacesList()), ErrUnknownSpace}
}

// spacesList says which Spaces there are, by number and name.
func (s Snapshot) spacesList() string {
	var names []string
	for _, sp := range s.Spaces {
		if sp.Number > 0 {
			names = append(names, fmt.Sprintf("%d %s", sp.Number, sp.Name))
		} else {
			names = append(names, sp.Name)
		}
	}
	return "the Spaces are " + strings.Join(names, ", ")
}

// ResolveGroup returns the name of the Group input names in the Space ref
// ("" for the default Space): a Group number, its position in the Space
// counting from 1, or a Group name. "0" names ungrouped, returned as "".
// It refuses a number no Group has (ErrUnknownGroupNumber) and a name no
// Group of the Space has (ErrUnknownGroup), naming the Space's Groups.
func (s Snapshot) ResolveGroup(ref, input string) (string, error) {
	names := s.GroupNames(ref)
	if isNumber(input) {
		n, err := strconv.Atoi(input)
		switch {
		case err == nil && n == 0:
			return "", nil
		case err == nil && n <= len(names):
			return names[n-1], nil
		}
		return "", &lookupError{fmt.Sprintf("no group number %s in Space %s; %s", input, s.SpaceName(ref), groupsList(names)), ErrUnknownGroupNumber}
	}
	if s.HasGroup(ref, input) {
		return input, nil
	}
	return "", &lookupError{fmt.Sprintf("no group %q in Space %s; %s", input, s.SpaceName(ref), groupsList(names)), ErrUnknownGroup}
}

// groupsList says which Groups a Space has, by number and name.
func groupsList(names []string) string {
	if len(names) == 0 {
		return "it has no groups"
	}
	numbered := make([]string, len(names))
	for i, n := range names {
		numbered[i] = fmt.Sprintf("%d %s", i+1, n)
	}
	return "its groups are " + strings.Join(numbered, ", ")
}

// FindGroup returns the Space, as Groups refer to it, and the name of the
// Group input names when no Space is given. With one Space it is
// ResolveGroup in that Space. With several, a name is looked up in every
// Space and used when exactly one has it; a Group number (or 0, ungrouped)
// counts within a Space, so it is refused. Either refusal is
// ErrGroupInSeveralSpaces, naming the Spaces; a name no Space has is
// ErrUnknownGroup, naming every Space's Groups.
func (s Snapshot) FindGroup(input string) (ref, name string, err error) {
	if len(s.Spaces) == 1 {
		ref, _ := s.SpaceRef(s.Spaces[0].Name)
		name, err := s.ResolveGroup(ref, input)
		return ref, name, err
	}
	if isNumber(input) {
		return "", "", &lookupError{fmt.Sprintf("group number %s counts within a Space, and there are %d Spaces", input, len(s.Spaces)), ErrGroupInSeveralSpaces}
	}
	var found, in, all []string
	for _, sp := range s.Spaces {
		ref, _ := s.SpaceRef(sp.Name)
		if s.HasGroup(ref, input) {
			found = append(found, ref)
			in = append(in, sp.Name)
		}
		if names := s.GroupNames(ref); len(names) > 0 {
			all = append(all, sp.Name+": "+strings.Join(names, ", "))
		}
	}
	switch {
	case len(found) == 1:
		return found[0], input, nil
	case len(found) > 1:
		return "", "", &lookupError{fmt.Sprintf("group %q is in Spaces %s", input, strings.Join(in, ", ")), ErrGroupInSeveralSpaces}
	case len(all) == 0:
		return "", "", &lookupError{fmt.Sprintf("no group %q in any Space; there are no groups", input), ErrUnknownGroup}
	}
	return "", "", &lookupError{fmt.Sprintf("no group %q in any Space; the groups are %s", input, strings.Join(all, "; ")), ErrUnknownGroup}
}
