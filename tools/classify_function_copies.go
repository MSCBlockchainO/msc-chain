package main

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type functionInfo struct {
	path        string
	key         string
	fingerprint string
	position    token.Position
}

type markerInsertion struct {
	offset int
	text   string
}

const (
	productionHotfix = "PRODUCTION HOTFIX"
	oldSourceCopy    = "OLD SOURCE COPY"
	experimentalPatch = "EXPERIMENTAL PATCH"
)

func main() {
	write := flag.Bool("write", false, "insert classification comments")
	flag.Parse()

	old := loadReferenceSets([]string{".codex-baseline-55a7c4e", ".codex-production-exact", ".codex-production-exact-v2"})
	hotfix := loadReferenceSets([]string{".codex-production-order-hotfix"})
	current := collectFunctions(".", true)

	counts := map[string]int{}
	byFile := map[string][]functionInfo{}
	classes := map[string]string{}
	for _, fn := range current {
		class := classify(fn, old[fn.key], hotfix[fn.key])
		counts[class]++
		classes[fn.path+"\x00"+fn.key+"\x00"+fmt.Sprint(fn.position.Offset)] = class
		byFile[fn.path] = append(byFile[fn.path], fn)
	}

	fmt.Printf("functions=%d production_hotfix=%d old_source_copy=%d experimental_patch=%d\n",
		len(current), counts[productionHotfix], counts[oldSourceCopy], counts[experimentalPatch])
	if !*write {
		return
	}

	changedFiles := 0
	addedMarkers := 0
	for path, functions := range byFile {
		added, err := markFile(path, functions, classes)
		if err != nil {
			panic(fmt.Errorf("%s: %w", path, err))
		}
		if added > 0 {
			changedFiles++
			addedMarkers += added
		}
	}
	fmt.Printf("changed_files=%d added_markers=%d\n", changedFiles, addedMarkers)
}

func classify(fn functionInfo, oldSet, hotfixSet map[string]struct{}) string {
	_, matchesOld := oldSet[fn.fingerprint]
	_, matchesHotfix := hotfixSet[fn.fingerprint]
	if matchesHotfix && !matchesOld {
		return productionHotfix
	}
	if matchesOld {
		return oldSourceCopy
	}
	return experimentalPatch
}

func loadReferenceSets(roots []string) map[string]map[string]struct{} {
	result := map[string]map[string]struct{}{}
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		for _, fn := range collectFunctions(root, false) {
			if result[fn.key] == nil {
				result[fn.key] = map[string]struct{}{}
			}
			result[fn.key][fn.fingerprint] = struct{}{}
		}
	}
	return result
}

func collectFunctions(root string, current bool) []functionInfo {
	var result []functionInfo
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && shouldSkipDir(root, path, entry.Name(), current) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.ToSlash(path) == "tools/classify_function_copies.go" {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(source[:min(len(source), 512)], []byte("Code generated")) && bytes.Contains(source[:min(len(source), 512)], []byte("DO NOT EDIT")) {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, source, 0)
		if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		packagePath := filepath.ToSlash(filepath.Dir(relativePath))
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			result = append(result, functionInfo{
				path:        path,
				key:         packagePath + "|" + receiverName(fn) + "|" + fn.Name.Name,
				fingerprint: fingerprint(fn),
				position:    fset.Position(fn.Pos()),
			})
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	return result
}

func shouldSkipDir(root, path, name string, current bool) bool {
	if name == ".git" || name == "runtime-data" || name == "runtime-logs" || name == "key-backups" || name == "bin" || strings.HasPrefix(name, "node_") {
		return true
	}
	if current && strings.HasPrefix(name, ".codex") {
		return true
	}
	return false
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	return expressionName(fn.Recv.List[0].Type)
}

func expressionName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return expressionName(value.X)
	case *ast.IndexExpr:
		return expressionName(value.X)
	case *ast.IndexListExpr:
		return expressionName(value.X)
	case *ast.SelectorExpr:
		return expressionName(value.X) + "." + value.Sel.Name
	default:
		return fmt.Sprintf("%T", expression)
	}
}

func fingerprint(fn *ast.FuncDecl) string {
	copyFn := *fn
	copyFn.Doc = nil
	stripPositionsAndComments(&copyFn)
	var output bytes.Buffer
	if err := format.Node(&output, token.NewFileSet(), &copyFn); err != nil {
		panic(err)
	}
	sum := sha256.Sum256(output.Bytes())
	return fmt.Sprintf("%x", sum[:])
}

func stripPositionsAndComments(node ast.Node) {
	value := reflect.ValueOf(node)
	stripValue(value)
}

func stripValue(value reflect.Value) {
	if !value.IsValid() {
		return
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		stripValue(value.Elem())
		return
	}
	if value.Kind() == reflect.Interface {
		if !value.IsNil() {
			stripValue(value.Elem())
		}
		return
	}
	if value.Kind() == reflect.Slice {
		for i := 0; i < value.Len(); i++ {
			stripValue(value.Index(i))
		}
		return
	}
	if value.Kind() != reflect.Struct {
		return
	}
	positionType := reflect.TypeOf(token.Pos(0))
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		structField := value.Type().Field(i)
		if structField.Type == positionType && field.CanSet() {
			field.SetInt(0)
			continue
		}
		stripValue(field)
	}
}

func markFile(path string, functions []functionInfo, classes map[string]string) (int, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	newline := "\n"
	if bytes.Contains(source, []byte("\r\n")) {
		newline = "\r\n"
	}
	var insertions []markerInsertion
	for _, fn := range functions {
		lineStart := fn.position.Offset
		for lineStart > 0 && source[lineStart-1] != '\n' {
			lineStart--
		}
		if hasClassificationMarker(source, lineStart) {
			continue
		}
		indentEnd := lineStart
		for indentEnd < len(source) && (source[indentEnd] == ' ' || source[indentEnd] == '\t') {
			indentEnd++
		}
		class := classes[fn.path+"\x00"+fn.key+"\x00"+fmt.Sprint(fn.position.Offset)]
		comment := markerText(class)
		insertions = append(insertions, markerInsertion{offset: lineStart, text: string(source[lineStart:indentEnd]) + "// " + comment + newline})
	}
	sort.Slice(insertions, func(i, j int) bool { return insertions[i].offset > insertions[j].offset })
	updated := append([]byte(nil), source...)
	for _, insertion := range insertions {
		updated = append(updated[:insertion.offset], append([]byte(insertion.text), updated[insertion.offset:]...)...)
	}
	if len(insertions) == 0 {
		return 0, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return len(insertions), os.WriteFile(path, updated, info.Mode())
}

func hasClassificationMarker(source []byte, lineStart int) bool {
	windowStart := max(0, lineStart-600)
	window := source[windowStart:lineStart]
	lastFunc := bytes.LastIndex(window, []byte("\nfunc "))
	lastMarker := max(
		bytes.LastIndex(window, []byte("PRODUCTION HOTFIX:")),
		bytes.LastIndex(window, []byte("OLD SOURCE COPY:")),
		bytes.LastIndex(window, []byte("EXPERIMENTAL PATCH:")),
	)
	return lastMarker >= 0 && lastMarker > lastFunc
}

func markerText(class string) string {
	switch class {
	case productionHotfix:
		return "PRODUCTION HOTFIX: Matches the retained production-hotfix implementation."
	case oldSourceCopy:
		return "OLD SOURCE COPY: Unchanged duplicate of a retained source snapshot."
	default:
		return "EXPERIMENTAL PATCH: Differs from the retained source and production-hotfix snapshots."
	}
}
