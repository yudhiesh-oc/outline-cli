package cli

import (
	"fmt"
	"strings"
)

// exitCode is the process exit status contract of Run.
type exitCode int

const (
	exitSuccess exitCode = 0
	exitRuntime exitCode = 1
	exitUsage   exitCode = 2
)

// urfaveHelpExit is the urfave exit code for an unknown help topic; Run
// reports it as exitUsage.
const urfaveHelpExit = 3

// editMode is the documents.update edit-mode enum.
type editMode string

const (
	editAppend editMode = "append"
	editPatch  editMode = "patch"
)

// String returns the wire value accepted by documents.update.
func (m editMode) String() string { return string(m) }

// sortField is the documents.list sort enum.
type sortField string

const (
	sortUpdatedAt sortField = "updatedAt"
	sortCreatedAt sortField = "createdAt"
	sortTitle     sortField = "title"
	sortIndex     sortField = "index"
)

// String returns the wire value accepted by documents.list.
func (f sortField) String() string { return string(f) }

// parseSortField validates a nonempty --sort value.
func parseSortField(value string) (sortField, error) {
	switch field := sortField(value); field {
	case sortUpdatedAt, sortCreatedAt, sortTitle, sortIndex:
		return field, nil
	}
	return "", fmt.Errorf("--sort must be updatedAt, createdAt, title, or index")
}

// sortDirection is the documents.list direction enum; Outline treats only the
// exact uppercase value as ascending.
type sortDirection string

const (
	sortAscending  sortDirection = "ASC"
	sortDescending sortDirection = "DESC"
)

// String returns the wire value accepted by documents.list.
func (d sortDirection) String() string { return string(d) }

// parseSortDirection accepts asc or desc in any case.
func parseSortDirection(value string) (sortDirection, error) {
	switch strings.ToLower(value) {
	case "asc":
		return sortAscending, nil
	case "desc":
		return sortDescending, nil
	}
	return "", fmt.Errorf("--direction must be asc or desc")
}
