// Package cli implements the outline command-line interface.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	urfave "github.com/urfave/cli/v3"

	"github.com/yudhiesh-oc/outline-cli/internal/outline"
)

var version = "dev"

// uuidRe validates the identifiers accepted for collections and documents.
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Page-size bounds for list and search pagination.
const (
	defaultPageSize = 25
	maxPageSize     = 100
)

// Run executes one outline command and returns the process exit code:
// exitSuccess, exitRuntime, or exitUsage.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	cmd := newCommand(stdout, stderr)
	if err := cmd.Run(ctx, append([]string{"outline"}, args...)); err != nil {
		if err.Error() != "" {
			fmt.Fprintf(stderr, "outline: %v\n", err)
		}
		var exit urfave.ExitCoder
		if errors.As(err, &exit) && (exit.ExitCode() == int(exitUsage) || exit.ExitCode() == urfaveHelpExit) {
			return int(exitUsage)
		}
		return int(exitRuntime)
	}
	return int(exitSuccess)
}

func newCommand(stdout, stderr io.Writer) *urfave.Command {
	cmd := &urfave.Command{
		Name:    "outline",
		Version: version,
		Usage:   "Search and manage an Outline workspace; summary JSON on stdout",
		Description: fmt.Sprintf("Flags may appear before or after arguments; -- ends option parsing.\n"+
			"Lists return one page by default; --all fails if the %d-page safety limit is reached.\n"+
			"Configuration: OUTLINE_URL + OUTLINE_API_KEY, or OUTLINE_CONFIG (defaults to the OS config directory).", outline.MaxPages),
		Writer:    stdout,
		ErrWriter: stderr,
		Flags: []urfave.Flag{
			&urfave.BoolFlag{Name: "raw", Usage: "Return full REST data instead of summaries (api is always raw)"},
		},
		OnUsageError: onUsageError,
		// Run owns error reporting and exit codes; never let the library exit the process.
		ExitErrHandler: func(context.Context, *urfave.Command, error) {},
		Action: func(_ context.Context, c *urfave.Command) error {
			if c.NArg() > 0 {
				return usageError("unknown command %q", c.Args().First())
			}
			c.Writer = stderr
			defer func() { c.Writer = stdout }()
			if err := urfave.ShowRootCommandHelp(c); err != nil {
				return err
			}
			return urfave.Exit("", 2)
		},
		Commands: []*urfave.Command{
			{
				Name: commandSearch.String(), Usage: "Full-text search; hits contain document metadata and context", ArgsUsage: "<query>",
				Before: argumentCount(1, 1),
				Flags:  append(pagingFlags(), uuidFlag("collection", "Collection UUID to search")),
				Action: cmdSearch,
			},
			{
				Name: commandGet.String(), Usage: "Read one document including markdown text", ArgsUsage: "<id-or-urlId>",
				Before: argumentCount(1, 1),
				Action: func(ctx context.Context, c *urfave.Command) error {
					return request(ctx, c, commandGet, outline.MethodDocumentsInfo, map[string]any{"id": c.Args().First()}, false)
				},
			},
			{
				Name: commandList.String(), Usage: "List document metadata; use search for full-text queries", ArgsUsage: " ",
				Before: argumentCount(0, 0),
				Flags: append(pagingFlags(),
					uuidFlag("collection", "Collection UUID"), uuidFlag("parent", "Parent document UUID"),
					&urfave.StringFlag{Name: "sort", Usage: "updatedAt|createdAt|title|index"},
					&urfave.StringFlag{Name: "direction", Usage: "asc|desc"},
				),
				Action: cmdList,
			},
			{
				Name: commandCreate.String(), Usage: "Create and publish a document; --publish=false creates a draft", ArgsUsage: "<title>",
				Before: argumentCount(1, 1),
				Flags: []urfave.Flag{
					uuidFlag("collection", "Collection UUID (required to publish)"), uuidFlag("parent", "Parent document UUID"),
					&urfave.StringFlag{Name: "text", Usage: "Markdown body"},
					&urfave.StringFlag{Name: "template", Usage: "Template ID to pre-fill from"},
					&urfave.BoolFlag{Name: "publish", Value: true, Usage: "Publish immediately"},
				},
				Action: cmdCreate,
			},
			{
				Name: commandUpdate.String(), Usage: "Edit a document; prefer patch to preserve rich formatting", ArgsUsage: "<id>",
				Before: argumentCount(1, 1),
				Flags: []urfave.Flag{
					&urfave.StringFlag{Name: "title", Usage: "New title"},
					&urfave.StringFlag{Name: "text", Usage: "Markdown body; replaces all content unless --append or --patch is set"},
					&urfave.StringFlag{Name: "find", Usage: "Exact markdown to replace in patch mode"},
					uuidFlag("collection", "Collection to publish a draft into; use move to relocate"),
					&urfave.BoolFlag{Name: "publish", Usage: "Publish a draft"},
					&urfave.BoolFlag{Name: "append", Usage: "Append instead of replacing"},
					&urfave.BoolFlag{Name: "patch", Usage: "Replace the first match of --find"},
				},
				Action: cmdUpdate,
			},
			{
				Name: commandMove.String(), Usage: "Move or reorder a document; requires --collection or --parent", ArgsUsage: "<id>",
				Before: argumentCount(1, 1),
				Flags: []urfave.Flag{
					uuidFlag("collection", "Target collection UUID"), uuidFlag("parent", "New parent document UUID"),
					&urfave.IntFlag{Name: "index", Usage: "Nonnegative sibling position", Validator: nonnegative},
				},
				Action: cmdMove,
			},
			{
				Name: commandArchive.String(), Usage: "Archive a document (recoverable)", ArgsUsage: "<id>",
				Before: argumentCount(1, 1),
				Action: func(ctx context.Context, c *urfave.Command) error {
					return request(ctx, c, commandArchive, outline.MethodDocumentsArchive, map[string]any{"id": c.Args().First()}, false)
				},
			},
			{
				Name: commandRestore.String(), Usage: "Restore an archived or trashed document", ArgsUsage: "<id>",
				Before: argumentCount(1, 1),
				Action: func(ctx context.Context, c *urfave.Command) error {
					return request(ctx, c, commandRestore, outline.MethodDocumentsRestore, map[string]any{"id": c.Args().First()}, false)
				},
			},
			{
				Name: commandDelete.String(), Usage: "Move a document to trash; --permanent cannot be undone", ArgsUsage: "<id>",
				Before: argumentCount(1, 1),
				Flags:  []urfave.Flag{&urfave.BoolFlag{Name: "permanent", Usage: "Destroy permanently (no undo)"}},
				Action: func(ctx context.Context, c *urfave.Command) error {
					body := map[string]any{"id": c.Args().First()}
					if c.Bool("permanent") {
						body["permanent"] = true
					}
					return request(ctx, c, commandDelete, outline.MethodDocumentsDelete, body, false)
				},
			},
			resourceCommand(commandCollections, outline.MethodCollectionsList, "List collections", "[name-filter]", "query", false),
			{
				Name: commandTree.String(), Usage: "Read a collection's complete published document hierarchy", ArgsUsage: "<collectionId>",
				Before: argumentCount(1, 1),
				Action: func(ctx context.Context, c *urfave.Command) error {
					return request(ctx, c, commandTree, outline.MethodCollectionsDocuments, map[string]any{"id": c.Args().First()}, false)
				},
			},
			resourceCommand(commandComments, outline.MethodCommentsList, "List document comments", "<documentId>", "documentId", true),
			{
				Name: commandComment.String(), Usage: "Add a document comment", ArgsUsage: "<documentId> <text>",
				Before: argumentCount(2, 2),
				Action: func(ctx context.Context, c *urfave.Command) error {
					return request(ctx, c, commandComment, outline.MethodCommentsCreate, map[string]any{"documentId": c.Args().Get(0), "text": c.Args().Get(1)}, false)
				},
			},
			resourceCommand(commandUsers, outline.MethodUsersList, "List workspace users", "[name-or-email-filter]", "query", false),
			resourceCommand(commandTemplates, outline.MethodTemplatesList, "List template metadata", "[title-filter]", "query", false),
			{
				Name: commandAPI.String(), Usage: "Call any JSON REST method; output is always unfiltered", ArgsUsage: "<domain.action>",
				Before: argumentCount(1, 1),
				Flags:  []urfave.Flag{&urfave.StringFlag{Name: "data", Usage: "JSON request object"}},
				Action: cmdAPI,
			},
			{
				Name: commandSkill.String(), Usage: "Install the bundled Outline agent skill into an LLM harness",
				Description: fmt.Sprintf("Harnesses and their skill directories:\n   %s", strings.Join(harnessNames(), ", ")),
				Commands: []*urfave.Command{
					{
						Name: commandSkillInstall.String(), Usage: "Write the outline skill into the harness skills directory", ArgsUsage: "<harness>",
						Before: argumentCount(1, 1),
						Flags:  []urfave.Flag{&urfave.BoolFlag{Name: "force", Usage: "Overwrite an existing skill file"}},
						Action: cmdSkillInstall,
					},
				},
			},
		},
	}
	for _, subcommand := range cmd.Commands {
		prepareCommand(subcommand)
	}
	return cmd
}

