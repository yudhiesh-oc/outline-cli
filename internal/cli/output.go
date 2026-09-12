package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func emit(w io.Writer, cmd command, raw bool, data json.RawMessage) error {
	data = bytes.TrimSpace(data)
	if !raw && cmd != commandAPI {
		var err error
		data, err = summarize(cmd, data)
		if err != nil {
			return fmt.Errorf("formatting response: %w", err)
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return fmt.Errorf("invalid JSON response: %w", err)
	}
	compact.WriteByte('\n')
	if n, err := w.Write(compact.Bytes()); err != nil || n != compact.Len() {
		if err == nil {
			err = io.ErrShortWrite
		}
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

func summarize(cmd command, data json.RawMessage) (json.RawMessage, error) {
	if cmd == commandTree || string(data) == "null" {
		return data, nil
	}
	if len(data) > 0 && data[0] == '[' {
		return summarizeArray(cmd, data)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	switch cmd {
	case commandMove:
		return summarizeMove(object)
	case commandSearch:
		return summarizeSearch(object)
	case commandComments, commandComment:
		return summarizeComments(object)
	}
	return json.Marshal(selectFields(object, cmd.projection()...))
}

// summarizeArray projects every element of a JSON array with the same rules,
// preserving the array shape and erroring on the first failing element.
func summarizeArray(cmd command, data json.RawMessage) (json.RawMessage, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	for i, item := range items {
		var err error
		items[i], err = summarize(cmd, item)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(items)
}

// summarizeMove shapes a move receipt: the API nests affected documents under
// "documents"; they are projected with list rules and re-wrapped unchanged.
func summarizeMove(object map[string]json.RawMessage) (json.RawMessage, error) {
	documents, ok := object["documents"]
	if !ok {
		return nil, fmt.Errorf("move response is missing documents")
	}
	documents, err := summarize(commandList, documents)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]json.RawMessage{"documents": documents})
}

// summarizeSearch shapes one discovery hit: the nested document is projected
// with list rules while the context and ranking siblings are kept verbatim.
func summarizeSearch(object map[string]json.RawMessage) (json.RawMessage, error) {
	document, ok := object["document"]
	if !ok {
		return nil, fmt.Errorf("search hit is missing document")
	}
	document, err := summarize(commandList, document)
	if err != nil {
		return nil, err
	}
	result := selectFields(object, "context", "ranking")
	result["document"] = document
	return json.Marshal(result)
}

// summarizeComments projects the users embedded in createdBy/resolvedBy with
// user rules, leaving absent or null users untouched.
func summarizeComments(object map[string]json.RawMessage) (json.RawMessage, error) {
	result := selectFields(object, commentFields...)
	// Preserve editor JSON when the REST presenter omits computed markdown.
	if _, ok := object["text"]; !ok {
		if data, ok := object["data"]; ok {
			result["data"] = data
		}
	}
	for _, field := range []string{"createdBy", "resolvedBy"} {
		if user, ok := result[field]; ok && string(user) != "null" {
			var err error
			result[field], err = summarize(commandUsers, user)
			if err != nil {
				return nil, err
			}
		}
	}
	return json.Marshal(result)
}

func selectFields(object map[string]json.RawMessage, fields ...string) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(fields))
	for _, field := range fields {
		if value, ok := object[field]; ok {
			result[field] = value
		}
	}
	return result
}
