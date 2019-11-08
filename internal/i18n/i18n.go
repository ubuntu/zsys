package i18n

/*
 * This package is inspired from https://github.com/snapcore/snapd/blob/master/i18n, with other snap dependecies removed
 * and adapted to follow common go best practices.
 */

//FIXME: go:generate update-pot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/go-gettext"
)

type i18n struct {
	domain    string
	localeDir string
	loc       string

	gettext.Catalog
	translations gettext.Translations
}

var (
	locale i18n

	// G is the shorthand for Gettext
	G = func(msgid string) string { return msgid }
	// NG is the shorthand for NGettext
	NG = func(msgid string, msgidPlural string, n uint32) string { return msgid }
)

// InitI18nDomain calls bind + set locale to system values
func InitI18nDomain(domain string, options ...func(l *i18n)) {
	locale = i18n{
		domain:    domain,
		localeDir: "/usr/share/locale",
	}
	for _, option := range options {
		option(&locale)
	}

	locale.bindTextDomain(locale.domain, locale.localeDir)
	locale.setLocale(locale.loc)

	G = locale.Gettext
	NG = locale.NGettext
}

// langpackResolver tries to fetch locale mo file path.
// It first checks for the real locale (e.g. de_DE) and then
// tries to simplify the locale (e.g. de_DE -> de)
func langpackResolver(root string, locale string, domain string) string {
	for _, locale := range []string{locale, strings.SplitN(locale, "_", 2)[0]} {
		r := filepath.Join(locale, "LC_MESSAGES", fmt.Sprintf("%s.mo", domain))

		// look into the generated mo files path first for translations, then the system
		var candidateDirs []string
		// Ubuntu uses /usr/share/locale-langpack and patches the glibc gettext implementation
		candidateDirs = append(candidateDirs, filepath.Join(root, "..", "locale-langpack"))
		candidateDirs = append(candidateDirs, root)

		for _, dir := range candidateDirs {
			candidateMo := filepath.Join(dir, r)
			// Only load valid candidates, if we can't access it or have perm issues, ignore
			if _, err := os.Stat(candidateMo); err != nil {
				continue
			}
			return candidateMo
		}
	}

	return ""
}

func (l *i18n) bindTextDomain(domain, dir string) {
	l.translations = gettext.NewTranslations(dir, domain, langpackResolver)
}

// setLocale initializes the locale name and simplify it.
// If empty, it defaults to system ones set in LC_MESSAGES and LANG.
func (l *i18n) setLocale(loc string) {
	if loc == "" {
		loc = os.Getenv("LC_MESSAGES")
		if loc == "" {
			loc = os.Getenv("LANG")
		}
	}
	// de_DE.UTF-8, de_DE@euro all need to get simplified
	loc = strings.Split(loc, "@")[0]
	loc = strings.Split(loc, ".")[0]
	fmt.Println(loc)

	l.Catalog = l.translations.Locale(loc)
}

// https://www.gnu.org/software/gettext/manual/html_node/Plural-forms.html
// (search for 1000)
func ngn(d int) uint32 {
	const max = 1000000
	if d < 0 {
		d = -d
	}
	if d > max {
		return uint32((d % max) + max)
	}
	return uint32(d)
}
