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

// IsCoatFile reports whether the given path has a recognised coat file extension
// (.yaml, .yml, or .json).
func IsCoatFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml" || ext == ".json"
}

// LoadResult contains the results of loading coat files.
type LoadResult struct {
	Coats    []LoadedCoat
	Errors   []error
	Warnings []string
}

// LoadPaths loads coat files from a list of file and directory paths.
// Directories are scanned non-recursively for .yaml, .yml, and .json files.
// Returns all loaded coats with their source paths, and any errors encountered.
func LoadPaths(paths []string) ([]LoadedCoat, []error) {
	result := LoadPathsWithWarnings(paths)
	return result.Coats, result.Errors
}

// LoadPathsWithWarnings loads coat files and returns both errors and warnings.
func LoadPathsWithWarnings(paths []string) LoadResult {
	var result LoadResult

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("cannot access %s: %w", p, err))
			continue
		}

		if info.IsDir() {
			lc, loadErrs, warns := loadDir(p)
			result.Coats = append(result.Coats, lc...)
			result.Errors = append(result.Errors, loadErrs...)
			result.Warnings = append(result.Warnings, warns...)
		} else {
			lc, loadErrs, warns := loadFile(p)
			result.Coats = append(result.Coats, lc...)
			result.Errors = append(result.Errors, loadErrs...)
			result.Warnings = append(result.Warnings, warns...)
		}
	}

	return result
}

func loadDir(dir string) ([]LoadedCoat, []error, []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []error{fmt.Errorf("reading directory %s: %w", dir, err)}, nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var loaded []LoadedCoat
	var errs []error
	var warnings []string
	for _, entry := range entries {
		if entry.IsDir() || !IsCoatFile(entry.Name()) {
			continue
		}
		lc, loadErrs, warns := loadFile(filepath.Join(dir, entry.Name()))
		loaded = append(loaded, lc...)
		errs = append(errs, loadErrs...)
		warnings = append(warnings, warns...)
	}
	return loaded, errs, warnings
}

func loadFile(path string) ([]LoadedCoat, []error, []string) {
	f, err := ParseFile(path)
	if err != nil {
		return nil, []error{err}, nil
	}

	result := ValidateWithWarnings(f)
	var validationErrs []error
	for _, e := range result.Errors {
		validationErrs = append(validationErrs, fmt.Errorf("%s: %s", path, e.Error()))
	}

	var warnings []string
	for _, w := range result.Warnings {
		warnings = append(warnings, fmt.Sprintf("%s: %s", path, w.String()))
	}

	loaded := make([]LoadedCoat, len(f.Coats))
	for i, c := range f.Coats {
		loaded[i] = LoadedCoat{Coat: c, FilePath: path}
	}
	return loaded, validationErrs, warnings
}
