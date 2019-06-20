package config

// ErrorFormat switch between "%v" and "%+v" depending if we want more verbose info
var ErrorFormat = "%v"

// SetVerboseMode change ErrorFormat between verbose and non verbose mode
func SetVerboseMode(verbose bool) {
	if verbose {
		ErrorFormat = "%+v"
	} else {
		ErrorFormat = "%v"
	}
}
