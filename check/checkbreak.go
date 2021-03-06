package check

import (
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
)

// Break represents base structure required for evaluating code changes
type Break struct {
	workingPath string
	startPoint  string
	endPoint    string
	config      *config
}

// Init bootstraps Break structure
func Init(workingPath string, startPoint string, endPoint string, configFilename string) (*Break, error) {
	if errPath := os.Chdir(workingPath); errPath != nil {
		return nil, fmt.Errorf("Path %s doesn't exist", workingPath)
	}

	if !refExists(startPoint) {
		return nil, fmt.Errorf("The object %s doesn't exist", startPoint)
	}

	if !refExists(endPoint) {
		return nil, fmt.Errorf("The object %s doesn't exist", endPoint)
	}

	return &Break{
		workingPath: workingPath,
		startPoint:  startPoint,
		endPoint:    endPoint,
		config:      loadConfiguration(workingPath, configFilename),
	}, nil
}

// HasConfiguration verifies that the config has been loaded
func (b *Break) HasConfiguration() bool {
	return b.config != nil
}

// filter drops a file if it satisfies exclusion criteria
func (b *Break) filter(files []file) []file {
	if 0 == len(b.exclusions()) {
		return files
	}
	filtered := make([]file, 0)
	var toExclude bool
	excluded := b.config.Excluded.Path
	for _, f := range files {
		toExclude = false
		for _, e := range excluded {
			if strings.HasPrefix(f.name, e) {
				toExclude = true
				break
			}
		}
		if !toExclude {
			filtered = append(filtered, f)
		}
	}

	return filtered
}

// exclusions is the exclusion list provided by config file
func (b *Break) exclusions() []string {
	excluded := make([]string, 0)
	if b.HasConfiguration() {
		for _, path := range b.config.Excluded.Path {
			excluded = append(excluded, path)
		}
	}

	return excluded
}

// file is a file representation
type file struct {
	name     string
	status   string
	diff     diff
	typeFile string
}

// method is a potential break on a public method
type method struct {
	before       string
	after        string
	commonFactor string
	explanation  string
}

// breaks returns all potentials CB on a file
func (f *file) breaks() (*[]method, error) {
	pattern, err := f.breakPattern()
	if err != nil {
		return nil, err
	}

	var methods []method
	var moveOnly bool
	for _, deleted := range f.diff.deletions {
		var closestAdding string
		moveOnly = false
		commonFactor := pattern.FindStringSubmatch(deleted)[0]
		for _, added := range f.diff.addings {
			if strings.HasPrefix(added, commonFactor) {
				// It's only a move
				if len(strings.Split(deleted, " ")) == len(strings.Split(added, " ")) && len(deleted) == len(added) {
					moveOnly = true
					break
				} else {
					closestAdding = added
				}
			}
		}

		explanation := explainedChanges(deleted, closestAdding)
		if !moveOnly && explanation != "" {
			method := method{
				before:       deleted,
				after:        closestAdding,
				commonFactor: commonFactor,
				explanation:  explanation,
			}
			methods = append(methods, method)
		}
	}

	return &methods, nil
}

// files initializes files struct
func files(changedFiles []string, b Break) ([]file, []file) {
	supported := make([]file, 0)
	ignored := make([]file, 0)

	for _, fileLine := range changedFiles {
		f := file{}
		status, name, filetype := extractDataFile(fileLine)
		f.name = name
		f.status = status
		f.typeFile = filetype
		diff, err := f.getDiff(b.startPoint, b.endPoint)
		if err == nil {
			f.diff = *diff
		}

		if f.canHaveBreak() {
			if f.isTypeSupported() {
				supported = append(supported, f)
			} else {
				ignored = append(ignored, f)
			}
		}
	}

	return supported, ignored
}

func (f *file) canHaveBreak() bool {
	return "A" != f.status
}

// explainedChanges try to understand nature of changes, returning a reason
// for compatibility break
func explainedChanges(before string, after string) string {
	if after == "" {
		return "Deletion of method"
	}

	deleted, added := differences(strings.Split(before, ","), strings.Split(after, ","))
	if len(deleted) > len(added) {
		if hasDefaultParameter(deleted) && !hasDefaultParameter(added) {
			return "Deletion of default parameter"
		}
		return "Deletion of parameter"
	} else if len(deleted) < len(added) {
		var explanation string
		for i := 0; i < len(added); i++ {
			if !hasDefaultParameter(added) {
				explanation = "Adding a parameter without default value"
			}
		}
		return explanation
	} else {
		explanation := "Unknown signature change"
		for i := 0; i < len(deleted); i++ {
			if !hasDefaultParameter(added) {
				if hasDefaultParameter(deleted) {
					return "Deletion of default parameter"
				}
				return "Adding a parameter without default value"
			}
			// TODO : Precise cases :
			//	- add type
			//	- change type
			// 	- drop type (not a CB)
		}
		return explanation
	}
}

