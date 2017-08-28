package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
)

// Defined by the user at the command line
var (
	configFile string
	inputFile  string
)

// Read from the config file provided
var (
	standardLibraryPath string
	vendorRelPath       string
)

// 300 is the max number of directories possible.
var pkgsByLocation = make(map[string]*Package, 300)

// 30 is a good maximum dependency depth.
var pkgsByDependencyDepth = make(map[int]pkgList, 30)

// The writer used for output.
const padding = 2
const padChar = ' '
const unlearnedIndicator = "*"

// Row output formats
const (
	noDependenciesRow = "%s\t%d\t%s\t"
	firstRow          = "%s\t%d\t%s\t%s\t"
	subsequentRows    = "\t\t\t%s\t"
)

var outputWriter *tabwriter.Writer = tabwriter.NewWriter(os.Stdout, 0, 0, padding, padChar, 0)

func init() {

	flag.StringVar(
		&inputFile,
		"input-file",
		"input.txt",
		"A file containing all the packages already learnt",
	)
	flag.StringVar(
		&configFile,
		"config-file",
		"config.txt",
		"A file containing the standard library configuration",
	)
}

func main() {

	flag.Parse()
	readConfig()

	NewPackage(path.Join(standardLibraryPath, "C")) // pseudo-directory.
	if err := filepath.Walk(standardLibraryPath, loadPath); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := loadLearnedPkgs(); err != nil {
		fmt.Println(err)
	}

	if err := loadDependencies(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, pkg := range pkgsByLocation {
		pkg.Dependants.Sort()
		pkg.Dependants.makeUnique()
	}

	sortByDependencyDepth()
	printPkgs()
}

// Read the config file.
func readConfig() {
	// Read the Go standard library config from the config file.
	f, err := os.Open(configFile)
	defer f.Close()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	_scanner := bufio.NewScanner(f)

	lineNumber := 0
	for _scanner.Scan() {
		lineNumber++
		configLine := strings.Split(_scanner.Text(), ":")
		if len(configLine) != 2 {
			fmt.Printf("Error parsinng line %d\n", lineNumber)
			os.Exit(1)
		}
		configKey := strings.TrimSpace(configLine[0])
		configValue := strings.TrimSpace(configLine[1])

		switch configKey {
		case "standardLibraryPath":
			standardLibraryPath = configValue
		case "vendorRelPath":
			vendorRelPath = configValue
		default:
			fmt.Printf("Invalid config key: %s\n", configKey)
			os.Exit(1)
		}
	}
}

// Sort packages by dependency depth.
func sortByDependencyDepth() {

	for _, pkg := range pkgsByLocation {
		depth := pkg.DependencyDepth()
		li, _ := pkgsByDependencyDepth[depth]
		pkgsByDependencyDepth[depth] = append(li, pkg)
	}

	for _, li := range pkgsByDependencyDepth {
		li.Sort()
	}
}

// Read the input file for packages already learned.
func loadLearnedPkgs() (err error) {

	f, err := os.Open(inputFile)
	defer f.Close()
	if err != nil {
		return
	}

	_scanner := bufio.NewScanner(f)
	for _scanner.Scan() {
		pkg := pkgFromImportPath(_scanner.Text())

		if pkg != nil {
			pkg.Learned = true
		}
	}

	return
}

// loadPath loads all the package info in the given path.
func loadPath(_path string, info os.FileInfo, e error) (err error) {

	if info == nil || e != nil {
		err = e
		return
	}

	if info.IsDir() {

		if _path == standardLibraryPath {
			return
		}

		pathBase := filepath.Base(_path)
		if pathBase == "cmd" || pathBase == "testdata" {
			err = filepath.SkipDir
			return
		}

		return
	}

	err = loadFileInfo(_path)
	return
}

// Parse the file and load package information.
func loadFileInfo(_path string) (err error) {

	if filepath.Ext(_path) != ".go" {
		return
	}

	// Test files are ignored.
	if isTest, _ := filepath.Match("*_test.go", filepath.Base(_path)); isTest {
		return
	}

	if _, found := pkgsByLocation[filepath.ToSlash(filepath.Dir(_path))]; found {
		// the package has been added already. The remaining files
		// needn't be scanned.
		return
	}

	// We just want the package clause.
	fileset := token.NewFileSet()
	astFile, err := parser.ParseFile(fileset, _path, nil, parser.PackageClauseOnly)
	if err != nil || astFile.Name == nil {

		// if an error occurs in parsing, it's a bad file. Move on.
		switch err.(type) {
		case scanner.Error, scanner.ErrorList:
			err = nil
		}
		return
	}

	// Only if the package name is the same as the directory do we have
	// a package from the Go standard library.
	if astFile.Name.Name == filepath.Base(filepath.Dir(_path)) {
		_, err = NewPackage(path.Dir(filepath.ToSlash(_path)))
	}

	return
}

// Loads all dependencies.
func loadDependencies() (err error) {

	for _path, pkg := range pkgsByLocation {

		if pkg.Name() == "C" {
			continue
		}

		fileList, e := ioutil.ReadDir(filepath.FromSlash(_path))
		if e != nil {
			err = e
			return
		}

		for _, f := range fileList {
			err = scanFileForDependencies(
				filepath.Join(filepath.FromSlash(_path), f.Name()),
				pkg,
			)
			if err != nil {
				return
			}
		}

	}
	return
}

// scanFileForDependencies assumes the file is a go file that compiles.
// It scans for any imports in the package with the same name as the
// directory the go file is in. It ignores test files.
func scanFileForDependencies(_path string, pkg *Package) (err error) {

	if filepath.Ext(_path) != ".go" {
		return
	}

	if isTest, _ := filepath.Match("*_test.go", filepath.Base(_path)); isTest {
		return
	}

	// Only parse up to the import statement as that is all we need.
	fileset := token.NewFileSet()
	astFile, err := parser.ParseFile(fileset, _path, nil, parser.ImportsOnly)
	if err != nil || astFile.Name == nil {
		return
	}

	if astFile.Name.Name != filepath.Base(pkg.Name()) {
		// the package name doesn't match the current directory
		return
	}

	for _, importSpec := range astFile.Imports {
		// record each package that the current file depends on
		importedPkg := pkgFromImportPath(strings.Trim(importSpec.Path.Value, `"`))
		pkg.DependsOn(importedPkg)
		importedPkg.Dependants = append(importedPkg.Dependants, pkg)
	}

	return
}

// importedPkg finds the package corresponding to a given import path.
func pkgFromImportPath(importPath string) (pkg *Package) {

	var fullPath string

	if isVendor, _ := regexp.MatchString(`golang_org/x/\w*`, importPath); isVendor {
		fullPath = path.Join(standardLibraryPath, "vendor", importPath)
	} else {
		fullPath = path.Join(standardLibraryPath, importPath)
	}

	pkg, _ = pkgsByLocation[fullPath]
	return
}

// print the output.
func printPkgs() {

	defer outputWriter.Flush()

	// skip depth -1 since that is reserved for learned or built-in
	// packages.
	for depth := 0; ; depth++ {
		pkgLi, ok := pkgsByDependencyDepth[depth]
		if !ok {
			return
		}

		// Print each package with the given dependency depth.
		for _, pkg := range pkgLi {
			pkg.Write()
		}
	}
}
