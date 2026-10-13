// Package i18n translates user-facing messages. English is the source
// language: messages are English fmt formats used as catalog keys
// (gettext-style), so an untranslated message still reads fine.
package i18n

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// catalogs maps a language to English format -> translated format.
var catalogs = map[string]map[string]string{
	"es": es,
}

// Supported lists the available languages (English first).
func Supported() []string { return []string{"en", "es"} }

func normalize(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if i := strings.IndexAny(lang, "-_.@"); i > 0 {
		lang = lang[:i]
	}
	if _, ok := catalogs[lang]; ok {
		return lang
	}
	return "en"
}

// Pick chooses a language from an explicit value (X-Lang) or an
// Accept-Language header.
func Pick(explicit, acceptLanguage string) string {
	if explicit != "" {
		return normalize(explicit)
	}
	for _, part := range strings.Split(acceptLanguage, ",") {
		tag := strings.TrimSpace(strings.Split(part, ";")[0])
		if l := normalize(tag); l != "en" || strings.HasPrefix(strings.ToLower(tag), "en") {
			return l
		}
	}
	return "en"
}

// Env returns the CLI language from LC_ALL / LC_MESSAGES / LANG.
func Env() string {
	for _, k := range []string{"DOCKERA_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" && v != "C" && v != "POSIX" {
			return normalize(v)
		}
	}
	return "en"
}

func lookup(lang, msg string) string {
	if c, ok := catalogs[normalize(lang)]; ok {
		if tr, ok := c[msg]; ok {
			return tr
		}
	}
	return msg
}

// T translates msg into lang and formats it with args. Error arguments
// that are *Error are rendered in the same language.
func T(lang, msg string, args ...any) string {
	f := lookup(lang, msg)
	if len(args) == 0 {
		return f
	}
	rendered := make([]any, len(args))
	for i, a := range args {
		if e, ok := a.(error); ok {
			rendered[i] = Render(e, lang)
		} else {
			rendered[i] = a
		}
	}
	return fmt.Sprintf(f, rendered...)
}

// Error is a translatable error: the English format plus its arguments.
type Error struct {
	Msg  string
	Args []any
}

func (e *Error) Error() string { return T("en", e.Msg, e.Args...) }

// In renders the error in a language.
func (e *Error) In(lang string) string { return T(lang, e.Msg, e.Args...) }

// Unwrap exposes error arguments to errors.Is / errors.As.
func (e *Error) Unwrap() []error {
	var out []error
	for _, a := range e.Args {
		if err, ok := a.(error); ok {
			out = append(out, err)
		}
	}
	return out
}

// Errorf builds a translatable error. Use %v (not %w) for wrapped errors;
// they are still reachable through errors.Is/As.
func Errorf(msg string, args ...any) error { return &Error{Msg: msg, Args: args} }

// Render returns err's message in lang when it is translatable.
func Render(err error, lang string) string {
	var e *Error
	if errors.As(err, &e) && error(e) == err {
		return e.In(lang)
	}
	// a translatable error wrapped by something that only adds context
	type langer interface{ In(string) string }
	if l, ok := err.(langer); ok {
		return l.In(lang)
	}
	return err.Error()
}

// Printf / Println / Fprintf for CLI output in the environment language.
func Printf(msg string, args ...any)         { fmt.Print(T(Env(), msg, args...)) }
func Sprintf(msg string, args ...any) string { return T(Env(), msg, args...) }
func Fprintf(w interface{ Write([]byte) (int, error) }, msg string, args ...any) {
	fmt.Fprint(w, T(Env(), msg, args...))
}
