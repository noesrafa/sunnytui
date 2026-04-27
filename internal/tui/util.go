package tui

import "os"

func homedir() string {
	h, _ := os.UserHomeDir()
	return h
}
