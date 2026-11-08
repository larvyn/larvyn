package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
)

// Human turns a result into text: a string as it is, a list of objects as
// a table, an object as "key: value" lines, in the order the app sent the
// fields.
func Human(raw json.RawMessage) string {
	v, err := decodeOrdered(raw)
	if err != nil {
		return string(raw) + "\n"
	}
	var b strings.Builder
	switch t := v.(type) {
	case nil:
	case string:
		b.WriteString(t)
		if t != "" && !strings.HasSuffix(t, "\n") {
			b.WriteByte('\n')
		}
	case []any:
		writeList(&b, t)
	case *object:
		writeObject(&b, t)
	default:
		b.WriteString(scalar(t) + "\n")
	}
	return b.String()
}

func writeList(b *strings.Builder, list []any) {
	if len(list) == 0 {
		b.WriteString("(none)\n")
		return
	}
	first, ok := list[0].(*object)
	if !ok {
		for _, v := range list {
			b.WriteString(scalar(v) + "\n")
		}
		return
	}
	w := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, strings.ToUpper(strings.Join(first.keys, "\t")))
	for _, v := range list {
		o, _ := v.(*object)
		if o == nil {
			continue
		}
		cells := make([]string, len(first.keys))
		for i, k := range first.keys {
			cells[i] = oneLine(scalar(o.vals[k]))
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	w.Flush()
}

func writeObject(b *strings.Builder, o *object) {
	w := tabwriter.NewWriter(b, 0, 0, 1, ' ', 0)
	for _, k := range o.keys {
		v := o.vals[k]
		if v == nil || v == "" {
			continue
		}
		if list, ok := v.([]any); ok {
			if len(list) == 0 {
				continue
			}
			if _, objs := list[0].(*object); objs {
				w.Flush()
				fmt.Fprintf(b, "%s:\n", k)
				var sub strings.Builder
				writeList(&sub, list)
				for _, l := range strings.Split(strings.TrimRight(sub.String(), "\n"), "\n") {
					b.WriteString("  " + l + "\n")
				}
				continue
			}
		}
		fmt.Fprintf(w, "%s:\t%s\n", k, scalar(v))
	}
	w.Flush()
}

func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "yes"
		}
		return "no"
	case json.Number:
		return t.String()
	case []any:
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = scalar(x)
		}
		return strings.Join(parts, ", ")
	case *object:
		parts := make([]string, 0, len(t.keys))
		for _, k := range t.keys {
			parts = append(parts, k+"="+scalar(t.vals[k]))
		}
		return strings.Join(parts, " ")
	}
	return fmt.Sprint(v)
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > 60 {
		s = string(r[:59]) + "…"
	}
	return s
}

// object is a JSON object that keeps its keys in order.
type object struct {
	keys []string
	vals map[string]any
}

func decodeOrdered(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	return decodeValue(d)
}

func decodeValue(d *json.Decoder) (any, error) {
	tok, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := &object{vals: map[string]any{}}
			for d.More() {
				kt, err := d.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := decodeValue(d)
				if err != nil {
					return nil, err
				}
				o.keys = append(o.keys, k)
				o.vals[k] = v
			}
			_, err := d.Token()
			return o, err
		case '[':
			list := []any{}
			for d.More() {
				v, err := decodeValue(d)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			}
			_, err := d.Token()
			return list, err
		}
	}
	return tok, nil
}
