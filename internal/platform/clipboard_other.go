//go:build !windows && !darwin

package platform

import "errors"

func readClipboard() (string, error) { return "", errors.ErrUnsupported }
