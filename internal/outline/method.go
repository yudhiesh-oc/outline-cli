package outline

import (
	"errors"
	"fmt"
	"regexp"
)

// Method names one Outline REST API method; every call posts to
// {base}/api/<method>. Constants cover the methods this CLI uses, and
// ParseMethod validates a runtime value such as `outline api domain.action`.
type Method string

const (
	MethodDocumentsInfo    Method = "documents.info"
	MethodDocumentsSearch  Method = "documents.search"
	MethodDocumentsList    Method = "documents.list"
	MethodDocumentsCreate  Method = "documents.create"
	MethodDocumentsUpdate  Method = "documents.update"
	MethodDocumentsMove    Method = "documents.move"
	MethodDocumentsArchive Method = "documents.archive"
	MethodDocumentsRestore Method = "documents.restore"
	MethodDocumentsDelete  Method = "documents.delete"

	MethodCollectionsList      Method = "collections.list"
	MethodCollectionsDocuments Method = "collections.documents"
	MethodCommentsList         Method = "comments.list"
	MethodCommentsCreate       Method = "comments.create"
	MethodUsersList            Method = "users.list"
	MethodTemplatesList        Method = "templates.list"
)

// ErrInvalidMethod reports a method name that is not domain.action.
var ErrInvalidMethod = errors.New("invalid API method")

var methodPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*\.[A-Za-z][A-Za-z0-9_]*$`)

// ParseMethod validates an externally supplied method name.
func ParseMethod(value string) (Method, error) {
	method := Method(value)
	if err := method.validate(); err != nil {
		return "", err
	}
	return method, nil
}

// String returns the wire form used in the request path.
func (m Method) String() string { return string(m) }

// validate rejects names that would escape the /api/<method> request path.
func (m Method) validate() error {
	if !methodPattern.MatchString(string(m)) {
		return fmt.Errorf("%w %q: expected domain.action", ErrInvalidMethod, m)
	}
	return nil
}
