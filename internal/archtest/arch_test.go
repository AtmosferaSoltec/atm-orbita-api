// Package archtest hace cumplir las reglas de dependencia de AGENT_GUIDE §3:
//
//  1. domain no importa gin, pgx ni net/http.
//  2. Un módulo no importa los paquetes internos de otro: solo su paquete raíz
//     (module.go) o su paquete api/.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const modulePath = "github.com/AtmosferaSoltec/atm-orbita-api"

var forbiddenInDomain = []string{
	"github.com/gin-gonic/",
	"github.com/jackc/pgx",
	"net/http",
	"database/sql",
}

type goFile struct {
	path    string // relativo a internal/, con /
	imports []string
}

func internalFiles(t *testing.T) []goFile {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	var files []goFile
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		file := goFile{path: filepath.ToSlash(rel)}
		for _, spec := range parsed.Imports {
			value, _ := strconv.Unquote(spec.Path.Value)
			file.imports = append(file.imports, value)
		}
		files = append(files, file)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return files
}

func domainViolations(files []goFile) []string {
	var problems []string
	for _, file := range files {
		if !strings.Contains(file.path, "/domain/") {
			continue
		}
		for _, imported := range file.imports {
			for _, forbidden := range forbiddenInDomain {
				if strings.HasPrefix(imported, forbidden) {
					problems = append(problems, file.path+" imports "+imported+": domain must stay plain Go")
				}
			}
		}
	}
	return problems
}

func moduleViolations(files []goFile) []string {
	prefix := modulePath + "/internal/modules/"
	var problems []string
	for _, file := range files {
		owner, ok := moduleOf(file.path)
		if !ok {
			continue
		}
		for _, imported := range file.imports {
			rest, ok := strings.CutPrefix(imported, prefix)
			if !ok {
				continue
			}
			other, inner, _ := strings.Cut(rest, "/")
			if other == owner || inner == "" || inner == "api" {
				continue
			}
			problems = append(problems, file.path+" imports "+imported+": use the public API of module "+other)
		}
	}
	return problems
}

// moduleOf dice a qué módulo pertenece un archivo de internal/modules/<module>/...
func moduleOf(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "modules/")
	if !ok {
		return "", false
	}
	module, _, _ := strings.Cut(rest, "/")
	return module, true
}

func TestTheCodeFollowsTheDependencyRules(t *testing.T) {
	files := internalFiles(t)
	assert.Empty(t, domainViolations(files))
	assert.Empty(t, moduleViolations(files))
}

func TestTheRulesCatchViolations(t *testing.T) {
	files := []goFile{
		{path: "modules/ledger/domain/account.go", imports: []string{"github.com/jackc/pgx/v5"}},
		{path: "modules/ledger/domain/money.go", imports: []string{"github.com/shopspring/decimal"}},
		{path: "modules/reports/app/summary.go", imports: []string{modulePath + "/internal/modules/ledger/adapters/postgres"}},
		{path: "modules/reports/app/query.go", imports: []string{modulePath + "/internal/modules/ledger"}},
		{path: "modules/reports/app/port.go", imports: []string{modulePath + "/internal/modules/ledger/api"}},
		{path: "modules/ledger/app/use.go", imports: []string{modulePath + "/internal/modules/ledger/domain"}},
	}
	assert.Len(t, domainViolations(files), 1)
	assert.Len(t, moduleViolations(files), 1)
}