// prepareCommand applies the leaf-command conventions to a command and its descendants.
func prepareCommand(c *urfave.Command) {
	c.OnUsageError = onUsageError
	// Leaf commands take document text as positionals, including the word "help".
	c.HideHelpCommand = true
	for _, child := range c.Commands {
		prepareCommand(child)
	}
}

func onUsageError(_ context.Context, _ *urfave.Command, err error, _ bool) error {
	return urfave.Exit(err, int(exitUsage))
}

func usageError(format string, args ...any) error {
	return urfave.Exit(fmt.Sprintf(format, args...), int(exitUsage))
}

func argumentCount(minimum, maximum int) urfave.BeforeFunc {
	return func(ctx context.Context, c *urfave.Command) (context.Context, error) {
		if c.NArg() < minimum || c.NArg() > maximum {
			return ctx, usageError("usage: %s %s [options]", c.FullName(), strings.TrimSpace(c.ArgsUsage))
		}
		return ctx, nil
	}
}

func uuidFlag(name, usage string) *urfave.StringFlag {
	return &urfave.StringFlag{Name: name, Usage: usage, Validator: func(value string) error {
		if value != "" && !uuidRe.MatchString(value) {
			return usageError("--%s must be a UUID; use IDs returned by outline collections or outline get", name)
		}
		return nil
	}}
}

