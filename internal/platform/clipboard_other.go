//go:build !windows && !darwin

package platform

import "errors"

// readClipboard has no clipboard to read here; the Workspace asks the
// terminal with OSC 52 instead.
func readClipboard() (string, error) { return "", errors.ErrUnsupported }
