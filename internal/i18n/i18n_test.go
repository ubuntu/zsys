package i18n_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/ubuntu/zsys/internal/i18n"
)

const (
	defaultDomain = "zsys-test"
	defaultLoc    = "en_DK"
	secondaryLoc  = "en"
)

var (
	defaultPo = fmt.Sprintf(`
msgid ""
msgstr ""
"Project-Id-Version: %s\n"
"Report-Msgid-Bugs-To: zsys-devel@lists.ubuntu.com\n"
"POT-Creation-Date: 2013-10-05 14:08+0200\n"
"Language: %s\n"
"MIME-Version: 1.0\n"
"Content-Type: text/plain; charset=UTF-8\n"
"Content-Transfer-Encoding: 8bit\n"
"Plural-Forms: nplurals=2; plural=n != 1;>\n"
msgid "plural_1"
msgid_plural "plural_2"
msgstr[0] "translated plural_1"
msgstr[1] "translated plural_2"
msgid "singular"
msgstr "translated singular"
`, defaultDomain, defaultLoc)

	localePo = map[string]string{
		defaultLoc: defaultPo,
		secondaryLoc: strings.ReplaceAll(
			strings.ReplaceAll(defaultPo, defaultLoc, secondaryLoc),
			"translated singular", "secondary translated singular"),
	}
)

func TestTranslations(t *testing.T) {
	t.Parallel()

	defaultLocaleDir, cleanup := tempLocaleDir(t)
	defer cleanup()
	compileMoFiles(t, defaultLocaleDir)

	tests := map[string]struct {
		// default is singular/translated singular
		text []string
		want string

		localeDir  string
		lcmessages string // lcmessages can be set to "-" to ensure it's empty
		lang       string
		domain     string
		loc        string // loc can be set to "-" to ensure it's empty

		rename map[string]string
		noinit bool
	}{
		"One text elem, prefer en_DK over en": {},
		"Multiple text elems":                 {text: []string{"plural_1", "plural_2"}, want: "translated plural_1"},

		// Locale preferences
		"en_DK@ is en_DK": {loc: defaultLoc + "@foo"},
		"en_DK. is en_DK": {loc: defaultLoc + ".foo"},
		"Fallback to en if en_DK isn't present": {
			want:   "secondary translated singular",
			rename: map[string]string{filepath.Join(defaultLocaleDir, "en_DK"): filepath.Join(defaultLocaleDir, "other")},
		},
		"Prefer locale-langpack to locale": {
			want: "secondary translated singular",
			rename: map[string]string{
				filepath.Join(defaultLocaleDir, "en"): filepath.Join(strings.ReplaceAll(defaultLocaleDir, "locale", "locale-langpack"), "en_DK"),
			},
		},

		"No loc prefers LC_MESSAGES first":           {lcmessages: "en_DK", loc: "-"},
		"No loc fallbacks to LANG if no LC_MESSAGES": {lang: "en_DK", loc: "-", lcmessages: "-"},

		"Untranslated elem":        {text: []string{"untranslated"}, want: "untranslated"},
		"Missing locale":           {loc: "doesntexists", want: "singular"},
		"Missing domain":           {domain: "doesntexists", want: "singular"},
		"Invalid locale directory": {localeDir: "/doesntexists", want: "singular"},
		"Init wasn't ran":          {noinit: true, want: "singular"},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			// We can't run those subtests in parallel as we want defer of global functions to end once all subtests are.

			// As we can't run those tests in parallel, we are doing the file switches and env changes to test priorities
			// in subtests here and reset globals.
			defer i18n.ResetGlobals()
			if tc.text == nil {
				tc.text = []string{"singular"}
			}
			if tc.want == "" {
				tc.want = "translated singular"
			}
			if tc.localeDir == "" {
				tc.localeDir = defaultLocaleDir
			}
			if tc.lcmessages == "" {
				tc.lcmessages = "FR_fr"
			} else if tc.lcmessages == "-" {
				tc.lcmessages = ""
			}
			defer switchEnv(t, "LC_MESSAGES", tc.lcmessages)()
			if tc.lang == "" {
				tc.lang = "FR_fr"
			}
			defer switchEnv(t, "LANG", tc.lang)()
			if tc.loc == "" {
				tc.loc = defaultLoc
			} else if tc.loc == "-" {
				tc.loc = ""
			}
			if tc.domain == "" {
				tc.domain = defaultDomain
			}
			if tc.rename != nil {
				for old, new := range tc.rename {
					defer renameElem(t, old, new)()
				}
			}

			if !tc.noinit {
				i18n.InitI18nDomain(tc.domain, i18n.WithLocaleDir(tc.localeDir), i18n.WithLoc(tc.loc))
			}
			switch len(tc.text) {
			case 1:
				assert.Equal(t, tc.want, i18n.G(tc.text[0]))
			case 2:
				assert.Equal(t, tc.want, i18n.NG(tc.text[0], tc.text[1], 1))
			default:
				t.Fatalf("unexpected case: %v", tc.text)
			}
		})
	}
}

func tempLocaleDir(t *testing.T) (string, func()) {
	t.Helper()

	dir, err := ioutil.TempDir("", "zsystest-")
	if err != nil {
		t.Fatal("can't create temporary directory", err)
	}
	return filepath.Join(dir, "locale"), func() {
		if err = os.RemoveAll(dir); err != nil {
			t.Error("can't clean temporary directory", err)
		}
	}
}

func compileMoFiles(t *testing.T, localeDir string) {
	t.Helper()

	for loc, poContent := range localePo {
		fullLocaleDir := filepath.Join(localeDir, loc, "LC_MESSAGES")
		if err := os.MkdirAll(fullLocaleDir, 0755); err != nil {
			t.Fatalf("couldn't create temporary directory %q: %v", fullLocaleDir, err)
		}

		po := filepath.Join(localeDir, defaultDomain+".po")
		mo := filepath.Join(fullLocaleDir, defaultDomain+".mo")

		if err := ioutil.WriteFile(po, []byte(poContent), 0644); err != nil {
			t.Fatalf("couldn't write po file: %v", err)
		}

		cmd := exec.Command("msgfmt", po, "--output-file", mo)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("couldn't compile %q to %q: %v", po, mo, err)
		}
	}
}

func switchEnv(t *testing.T, key, value string) func() {
	t.Helper()

	orig := os.Getenv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("couldn't change environment %s=%s: %v", key, value, err)
	}
	return func() {
		if err := os.Setenv(key, orig); err != nil {
			t.Fatalf("couldn't restore environment %s=%s: %v", key, orig, err)
		}
	}
}

func renameElem(t *testing.T, old, new string) func() {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(new), 0755); err != nil {
		t.Fatalf("couldn't create parent directory %q to be renamed: %v", new, err)
	}
	if err := os.Rename(old, new); err != nil {
		t.Fatalf("couldn't rename %q to %q: %v", old, new, err)
	}
	return func() {
		if err := os.Rename(new, old); err != nil {
			t.Fatalf("couldn't restore %q to %q: %v", new, old, err)
		}
	}
}