func nonnegative(value int) error {
	if value < 0 {
		return usageError("value must be nonnegative")
	}
	return nil
}

func pagingFlags() []urfave.Flag {
	return []urfave.Flag{
		&urfave.BoolFlag{Name: "all", Usage: fmt.Sprintf("Fetch all pages (up to %d; errors if incomplete)", outline.MaxPages)},
		&urfave.IntFlag{Name: "limit", Value: defaultPageSize, Usage: fmt.Sprintf("Page size (1-%d)", maxPageSize), Validator: func(value int) error {
			if value < 1 || value > maxPageSize {
				return usageError("--limit must be 1-%d", maxPageSize)
			}
			return nil
		}},
		&urfave.IntFlag{Name: "offset", Usage: "Skip first N results", Validator: nonnegative},
	}
}

func pageBody(c *urfave.Command) map[string]any {
	return map[string]any{"limit": c.Int("limit"), "offset": c.Int("offset")}
}

func resourceCommand(cmd command, method outline.Method, usage, argsUsage, key string, required bool) *urfave.Command {
	minimum := 0
	if required {
		minimum = 1
	}
	return &urfave.Command{
		Name: cmd.String(), Usage: usage, ArgsUsage: argsUsage,
		Before: argumentCount(minimum, 1), Flags: pagingFlags(),
		Action: func(ctx context.Context, c *urfave.Command) error {
			body := pageBody(c)
			if c.NArg() == 1 {
				body[key] = c.Args().First()
			}
			return request(ctx, c, cmd, method, body, c.Bool("all"))
		},
	}
}

func cmdSearch(ctx context.Context, c *urfave.Command) error {
	body := pageBody(c)
	body["query"] = c.Args().First()
	putIf(body, "collectionId", c.String("collection"))
	return request(ctx, c, commandSearch, outline.MethodDocumentsSearch, body, c.Bool("all"))
}

func cmdList(ctx context.Context, c *urfave.Command) error {
	body := pageBody(c)
	putIf(body, "collectionId", c.String("collection"))
	putIf(body, "parentDocumentId", c.String("parent"))
	if value := c.String("sort"); value != "" {
		field, err := parseSortField(value)
		if err != nil {
			return usageError("%v", err)
		}
		body["sort"] = field.String()
	}
	if value := c.String("direction"); value != "" {
		direction, err := parseSortDirection(value)
		if err != nil {
			return usageError("%v", err)
		}
		body["direction"] = direction.String()
	}
	return request(ctx, c, commandList, outline.MethodDocumentsList, body, c.Bool("all"))
}

