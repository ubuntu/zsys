package config

import "time"

const (
	// ModifiedBoot is the message to print when the current boot has been modified
	ModifiedBoot = "zsys-meta:modified-boot"
	// NoModifiedBoot is the message to print when the current boot has no dataset modifications
	NoModifiedBoot = "zsys-meta:no-modified-boot"

	// DefaultSocket path.
	DefaultSocket = "/run/zsysd.sock"

	// DefaultClientTimeout for client requests
	DefaultClientTimeout = 30 * time.Second
)
