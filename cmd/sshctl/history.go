package main

import "fmt"

// history prints the Connections opened, most recent first.
func (a *app) history(input) error {
	opened, err := a.catalog().History()
	if err != nil {
		return err
	}
	for _, alias := range opened {
		fmt.Fprintln(a.out, alias)
	}
	return nil
}

// recordHistory records in History that alias was opened.
func (a *app) recordHistory(alias string) error { return a.catalog().RecordOpen(alias) }