func cmdCreate(ctx context.Context, c *urfave.Command) error {
	body := map[string]any{"title": c.Args().First(), "publish": c.Bool("publish")}
	putIf(body, "collectionId", c.String("collection"))
	putIf(body, "parentDocumentId", c.String("parent"))
	putIf(body, "templateId", c.String("template"))
	if c.IsSet("text") {
		body["text"] = c.String("text")
	}
	return request(ctx, c, commandCreate, outline.MethodDocumentsCreate, body, false)
}

// validateUpdateText checks edit-mode constraints before publishing constraints.
func validateUpdateText(c *urfave.Command) error {
	patch, appendMode := c.Bool("patch"), c.Bool("append")
	if patch != (c.String("find") != "") {
		if patch {
			return usageError("--patch requires --find TEXT")
		}
		return usageError("--find requires --patch")
	}
	if appendMode && patch {
		return usageError("--append and --patch are mutually exclusive")
	}
	if (appendMode || patch) && !c.IsSet("text") {
		return usageError("--append and --patch require --text (empty text deletes a patch match)")
	}
	if appendMode && c.String("text") == "" {
		return usageError("--append requires nonempty --text")
	}
	return nil
}

func validateUpdate(c *urfave.Command) error {
	if err := validateUpdateText(c); err != nil {
		return err
	}
	if c.IsSet("publish") && !c.Bool("publish") {
		return usageError("--publish=false cannot unpublish via documents.update; use outline api documents.unpublish --data '{\"id\":\"...\"}'")
	}
	if c.String("collection") != "" && !c.Bool("publish") {
		return usageError("--collection requires --publish here; use outline move to relocate a document")
	}
	if !c.IsSet("title") && !c.IsSet("text") && !c.Bool("publish") {
		return usageError("update requires --title, --text, or --publish")
	}
	return nil
}

func cmdUpdate(ctx context.Context, c *urfave.Command) error {
	if err := validateUpdate(c); err != nil {
		return err
	}

	body := map[string]any{"id": c.Args().First()}
	for _, field := range []string{"title", "text"} {
		if c.IsSet(field) {
			body[field] = c.String(field)
		}
	}
	putIf(body, "findText", c.String("find"))
	putIf(body, "collectionId", c.String("collection"))
	if c.Bool("append") {
		body["editMode"] = editAppend.String()
	}
	if c.Bool("patch") {
		body["editMode"] = editPatch.String()
	}
	if c.Bool("publish") {
		body["publish"] = true
	}
	return request(ctx, c, commandUpdate, outline.MethodDocumentsUpdate, body, false)
}

func cmdMove(ctx context.Context, c *urfave.Command) error {
	if c.String("collection") == "" && c.String("parent") == "" {
		return usageError("move requires --collection or --parent")
	}
	body := map[string]any{"id": c.Args().First()}
	putIf(body, "collectionId", c.String("collection"))
	putIf(body, "parentDocumentId", c.String("parent"))
	if c.IsSet("index") {
		body["index"] = c.Int("index")
	}
	return request(ctx, c, commandMove, outline.MethodDocumentsMove, body, false)
}

func cmdAPI(ctx context.Context, c *urfave.Command) error {
	method, err := outline.ParseMethod(c.Args().First())
	if err != nil {
		return usageError("%v", err)
	}
	body := json.RawMessage("{}")
	if value := c.String("data"); value != "" {
		body = json.RawMessage(value)
		var object map[string]json.RawMessage
		if err := json.Unmarshal(body, &object); err != nil || object == nil {
			return usageError("--data must be a JSON object")
		}
	}
	return request(ctx, c, commandAPI, method, body, false)
}

func request(ctx context.Context, c *urfave.Command, cmd command, method outline.Method, body any, all bool) error {
	client, err := outline.FromEnv()
	if err != nil {
		return err
	}
	var data json.RawMessage
	if all {
		data, err = client.Paged(ctx, method, body.(map[string]any))
	} else {
		data, err = client.Do(ctx, method, body)
	}
	if err != nil {
		return err
	}
	return emit(c.Root().Writer, cmd, c.Bool("raw"), data)
}

func putIf(m map[string]any, key, value string) {
	if value != "" {
		m[key] = value
	}
}
