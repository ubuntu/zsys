package config

import (
	"context"
	_ "embed"
	"fmt"
	"os"

	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	yaml "gopkg.in/yaml.v3"
)

//go:embed zsys.conf
var internalconf []byte

// TEXTDOMAIN is the message domain used by snappy; see dgettext(3)
// for more information.
const TEXTDOMAIN = "zsys"

// ErrorFormat switch between "%v" and "%+v" depending if we want more verbose info
var ErrorFormat = "%v"

// ZConfig stores the configuration of zsys
type ZConfig struct {
	History HistoryRules
	General struct {
		Timeout          int
		MinFreePoolSpace int
	}
	Path string
}

// HistoryRules store the rules for each GC element
type HistoryRules struct {
	GCStartAfter int64
	KeepLast     int
	GCRules      []struct {
		Name             string
		Buckets          int
		BucketLength     int64
		SamplesPerBucket int
	}
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

// Load reads a zsys configuration file into memory
func Load(ctx context.Context, path string) (ZConfig, error) {

	var c ZConfig

	b, err := os.ReadFile(path)
	if err != nil {
		log.Debug(ctx, i18n.G("failed to read configuration file %s: %v "), path, err)
		log.Debug(ctx, i18n.G("couldn't find default configuration path, fallback to internal default"))
		b = internalconf
	}

	err = yaml.Unmarshal(b, &c)
	if err != nil {
		return c, fmt.Errorf(i18n.G("failed to unmarshal yaml: %v"), err)
	}

	c.Path = path

	return c, nil
}

// SocketPath returns the unix path which can be overridden by environment variable
func SocketPath() string {
	s := defaultSocket
	overriddenS := os.Getenv(socketEnv)
	if overriddenS != "" {
		s = overriddenS
	}
	return s
}
