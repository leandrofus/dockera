package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// translating functions and the index of their message argument
var msgArg = map[string]int{
	"i18n.Sprintf": 0, "i18n.Printf": 0, "i18n.Errorf": 0, "i18n.Fprintf": 1, "i18n.T": 1,
	"herr": 1, "badRequest": 0,
}

// keys built by concatenating a constant, which no single literal produces
var builtKeys = map[string]bool{
	"pam via the root helper (/run/ducktainer/pam.sock)": true,
}

// scanSource returns every string literal of the repository's Go code and
// the literal messages passed to the translating functions (with position).
func scanSource(t *testing.T) (lits map[string]bool, msgs map[string]string) {
	t.Helper()
	root := filepath.Join("..", "..")
	self := filepath.Join(root, "internal", "i18n")
	lits, msgs = map[string]bool{}, map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// skip dependencies, hidden dirs and this package (its catalogs hold every key)
		if d.IsDir() && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") && p != root || p == self) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BasicLit:
				if s, ok := unquote(x); ok {
					lits[s] = true
				}
			case *ast.CallExpr:
				if i, ok := msgArg[callName(x)]; ok && i < len(x.Args) {
					if b, ok := x.Args[i].(*ast.BasicLit); ok {
						if s, ok := unquote(b); ok {
							msgs[s] = fset.Position(x.Pos()).String()
						}
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return lits, msgs
}

func unquote(b *ast.BasicLit) (string, bool) {
	if b.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(b.Value)
	return s, err == nil
}

func callName(c *ast.CallExpr) string {
	switch fn := c.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if id, ok := fn.X.(*ast.Ident); ok {
			return id.Name + "." + fn.Sel.Name
		}
	}
	return ""
}

// TestCatalogsMatchCode keeps every catalog in step with the code: a message
// the code translates must be in each catalog, and a catalog key must still
// exist in the code (an edited English string otherwise silently loses its
// translation, as happened with the CLI usage text).
func TestCatalogsMatchCode(t *testing.T) {
	lits, msgs := scanSource(t)
	for lang, cat := range catalogs {
		for msg, at := range msgs {
			if _, ok := cat[msg]; !ok && strings.ContainsAny(msg, "abcdefghijklmnopqrstuvwxyz") {
				t.Errorf("%s: no translation for %q (%s)", lang, msg, at)
			}
		}
		for key := range cat {
			if !lits[key] && !builtKeys[key] {
				t.Errorf("%s: %q is no longer in the code (was the English text changed?)", lang, key)
			}
		}
	}
}
