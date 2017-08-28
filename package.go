package main

import (
	"fmt"
	"sort"
	"strings"
	"path"
)

// NewPackage generates a new (valid) Directory. An error is returned
// when the directory already exists.
func NewPackage(fullPath string) (pkg *Package, err error) {

	if _, ok := pkgsByLocation[fullPath]; ok {
		err = fmt.Errorf("Package already loaded: %s", fullPath)
		return
	}

	if fullPath == standardLibraryPath {
		pkg = &Package{}
		pkgsByLocation[fullPath] = pkg
		return
	}

	relPath := fullPath[len(standardLibraryPath+"/"):]
	internal := strings.Contains(relPath, "internal")

	pkg = &Package{RelPath: relPath, Internal: internal}
	pkgsByLocation[fullPath] = pkg
	return
}

// A Package represents a package in the standard library, and
// enumerates its dependencies.
type Package struct {
	RelPath      string
	Learned      bool
	Dependencies pkgList // no duplicates, always sorted.
	Dependants   pkgList // a list of Dependants.
	Internal     bool
}

// Add a dependency to the package.
func (pkg *Package) DependsOn(dependency *Package) {

	pkg.Dependencies = append(pkg.Dependencies, dependency)
	pkg.Dependencies.Sort()
	pkg.Dependencies.makeUnique()
}

// Returns the path to the package.
func (pkg *Package) FullPath() string {
	return path.Join(standardLibraryPath, pkg.RelPath)
}

// Returns the packages name (import path).
func (pkg *Package) Name() string {

	if pkg.IsVendor() {
		return path.Join(strings.Split(pkg.RelPath, "/")[1:]...)
	}

	return pkg.RelPath
}

// The dependency depth of a package is:
//     -1 (if the pkg is predeclared or learned)
//     0  (if the pkg has no dependencies)
//     max(dependency depth of dependencies) + 1 (else)
func (pkg *Package) DependencyDepth() (depth int) {

	var importedPkgDepth int

	if pkg.Predeclared() || pkg.Learned {
		return -1
	}

	for _, importedPkg := range pkg.Dependencies {
		importedPkgDepth = importedPkg.DependencyDepth()
		if importedPkgDepth >= depth {
			depth = importedPkgDepth + 1
		}
	}
	return
}

// is the directory in the vendor directory?
func (pkg *Package) IsVendor() bool {

	if strings.Split(pkg.RelPath, "/")[0] == vendorRelPath {
		return true
	}
	return false
}

func (pkg *Package) Predeclared() bool {
	return pkg.Name() == "builtin" || pkg.Name() == "C" || pkg.Name() == "unsafe"
}

// is the package imported by a non-internal package somewhere in its dependants tree?
func (pkg *Package) Imported() bool {

	// Check if any of the dependants are non-internal.
	for _, dependant := range pkg.Dependants {
		if !dependant.Internal {
			return true
		}
	}

	// The package is not imported by any non-internal packages. Check if its dependants
	// are imported.
	for _, dependant := range pkg.Dependants {
		if dependant.Imported() {
			return true
		}
	}

	// The package has no non-internal packages in its dependants tree.
	return false
}

func (pkg *Package) Write() (n int, err error) {
	var rowFormat string
	var δn int


	imported := pkg.Imported()
	dependancyDepth := pkg.DependencyDepth()

	// Don't print details for non-imported internal packages.
	if pkg.Internal && !imported {
		return
	}

	// Indicates if the package has been imported by other
	// packages or not.
	importFlag := "imported"
	if !imported {
		importFlag = "unimported"
	}

	// No dependencies. Print the package information and continue.
	if len(pkg.Dependencies) == 0 {
		rowFormat = fmt.Sprintf(noDependenciesRow, pkg.Name(), dependancyDepth, importFlag)
		n, err = fmt.Fprintln(outputWriter, rowFormat)
		return
	}

	// There are dependencies. Print package info, then dependencies on each line.
	for i, dependency := range pkg.Dependencies {

		dependencyName := dependency.Name()
		if !dependency.Learned && !dependency.Predeclared() {
			dependencyName += (" " + unlearnedIndicator)
		}

		if i == 0 {
			// Print package info + first dependency.
			rowFormat = fmt.Sprintf(firstRow, pkg.Name(), dependancyDepth, importFlag, dependencyName)
		} else {
			// Print only the dependency - don't repeat package info.
			rowFormat = fmt.Sprintf(subsequentRows, dependencyName)
		}

		// Write the output line.
		δn, err = fmt.Fprintln(outputWriter, rowFormat)
		n += δn

		if err != nil {
			return
		}
	}

	return
}

// pkgList implements sort.Interface.
type pkgList []*Package

func (li pkgList) Len() int           { return len(li) }
func (li pkgList) Less(i, j int) bool { return li[i].Name() < li[j].Name() }
func (li pkgList) Swap(i, j int)      { li[i], li[j] = li[j], li[i] }
func (li pkgList) Sort()              { sort.Sort(li) }

//  This function assumes that the list is sorted.
func (li *pkgList) makeUnique() {

	for i := 0; i < len(*li)-1; {
		if (*li)[i] == (*li)[i+1] {
			*li = append((*li)[:i], (*li)[i+1:]...)
			continue
		}
		i++
	}
}