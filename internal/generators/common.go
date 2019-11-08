// Package generators contains common helpers for generators
package generators

import (
	"fmt"
	"os"
)

// CleanDirectory removes a directory and recreates it.
func CleanDirectory(p string) error {
	if err := os.RemoveAll(p); err != nil {
		return fmt.Errorf("Couldn't delete %q: %v", p, err)
	}
	if err := os.MkdirAll(p, 0755); err != nil {
		return fmt.Errorf("Couldn't create %q: %v", p, err)
	}
	return nil
}
