// Package dotenv reads Laravel .env files.
package dotenv

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Read parses a dotenv file; nil when it does not exist.
func Read(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := ParseLine(sc.Text())
		if ok {
			out[k] = v
		}
	}
	return out
}

// ParseLine parses one KEY=value line: quotes are removed, comments and
// "export " are ignored. ok is false for blank and comment lines.
func ParseLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	k, v, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	k = strings.TrimSpace(k)
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		v = v[1 : len(v)-1]
	} else if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return k, v, true
}

// Set writes values into the dotenv file at path: each key's existing line
// is replaced (every occurrence), and a key that is missing is added at the
// end. Other lines, comments and order stay as they are. The file is
// replaced atomically and keeps its mode. It returns the previous contents,
// for a backup.
func Set(path string, values map[string]string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	done := map[string]bool{}
	for i, line := range lines {
		if k, _, ok := ParseLine(line); ok {
			if v, set := values[k]; set {
				lines[i] = k + "=" + Quote(v)
				done[k] = true
			}
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		if !done[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > 0 && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for _, k := range keys {
		lines = append(lines, k+"="+Quote(values[k]))
	}
	if len(keys) > 0 {
		lines = append(lines, "")
	}
	return raw, writeFile(path, []byte(strings.Join(lines, "\n")))
}

// Quote returns v as a dotenv value: bare when it is safe, otherwise in
// quotes. A value with $ goes in single quotes, where phpdotenv never
// expands ${NAME}; in double quotes it would, so a value could pull another
// key (a secret) into this one. Callers must refuse values with both $ and
// a single quote, which no quoting keeps literal.
func Quote(v string) string {
	if strings.Contains(v, "$") && !strings.ContainsAny(v, "'\n\r") {
		return "'" + v + "'"
	}
	if v == "" || strings.ContainsAny(v, " \t#\"'$\\=") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
	}
	return v
}

func writeFile(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".larvyn-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
