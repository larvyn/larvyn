package agentsetup

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// Agents' settings files are the user's: an edit must keep their keys in
// the order they wrote them, which a Go map would not. node is a JSON value
// that does.
type node struct {
	// obj and arr are set for an object or an array; raw holds any other
	// value exactly as it was written.
	obj *object
	arr []*node
	raw json.RawMessage
}

type object struct {
	keys []string
	vals map[string]*node
}

func newObject() *node { return &node{obj: &object{vals: map[string]*node{}}} }

func (o *object) get(k string) *node { return o.vals[k] }

func (o *object) set(k string, v *node) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *object) del(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, x := range o.keys {
		if x == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

func parse(b []byte) (*node, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return newObject(), nil
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	n, err := decode(d)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err == nil {
		return nil, errors.New("more than one JSON value")
	}
	return n, nil
}

func decode(d *json.Decoder) (*node, error) {
	tok, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n := newObject()
			for d.More() {
				kt, err := d.Token()
				if err != nil {
					return nil, err
				}
				v, err := decode(d)
				if err != nil {
					return nil, err
				}
				n.obj.set(kt.(string), v)
			}
			_, err := d.Token()
			return n, err
		case '[':
			n := &node{arr: []*node{}}
			for d.More() {
				v, err := decode(d)
				if err != nil {
					return nil, err
				}
				n.arr = append(n.arr, v)
			}
			_, err := d.Token()
			return n, err
		}
	}
	raw, err := marshal(tok)
	return &node{raw: raw}, err
}

// value makes a node from a Go value (for what Larvyn adds).
func value(v any) *node {
	b, _ := marshal(v)
	n, _ := parse(b)
	return n
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// format writes a node with two-space indents, as the agents write their
// own files.
func format(n *node) []byte {
	var b strings.Builder
	write(&b, n, "")
	b.WriteString("\n")
	return []byte(b.String())
}

func write(b *strings.Builder, n *node, indent string) {
	switch {
	case n.obj != nil:
		if len(n.obj.keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, k := range n.obj.keys {
			key, _ := marshal(k)
			b.WriteString(indent + "  " + string(key) + ": ")
			write(b, n.obj.vals[k], indent+"  ")
			if i < len(n.obj.keys)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(indent + "}")
	case n.arr != nil:
		if len(n.arr) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, v := range n.arr {
			b.WriteString(indent + "  ")
			write(b, v, indent+"  ")
			if i < len(n.arr)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(indent + "]")
	default:
		b.Write(n.raw)
	}
}

// str is a string node's value, "" for any other.
func (n *node) str() string {
	if n == nil || n.raw == nil {
		return ""
	}
	var s string
	_ = json.Unmarshal(n.raw, &s)
	return s
}
