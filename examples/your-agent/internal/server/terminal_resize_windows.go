//go:build windows

package server

import "os"

func setWinsize(_ *os.File, _, _ uint16) error { return nil }
