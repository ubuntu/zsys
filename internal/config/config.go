package config

import (
	"os"

	"github.com/k0kubun/pp"
	log "github.com/sirupsen/logrus"
)

// ErrorFormat switch between "%v" and "%+v" depending if we want more verbose info
var ErrorFormat = "%v"

func init() {
	pp.SetDefaultOutput(os.Stderr)
}

// SetVerboseMode change ErrorFormat and logs between very, middly and non verbose
func SetVerboseMode(level int) {
	switch level {
	case 0:
		ErrorFormat = "%v"
		log.SetFormatter(&log.TextFormatter{
			DisableLevelTruncation: true,
			DisableTimestamp:       true,
		})
		log.SetLevel(log.WarnLevel)
	case 1:
		ErrorFormat = "%+v"
		log.SetFormatter(&log.TextFormatter{DisableLevelTruncation: true})
		log.SetLevel(log.InfoLevel)
		log.Debug("verbosity set to info and will print stacktraces")
	case 2:
		ErrorFormat = "%+v"
		log.SetFormatter(&log.TextFormatter{DisableLevelTruncation: true})
		log.SetLevel(log.DebugLevel)
		log.Debug("verbosity set to debug and will print stacktraces")
	}
}
