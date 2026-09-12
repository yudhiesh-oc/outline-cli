package cli

// command identifies one CLI subcommand: the name users type, the key that
// selects its summary projection, and the label used in usage errors.
type command string

const (
	commandSearch      command = "search"
	commandGet         command = "get"
	commandList        command = "list"
	commandCreate      command = "create"
	commandUpdate      command = "update"
	commandMove        command = "move"
	commandArchive     command = "archive"
	commandRestore     command = "restore"
	commandDelete      command = "delete"
	commandCollections command = "collections"
	commandTree        command = "tree"
	commandComments    command = "comments"
	commandComment     command = "comment"
	commandUsers       command = "users"
	commandTemplates   command = "templates"
	commandAPI         command = "api"
)

// String returns the subcommand name.
func (c command) String() string { return string(c) }

// Summary projection fields per command, in output order.
var (
	getFields = []string{"id", "title", "url", "text", "collectionId", "parentDocumentId", "updatedAt", "revision", "revisionCount", "publishedAt", "archivedAt", "deletedAt"}

	listFields = []string{"id", "title", "url", "collectionId", "parentDocumentId", "updatedAt", "publishedAt", "archivedAt"}

	collectionsFields = []string{"id", "name", "url", "description", "permission", "documentsCount"}

	usersFields = []string{"id", "name", "email", "role", "isSuspended"}

	templatesFields = []string{"id", "title", "collectionId", "updatedAt"}

	commentFields = []string{"id", "documentId", "parentCommentId", "text", "createdBy", "createdAt", "updatedAt", "resolvedAt", "resolvedBy"}

	defaultFields = []string{"id", "title", "url", "success", "updatedAt", "revision", "revisionCount", "publishedAt", "archivedAt", "deletedAt"}
)

// projection returns the summary fields for a command; mutations and every
// command without a dedicated shape fall back to defaultFields.
func (c command) projection() []string {
	switch c {
	case commandGet:
		return getFields
	case commandList:
		return listFields
	case commandCollections:
		return collectionsFields
	case commandUsers:
		return usersFields
	case commandTemplates:
		return templatesFields
	default:
		return defaultFields
	}
}
