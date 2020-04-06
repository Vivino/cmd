package harness

import (
	"go/ast"
	"go/build"
	"go/token"
	"strings"

	"github.com/revel/revel"
)

type ImportCache map[string]string

func (ip ImportCache) processPackage(fset *token.FileSet, pkgImportPath, pkgPath string, pkg *ast.Package) *SourceInfo {
	var (
		structSpecs     []*TypeInfo
		initImportPaths []string

		methodSpecs     = make(methodMap)
		validationKeys  = make(map[string]map[int]string)
		scanControllers = strings.HasSuffix(pkgImportPath, "/controllers") ||
			strings.Contains(pkgImportPath, "/controllers/")
		scanTests = strings.HasSuffix(pkgImportPath, "/tests") ||
			strings.Contains(pkgImportPath, "/tests/")
	)

	// For each source file in the package...
	for _, file := range pkg.Files {

		// Imports maps the package key to the full import path.
		// e.g. import "sample/app/models" => "models": "sample/app/models"
		imports := map[string]string{}

		// For each declaration in the source file...
		for _, decl := range file.Decls {
			ip.addImports(imports, decl, pkgPath)

			if scanControllers {
				// Match and add both structs and methods
				structSpecs = appendStruct(structSpecs, pkgImportPath, pkg, decl, imports, fset)
				appendAction(fset, methodSpecs, decl, pkgImportPath, pkg.Name, imports)
			} else if scanTests {
				structSpecs = appendStruct(structSpecs, pkgImportPath, pkg, decl, imports, fset)
			}

			// If this is a func...
			if funcDecl, ok := decl.(*ast.FuncDecl); ok {
				// Scan it for validation calls
				lineKeys := getValidationKeys(fset, funcDecl, imports)
				if len(lineKeys) > 0 {
					validationKeys[pkgImportPath+"."+getFuncName(funcDecl)] = lineKeys
				}

				// Check if it's an init function.
				if funcDecl.Name.Name == "init" {
					initImportPaths = []string{pkgImportPath}
				}
			}
		}
	}

	// Add the method specs to the struct specs.
	for _, spec := range structSpecs {
		spec.MethodSpecs = methodSpecs[spec.StructName]
	}

	return &SourceInfo{
		StructSpecs:     structSpecs,
		ValidationKeys:  validationKeys,
		InitImportPaths: initImportPaths,
	}
}

func (ip ImportCache) addImports(imports map[string]string, decl ast.Decl, srcDir string) {
	genDecl, ok := decl.(*ast.GenDecl)
	if !ok {
		return
	}

	if genDecl.Tok != token.IMPORT {
		return
	}

	for _, spec := range genDecl.Specs {
		importSpec := spec.(*ast.ImportSpec)
		var pkgAlias string
		if importSpec.Name != nil {
			pkgAlias = importSpec.Name.Name
			if pkgAlias == "_" {
				continue
			}
		}
		quotedPath := importSpec.Path.Value           // e.g. "\"sample/app/models\""
		fullPath := quotedPath[1 : len(quotedPath)-1] // Remove the quotes

		if pkgAlias == "" {
			pkgAlias = ip[fullPath]
		}

		// If the package was not aliased (common case), we have to import it
		// to see what the package name is.
		// TODO: Can improve performance here a lot:
		// 1. Do not import everything over and over again.  Keep a cache.
		// 2. Exempt the standard library; their directories always match the package name.
		// 3. Can use build.FindOnly and then use parser.ParseDir with mode PackageClauseOnly

		if pkgAlias == "" {
			pkg, err := build.Import(fullPath, srcDir, 0)
			if err != nil {
				// We expect this to happen for apps using reverse routing (since we
				// have not yet generated the routes).  Don't log that.
				if !strings.HasSuffix(fullPath, "/app/routes") {
					revel.TRACE.Println("Could not find import:", fullPath)
				}
				continue
			}
			pkgAlias = pkg.Name
			ip[fullPath] = pkgAlias
		}

		imports[pkgAlias] = fullPath
	}
}
