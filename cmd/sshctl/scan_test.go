package main

import (
	"reflect"
	"testing"
)

// Completion reads words through the same parser, tolerantly: what strict
// parsing refuses is skipped, and a value option given last is pending.
func TestScanArgsTolerant(t *testing.T) {
	in, pending, err := scanArgs([]string{"--bogus", "web", "-y", "-y", "--yes=1", "--space"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in.args, []string{"web"}) || !in.flag("yes") {
		t.Errorf("scanArgs = %+v", in)
	}
	if pending == nil || pending.name != "space" {
		t.Errorf("pending = %v, want --space", pending)
	}
	if in, _, _ := scanArgs([]string{"web", "--"}, false); !in.dash {
		t.Error("-- not seen")
	}
	if _, _, err := scanArgs([]string{"--bogus"}, true); err == nil {
		t.Error("strict scanArgs took an unknown option")
	}
}
