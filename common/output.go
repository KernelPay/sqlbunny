package common

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/tools/go/ast/astutil"
)

var (
	noEditDisclaimer = []byte(`// Code generated by SQLBoiler (https://github.com/KernelPay/sqlboiler). DO NOT EDIT.
    // This file is meant to be re-generated in place and/or deleted at any time.

    `)

	rgxSyntaxError = regexp.MustCompile(`(\d+):\d+: `)
)

// WriteFileDisclaimer writes the disclaimer at the top with a trailing
// newline so the package name doesn't get attached to it.
func WriteFileDisclaimer(out *bytes.Buffer) {
	_, _ = out.Write(noEditDisclaimer)
}

// WritePackageName writes the package name correctly, ignores errors
// since it's to the concrete buffer type which produces none
func WritePackageName(out *bytes.Buffer, pkgName string) {
	_, _ = fmt.Fprintf(out, "package %s\n\n", pkgName)
}

// WriteImports writes the import list, ignores errors
// since it's to the concrete buffer type which produces none
func WriteImports(out *bytes.Buffer, imports map[string]string) {
	if len(imports) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "import (\n")
	for pkg, name := range imports {
		_, _ = fmt.Fprintf(out, "    %s \"%s\"\n", name, pkg)
	}
	_, _ = fmt.Fprintf(out, ")\n\n")
}

// WriteFile writes to the given folder and filename, formatting the buffer
// given.
func WriteFile(outFolder string, fileName string, input *bytes.Buffer) error {
	byt, err := removeUnusedImports(input.Bytes())
	if err != nil {
		return err
	}
	byt, err = formatBuffer(byt)
	if err != nil {
		return err
	}

	path := filepath.Join(outFolder, fileName)
	if err = ioutil.WriteFile(path, byt, 0666); err != nil {
		return errors.Wrapf(err, "failed to write output file %s", path)
	}

	return nil
}

func formatBuffer(src []byte) ([]byte, error) {
	output, err := format.Source(src)
	if err == nil {
		return output, nil
	}

	return nil, processAstError(err, src)
}

func processAstError(err error, src []byte) error {

	matches := rgxSyntaxError.FindStringSubmatch(err.Error())
	if matches == nil {
		return errors.Wrap(err, "failed to format template")
	}

	lineNum, _ := strconv.Atoi(matches[1])
	scanner := bufio.NewScanner(bytes.NewReader(src))
	errBuf := &bytes.Buffer{}
	line := 1
	for ; scanner.Scan(); line++ {
		if delta := line - lineNum; delta < -5 || delta > 5 {
			continue
		}

		if line == lineNum {
			errBuf.WriteString(">>>> ")
		} else {
			fmt.Fprintf(errBuf, "% 4d ", line)
		}
		errBuf.Write(scanner.Bytes())
		errBuf.WriteByte('\n')
	}

	return errors.Wrapf(err, "failed to format template\n\n%s\n", errBuf.Bytes())
}

func removeUnusedImports(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return nil, processAstError(err, src)
	}

	// refs are a set of possible package references in the code.
	refs := make(map[string]struct{})

	// decls are the current package imports. key is base package or renamed package.
	decls := make(map[string]*ast.ImportSpec)

	// We don't have the source dir, assume we don't have relative imports
	srcDir := ""

	// collect potential uses of packages.
	var visitor visitFn
	visitor = visitFn(func(node ast.Node) ast.Visitor {
		if node == nil {
			return visitor
		}
		switch v := node.(type) {
		case *ast.ImportSpec:
			if v.Name != nil {
				decls[v.Name.Name] = v
				break
			}
			ipath := strings.Trim(v.Path.Value, `"`)
			if ipath == "C" {
				break
			}
			local := importPathToName(ipath, srcDir)
			decls[local] = v
		case *ast.SelectorExpr:
			xident, ok := v.X.(*ast.Ident)
			if !ok {
				break
			}
			if xident.Obj != nil {
				// if the parser can resolve it, it's not a package ref
				break
			}
			pkgName := xident.Name
			refs[pkgName] = struct{}{}
		}
		return visitor
	})
	ast.Walk(visitor, f)

	// Nil out any unused ImportSpecs, to be removed in following passes
	unusedImport := map[string]string{}
	for pkg, is := range decls {
		if _, ok := refs[pkg]; !ok && pkg != "_" && pkg != "." {
			name := ""
			if is.Name != nil {
				name = is.Name.Name
			}
			unusedImport[strings.Trim(is.Path.Value, `"`)] = name
		}
	}
	for ipath, name := range unusedImport {
		if ipath == "C" {
			// Don't remove cgo stuff.
			continue
		}
		astutil.DeleteNamedImport(fset, f, name, ipath)
	}

	var buf bytes.Buffer
	err = printer.Fprint(&buf, fset, f)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// importPathToName returns the package name for the given import path.
var importPathToName func(importPath, srcDir string) (packageName string) = importPathToNameGoPath

// importPathToNameBasic assumes the package name is the base of import path.
func importPathToNameBasic(importPath, srcDir string) (packageName string) {
	return path.Base(importPath)
}

// importPathToNameGoPath finds out the actual package name, as declared in its .go files.
// If there's a problem, it falls back to using importPathToNameBasic.
func importPathToNameGoPath(importPath, srcDir string) (packageName string) {
	// Fast path for standard library without going to disk.
	//if pkg, ok := stdImportPackage[importPath]; ok {
	//	return pkg
	//}

	pkgName, err := importPathToNameGoPathParse(importPath, srcDir)
	if err == nil {
		return pkgName
	}
	return importPathToNameBasic(importPath, srcDir)
}

// importPathToNameGoPathParse is a faster version of build.Import if
// the only thing desired is the package name. It uses build.FindOnly
// to find the directory and then only parses one file in the package,
// trusting that the files in the directory are consistent.
func importPathToNameGoPathParse(importPath, srcDir string) (packageName string, err error) {
	buildPkg, err := build.Import(importPath, srcDir, build.FindOnly)
	if err != nil {
		return "", err
	}
	d, err := os.Open(buildPkg.Dir)
	if err != nil {
		return "", err
	}
	names, err := d.Readdirnames(-1)
	d.Close()
	if err != nil {
		return "", err
	}
	sort.Strings(names) // to have predictable behavior
	var lastErr error
	var nfile int
	for _, name := range names {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		nfile++
		fullFile := filepath.Join(buildPkg.Dir, name)

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, fullFile, nil, parser.PackageClauseOnly)
		if err != nil {
			lastErr = err
			continue
		}
		pkgName := f.Name.Name
		if pkgName == "documentation" {
			// Special case from go/build.ImportDir, not
			// handled by ctx.MatchFile.
			continue
		}
		if pkgName == "main" {
			// Also skip package main, assuming it's a +build ignore generator or example.
			// Since you can't import a package main anyway, there's no harm here.
			continue
		}
		return pkgName, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no importable package found in %d Go files", nfile)
}

type visitFn func(node ast.Node) ast.Visitor

func (fn visitFn) Visit(node ast.Node) ast.Visitor {
	return fn(node)
}
