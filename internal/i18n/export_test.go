package i18n

// WithLocaleDir enables overriding locale directory in tests
func WithLocaleDir(path string) func(l *i18n) {
	return func(l *i18n) {
		l.localeDir = path
	}
}

// WithLoc enables overriding loc settings in tests
func WithLoc(loc string) func(l *i18n) {
	return func(l *i18n) {
		l.loc = loc
	}
}
