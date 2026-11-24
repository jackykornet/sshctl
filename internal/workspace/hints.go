package workspace

import "strings"

// A hintLine tells what keys do, as "key action · key action", most
// important first. Every hint line in the Workspace is built by hintsFor
// from the keys bound where it shows (ADR-0007), so it cannot name a key
// that does nothing there.
type hintLine struct {
	prefixKey string
	items     []hintItem
}

// A hintItem is one action of a hint line and the keys that run it.
type hintItem struct {
	// lead is written before the item's keys: the prefix key, such as
	// "^B ", for keys after the prefix, or "".
	lead string
	keys []string
	name string
}

// hintsFor is the hint line for keys, in the order given, each named as
// table binds it. Keys given one after another with the same name share an
// item, as in "Shift+↑/↓ move". A key not in table keeps an empty name, which
// TestHintsNameOnlyBoundKeys catches.
func hintsFor(table []boundKey, keys ...string) hintLine {
	var l hintLine
	for _, k := range keys {
		name := ""
		if bk, ok := findKey(table, k); ok {
			name = bk.name
		}
		if n := len(l.items); n > 0 && l.items[n-1].name == name {
			l.items[n-1].keys = append(l.items[n-1].keys, k)
			continue
		}
		l.items = append(l.items, hintItem{keys: []string{k}, name: name})
	}
	return l
}

// hint is the hint line for keys bound in ctx, written after the prefix
// key in inPrefix.
func (m *Model) hint(ctx keyContext, keys ...string) hintLine {
	l := hintsFor(m.contextKeys(ctx), keys...)
	l.prefixKey = m.opts.PrefixKey
	if ctx == inPrefix {
		for i := range l.items {
			l.items[i].lead = keyHint(m.opts.PrefixKey) + " "
		}
	}
	return l
}

// first is l with its first n items.
func (l hintLine) first(n int) hintLine {
	l.items = l.items[:min(n, len(l.items)):min(n, len(l.items))]
	return l
}

// and is l followed by o's items, so one line can join keys of two
// contexts, such as the list's and the prefix's.
func (l hintLine) and(o hintLine) hintLine {
	l.items = append(append([]hintItem{}, l.items...), o.items...)
	return l
}

// contextKeys is every key bound in ctx, with the prefix key itself under
// its own name.
func (m *Model) contextKeys(ctx keyContext) []boundKey {
	var out []boundKey
	for _, b := range bindings {
		for _, bk := range b.keys[ctx] {
			if bk.key == prefixSelf {
				bk.key = m.opts.PrefixKey
			}
			out = append(out, bk)
		}
	}
	return out
}

func (l hintLine) String() string {
	parts := make([]string, len(l.items))
	for i, it := range l.items {
		parts[i] = it.lead + keyNames(it.keys, l.prefixKey) + " " + it.name
	}
	return strings.Join(parts, " · ")
}

// findKey returns the binding of key in table.
func findKey(table []boundKey, key string) (boundKey, bool) {
	for _, bk := range table {
		if bk.key == key {
			return bk, true
		}
	}
	return boundKey{}, false
}

// A hinter is a modal whose keys come from a table, so its hint lines can
// be built from it.
type hinter interface {
	// keys is the modal's key table, as it is now.
	keys() []boundKey
	// hints is every hint line the modal shows now.
	hints() []hintLine
}
