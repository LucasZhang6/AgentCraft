//go:build !windows

package server

import "os"

func replaceFile(source, target string) error { return os.Rename(source, target) }
