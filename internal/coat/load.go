package coat

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadedCoat is a coat with its source file path for error reporting.
type LoadedCoat struct {
	Coat     Coat
	FilePath string
}

// LoadPaths loads coat files from a list of file and directory paths.
// Directories are scanned non-recursively for .yaml, .yml, and .json files.
// Returns all loaded coats with their source paths, and any errors encountered.
func LoadPaths(paths []string) ([]LoadedCoat, []error) {
	var loaded []LoadedCoat
	var errs []error

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("cannot access %s: %w", p, err))
			continue
		}

		if info.IsDir() {
			lc, loadErrs := loadDir(p)
			loaded = append(loaded, lc...)
			errs = append(errs, loadErrs...)
		} else {
			lc, loadErrs := loadFile(p)
			loaded = append(loaded, lc...)
			errs = append(errs, loadErrs...)
		}
	}

	return loaded, errs
}

func loadDir(dir string) ([]LoadedCoat, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []error{fmt.Errorf("reading directory %s: %w", dir, err)}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var loaded []LoadedCoat
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			continue
		}
		lc, loadErrs := loadFile(filepath.Join(dir, entry.Name()))
		loaded = append(loaded, lc...)
		errs = append(errs, loadErrs...)
	}
	return loaded, errs
}

func loadFile(path string) ([]LoadedCoat, []error) {
	f, err := ParseFile(path)
	if err != nil {
		return nil, []error{err}
	}

	errs := Validate(f)
	var validationErrs []error
	for _, e := range errs {
		validationErrs = append(validationErrs, fmt.Errorf("%s: %s", path, e.Error()))
	}

	var loaded []LoadedCoat
	for _, c := range f.Coats {
		loaded = append(loaded, LoadedCoat{Coat: c, FilePath: path})
	}
	return loaded, validationErrs
}
