package config

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"

	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	yaml "gopkg.in/yaml.v2"
)

//go:generate go run generator.go

// TEXTDOMAIN is the message domain used by snappy; see dgettext(3)
// for more information.
const TEXTDOMAIN = "zsys"

// ErrorFormat switch between "%v" and "%+v" depending if we want more verbose info
var ErrorFormat = "%v"

// ZConfig stores the configiuration of zsys
type ZConfig struct {
	History struct {
		Users  GCRules
		System GCRules
	}
	General struct {
		Timeout int
	}
	Path string
}

// GCRules stores the rules of the garbage collector
type GCRules struct {
	PerDay   int
	PerWeek  int
	PerMonth int
	PerYear  int
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
	var dir http.FileSystem = http.Dir(filepath.Dir(path))
	f, err := dir.Open(filepath.Base(path))
	if err != nil {
		if path != DefaultPath {
			return c, fmt.Errorf(i18n.G("failed to load configuration file %s: %v "), path, err)
		}
		log.Info(ctx, i18n.G("couldn't find default configuration path, fallback to internal default"))
		if f, err = internalAssets.Open(filepath.Base(path)); err != nil {
			return c, fmt.Errorf(i18n.G("couldn't read our internal configuration: %v "), path, err)
		}
	}
	defer f.Close()

	b, err := ioutil.ReadAll(f)
	if err != nil {
		return c, fmt.Errorf(i18n.G("failed to read configuration file %s: %v "), path, err)
	}

	err = yaml.Unmarshal(b, &c)
	if err != nil {
		return c, fmt.Errorf(i18n.G("failed to unmarshal yaml: %v"), err)
	}

	c.Path = path

	return c, nil
}