func hasDefaultParameter(slice []string) bool {
	for _, s := range slice {
		if strings.Contains(s, "=") {
			return true
		}
	}

	return false
}

// differences shows slices of differences (deletion, adding) between two slices
func differences(before []string, after []string) ([]string, []string) {
	var length int
	lengthBefore := len(before)
	lengthAfter := len(after)

	if lengthBefore < lengthAfter {
		for i := lengthBefore; i < lengthAfter; i++ {
			before = append(before, "")
		}
		length = lengthAfter
	} else {
		for i := lengthAfter; i < lengthBefore; i++ {
			after = append(after, "")
		}
		length = lengthBefore
	}

	var deleted []string
	var added []string
	for i := 0; i < length; i++ {
		if before[i] == after[i] {
			continue
		}
		if before[i] == "" {
			added = append(added, after[i])
		} else if after[i] == "" {
			deleted = append(deleted, before[i])
		} else {
			deleted = append(deleted, before[i])
			added = append(added, after[i])
		}
	}

	return deleted, added
}

// extractDataFile gives file's name, status and type
func extractDataFile(fileLine string) (string, string, string) {
	fields := strings.Fields(fileLine)
	status := fields[0]
	name := fields[1]
	if strings.HasPrefix(status, "R") {
		status = "D"
	}

	return status, name, typefile(name)
}

// typeFile return a file's extension
func typefile(filepath string) string {
	var typeFile string
	filename := path.Base(filepath)
	if strings.Contains(filename, ".") && !strings.HasPrefix(filename, ".") {
		typeFile = strings.TrimSpace(path.Ext(filename)[1:])
	}

	return typeFile
}

// diff represents the diff of a file, segregated with deletion and adding
type diff struct {
	deletions []string
	addings   []string
}

// getDiff fetches diff (in a git sense) and extracts changes occured
func (f *file) getDiff(startObject string, endObject string) (*diff, error) {
	if f.isDeleted() {
		return f.getDiffDeleted(startObject)
	}
	diffFile, err := diffFile(startObject, endObject, f.name)
	if err != nil {
		return nil, err
	}

	pattern, errPattern := f.breakPattern()
	if errPattern != nil {
		return nil, errPattern
	}

	var diffDeleted []string
	var diffAdded []string
	for _, line := range diffFile {
		if strings.HasPrefix(line, "-") {
			diffDeleted = append(diffDeleted, strings.TrimSpace(line[1:]))
		} else if strings.HasPrefix(line, "+") {
			diffAdded = append(diffAdded, strings.TrimSpace(line[1:]))
		}
	}

	return &diff{
		deletions: filteredByPattern(pattern, diffDeleted),
		addings:   filteredByPattern(pattern, diffAdded),
	}, nil
}

func (f *file) isDeleted() bool {
	return "D" == f.status
}

func (f *file) getDiffDeleted(startObject string) (*diff, error) {
	diffFile, err := showFile(startObject, f.name)
	if err != nil {
		return nil, err
	}

	pattern, errPattern := f.breakPattern()
	if errPattern != nil {
		return nil, errPattern
	}
	var diffDeleted []string
	for _, line := range diffFile {
		diffDeleted = append(diffDeleted, strings.TrimSpace(line))
	}

	return &diff{
		deletions: filteredByPattern(pattern, diffDeleted),
	}, nil
}

// filteredByPattern keeps only data lines that match a pattern
func filteredByPattern(r *regexp.Regexp, data []string) []string {
	filtered := make([]string, 0)
	for _, element := range data {
		if r.MatchString(element) {
			filtered = append(filtered, element)
		}
	}

	return filtered
}

func (f *file) isTypeSupported() bool {
	_, err := f.breakPattern()

	return err == nil
}

// breakPattern returns the regex of a potential compatibility break associated
// with type of the file
func (f *file) breakPattern() (*regexp.Regexp, error) {
	var pattern *regexp.Regexp
	switch f.typeFile {
	case "go":
		pattern = regexp.MustCompile(`^(\s)*func( \(.+)\)? [A-Z]{1}[A-Za-z]*\(`)
	case "php":
		pattern = regexp.MustCompile(`^(\s)*public( static)? function [_A-Za-z]+\(|^(\s)*function [_A-Za-z]+\(`)
	case "java":
		pattern = regexp.MustCompile(`^(\s)*public( static)?( .+)? [A-Za-z]+\(`)
	case "js":
		pattern = regexp.MustCompile(`^(\s)*function [A-Za-z]+\(|^(\s)*(var )?[A-Za-z._]+(\s)*=(\s)*function \(|(\s)*[A-Za-z._]+(\s)*:(\s)*function \(`)
	case "sh":
		pattern = regexp.MustCompile(`^(\s)*function [A-Za-z_]+\(`)
	}

	if pattern == nil {
		return pattern, errors.New("Unknown langage")
	}

	return pattern, nil
}
