package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
)

func TestGroupColorSetsAndClearsAndListShowsIt(t *testing.T) {
	a, out := groupTree(t, "")
	if err := a.execute([]string{"group", "color", "prod", "red"}); err != nil {
		t.Fatal(err)
	}
	if got := loadCLI(t, a).Groups[0].Color; got != "red" {
		t.Errorf("prod's color = %q, want red", got)
	}
	if err := a.execute([]string{"group", "list"}); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"1  prod                 2  unset  color red\n" +
		"2  empty                0  unset\n" +
		"3  data                 1  unset\n"
	if out.String() != want {
		t.Errorf("group list:\n%s\nwant:\n%s", out.String(), want)
	}
	if err := a.execute([]string{"group", "color", "1", "none"}); err != nil {
		t.Fatal(err)
	}
	if got := loadCLI(t, a).Groups[0].Color; got != "" {
		t.Errorf("cleared color = %q", got)
	}
}

func TestGroupColorTakesTheSpaceOption(t *testing.T) {
	a, _ := spaceTree(t, "")
	if err := a.execute([]string{"group", "color", "prod", "yellow", "--space", "acme"}); err != nil {
		t.Fatal(err)
	}
	groups := loadCLI(t, a).Groups
	if groups[0].Color != "" || groups[1].Color != "yellow" {
		t.Errorf("colors = %q (home prod), %q (acme prod); want only acme's yellow", groups[0].Color, groups[1].Color)
	}
}

func TestSpaceColorSetsAndClearsAndListShowsIt(t *testing.T) {
	a, out := spaceTree(t, "")
	for _, args := range [][]string{
		{"space", "color", "acme", "magenta"},
		{"space", "color", "1", "green"},
		{"space", "color", "home", "none"},
		{"space", "list"},
	} {
		if err := a.execute(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	want := "" +
		"1  home                 1  3  (default)\n" +
		"2  acme                 2  1  color magenta\n" +
		"4  empty                0  0\n"
	if out.String() != want {
		t.Errorf("space list:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestColorCommandsRefuseAnUnknownColorListingTheValidOnes(t *testing.T) {
	for _, args := range [][]string{
		{"group", "color", "prod", "pink"},
		{"space", "color", "acme", "#ff0000"},
	} {
		a, _ := spaceTree(t, "")
		err := a.execute(args)
		if !errors.Is(err, config.ErrUnknownColor) || !strings.Contains(err.Error(), "none, red, yellow, green, cyan, blue, magenta") {
			t.Errorf("%v = %v, want ErrUnknownColor listing the colors", args, err)
		}
	}
}
