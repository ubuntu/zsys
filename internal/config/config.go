package config

import (
	"os"

	"github.com/k0kubun/pp"
	"github.com/ubuntu/zsys/internal/log"
)

// ErrorFormat switch between "%v" and "%+v" depending if we want more verbose info
var ErrorFormat = "%v"

func init() {
	pp.SetDefaultOutput(os.Stderr)
}

// SetVerboseMode change ErrorFormat and logs between very, middly and non verbose
func SetVerboseMode(level int) {
	if level > 2 {
		level = 2
	}
	switch level {
	default:
		ErrorFormat = "%v"
		log.SetLevel(log.DefaultLevel)
	case 1:
		ErrorFormat = "%+v"
		log.SetLevel(log.InfoLevel)
	case 2:
		ErrorFormat = "%+v"
		log.SetLevel(log.DebugLevel)
	}
}
