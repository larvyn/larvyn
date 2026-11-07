package projectfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"larvyn/internal/safefile"
)

// EditCommands changes the commands in the project's file, creating it when
// there is none. fn gets the commands that loaded and returns the new list.
// Everything else in the file is kept as it was, entries Larvyn skipped
// included, and so is the order of its fields. A file that doesn't parse is
// not touched: fixing it is up to the user.
func EditCommands(root string, fn func([]Command) ([]Command, error)) error {
	path := Path(root)
	fields := []field{{"apiVersion", json.RawMessage(fmt.Sprint(APIVersion))}}
	var raws []json.RawMessage
	b, err := safefile.ReadRegularIn(root, filepath.Join(Dir, Name), maxSize)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	default:
		if _, _, err := Parse(b); err != nil {
			return fmt.Errorf("fix %s first: %w", Rel, err)
		}
		if fields, err = readFields(b); err != nil {
			return err
		}
		if raw := get(fields, "commands"); raw != nil && string(raw) != "null" {
			if err := json.Unmarshal(raw, &raws); err != nil {
				return fmt.Errorf("%s: commands must be a list", Rel)
			}
		}
	}

	// The commands that load, and where each one is in the list.
	var current []Command
	at := map[string]int{}
	seen := map[string]bool{}
	for i, raw := range raws {
		var c Command
		if strict(raw, &c) == nil && checkCommand(c, seen) == nil {
			seen[c.ID] = true
			at[c.ID] = i
			current = append(current, c)
		}
	}
	next, err := fn(append([]Command(nil), current...))
	if err != nil {
		return err
	}
	seen = map[string]bool{}
	for _, c := range next {
		if err := checkCommand(c, seen); err != nil {
			return err
		}
		seen[c.ID] = true
	}

	// Rebuild the list: skipped entries stay where they were, kept commands
	// are updated in place, removed ones go, new ones are added at the end.
	keep := map[string]Command{}
	for _, c := range next {
		keep[c.ID] = c
	}
	var out []json.RawMessage
	for i, raw := range raws {
		var c Command
		if strict(raw, &c) != nil {
			out = append(out, raw)
			continue
		}
		if j, loaded := at[c.ID]; !loaded || j != i {
			out = append(out, raw) // skipped when loading: left as it is
			continue
		}
		nc, ok := keep[c.ID]
		if !ok {
			continue // removed
		}
		enc, _ := json.Marshal(nc)
		out = append(out, enc)
		delete(keep, c.ID)
	}
	for _, c := range next {
		if _, ok := keep[c.ID]; ok {
			enc, _ := json.Marshal(c)
			out = append(out, enc)
		}
	}
	list, _ := json.Marshal(out)
	if len(out) == 0 {
		fields = without(fields, "commands")
	} else {
		fields = set(fields, "commands", list)
	}
	if err := write(path, fields); err != nil {
		return err
	}
	Forget(root)
	return nil
}

// field is one top-level key of the file, in the order it was written.
type field struct {
	key string
	raw json.RawMessage
}

func readFields(b []byte) ([]field, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("%s must hold a JSON object", Rel)
	}
	var out []field
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		out = set(out, key, raw)
	}
	return out, nil
}

func get(fields []field, key string) json.RawMessage {
	for _, f := range fields {
		if f.key == key {
			return f.raw
		}
	}
	return nil
}

func set(fields []field, key string, raw json.RawMessage) []field {
	for i := range fields {
		if fields[i].key == key {
			fields[i].raw = raw
			return fields
		}
	}
	return append(fields, field{key, raw})
}

func without(fields []field, key string) []field {
	out := fields[:0:0]
	for _, f := range fields {
		if f.key != key {
			out = append(out, f)
		}
	}
	return out
}

// write saves the fields as an indented JSON object, through a temp file so
// a reader never sees half of it.
func write(path string, fields []field) error {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(f.key)
		buf.Write(k)
		buf.WriteByte(':')
		if err := json.Compact(&buf, f.raw); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	var out bytes.Buffer
	if err := json.Indent(&out, buf.Bytes(), "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, Name+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(out.Bytes()); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// A file committed to git is usually world-readable; CreateTemp makes it 0600.
	_ = os.Chmod(f.Name(), 0o644)
	return os.Rename(f.Name(), path)
}
