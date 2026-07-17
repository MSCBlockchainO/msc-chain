// Command archguard enforces the compile-time dependency direction of the
// consensus-critical packages. Run it from the module root with:
//
//	go run ./tools/archguard
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var packageRules = map[string]struct {
	allowedInternal      map[string]bool
	forbiddenImports     map[string]bool
	forbiddenIdentifiers map[string]bool
}{
	"execution": {
		allowedInternal:      identifiers("state", "dtl"),
		forbiddenImports:     identifiers("time", "math/rand", "crypto/rand", "net/http"),
		forbiddenIdentifiers: identifiers("Node", "Mempool", "Blockchain", "RuntimeStatusSnapshot", "GlobalConfig"),
	},
	"dtl": {
		allowedInternal:      identifiers("state"),
		forbiddenImports:     identifiers("time", "math/rand", "crypto/rand", "net/http"),
		forbiddenIdentifiers: identifiers("Node", "Mempool", "Blockchain", "RuntimeStatusSnapshot", "GlobalConfig"),
	},
	"consensus": {
		allowedInternal: identifiers("execution", "registry", "network", "storage"),
	},
	"state":    {allowedInternal: identifiers("storage")},
	"registry": {allowedInternal: identifiers("state", "storage")},
	"network":  {allowedInternal: identifiers()},
	"storage":  {allowedInternal: identifiers()},
}

func identifiers(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

type violation struct {
	file    string
	line    int
	message string
}

func (v violation) String() string { return fmt.Sprintf("%s:%d: %s", v.file, v.line, v.message) }

func main() {
	root := flag.String("root", ".", "module root")
	flag.Parse()
	violations, err := check(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "archguard:", err)
		os.Exit(2)
	}
	if len(violations) == 0 {
		fmt.Println("architecture guard: clean")
		return
	}
	for _, item := range violations {
		fmt.Fprintln(os.Stderr, item.String())
	}
	os.Exit(1)
}

func modulePath(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return strings.TrimSpace(fields[1]), nil
		}
	}
	return "", fmt.Errorf("module path missing from go.mod")
}

func check(root string) ([]violation, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var violations []violation
	for packageName, rule := range packageRules {
		directory := filepath.Join(root, packageName)
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil, fmt.Errorf("required package %s: %w", packageName, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			parsed, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return nil, err
			}
			for _, spec := range parsed.Imports {
				importPath, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return nil, err
				}
				line := fset.Position(spec.Pos()).Line
				if rule.forbiddenImports[importPath] {
					violations = append(violations, violation{path, line, "forbidden runtime dependency " + importPath})
				}
				prefix := module + "/"
				if !strings.HasPrefix(importPath, prefix) {
					continue
				}
				relative := strings.TrimPrefix(importPath, prefix)
				dependency := strings.Split(relative, "/")[0]
				if !rule.allowedInternal[dependency] {
					violations = append(violations, violation{path, line, fmt.Sprintf("%s must not import %s", packageName, importPath)})
				}
				if packageName == "consensus" {
					lower := strings.ToLower(relative)
					for _, domain := range []string{"dtl", "token", "nft", "bridge", "lending"} {
						if strings.Contains(lower, domain) {
							violations = append(violations, violation{path, line, "consensus must not depend on " + domain})
						}
					}
				}
			}
			if len(rule.forbiddenIdentifiers) > 0 {
				ast.Inspect(parsed, func(node ast.Node) bool {
					identifier, ok := node.(*ast.Ident)
					if ok && rule.forbiddenIdentifiers[identifier.Name] {
						violations = append(violations, violation{path, fset.Position(identifier.Pos()).Line, packageName + " accesses forbidden runtime identifier " + identifier.Name})
					}
					return true
				})
			}
		}
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].file != violations[j].file {
			return violations[i].file < violations[j].file
		}
		if violations[i].line != violations[j].line {
			return violations[i].line < violations[j].line
		}
		return violations[i].message < violations[j].message
	})
	return violations, nil
}
