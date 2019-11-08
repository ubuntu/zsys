// Package generators contains common helpers for generators
package generators

import (
	"fmt"
	"os"
)

const installVar = "ZSYS_INSTALL_SHARE_PREFIX"

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

// InstallOnlyMode returns if we only want to install and not affect current repository.
func InstallOnlyMode() bool {
	return os.Getenv(installVar) != ""
}

// DestDirectory returns the destination directory to generate to.
// It will prefer the zsys install directory if available, or will return path otherwise
func DestDirectory(p string) string {
	installDir := os.Getenv(installVar)
	if installDir == "" {
		installDir = p
	}
	return installDir
}
