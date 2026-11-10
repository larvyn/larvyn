package agentapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"larvyn/internal/api"
	"larvyn/internal/laravel"
)

// The app's models and database through the API, read from the Laravel app
// the caller works in: an agent in a worktree sees its own .env and its own
// SQLite copy. Everything here only reads; to change data an agent runs
// artisan (migrations, seeders) or writes code.
func init() {
	api.Register("models", "List the app's Eloquent models with their table, relationships, fillable, casts and row count", apiModels,
		"name?: only the models whose name contains this")
	api.Register("db.tables", "List the database's tables with their row counts, as the app connects to it", apiDBTables)
	api.Register("db.table", "Print a table's columns, indexes and foreign keys", apiDBTable,
		"name: the table")
	api.Register("db.query", "Run a query that reads (select, with, show, describe, explain) and print its rows; it runs in a read-only transaction that is rolled back, so it can't change anything", apiDBQuery,
		"sql: the query",
		"limit?: how many rows to print, 100 by default, 1000 at most")
	api.MarkReadOnly("models", "db.tables", "db.table", "db.query")
}

// bridgeCall runs a bridge action in the caller's Laravel app and decodes
// its answer into out.
func bridgeCall(c api.Caller, action string, payload any, out any) error {
	p, dir, _, err := callerProject(c)
	if err != nil {
		return err
	}
	raw, err := laravel.BridgeIn(appRoot(p, dir), action, payload, 60*time.Second)
	if err != nil {
		return err
	}
	var head struct {
		OK    bool `json:"ok"`
		Error *struct {
			Class   string `json:"class"`
			Message string `json:"message"`
			File    string `json:"file"`
			Line    int    `json:"line"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return fmt.Errorf("the bridge answered: %v", err)
	}
	if !head.OK {
		if e := head.Error; e != nil {
			msg := e.Message
			if e.Class != "Larvyn" && e.Class != "" {
				msg = e.Class + ": " + msg
			}
			if e.File != "" {
				msg += fmt.Sprintf(" (%s:%d)", e.File, e.Line)
			}
			return errors.New(msg)
		}
		return errors.New("the bridge failed")
	}
	// UseNumber: a bigint row id past 2^53 would lose digits as a float64,
	// in the text and in --json.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}

// Model is an Eloquent model as the bridge reads it.
type Model struct {
	Class       string            `json:"class"`
	Name        string            `json:"name"`
	File        string            `json:"file"`
	Table       string            `json:"table,omitempty"`
	PrimaryKey  string            `json:"primaryKey,omitempty"`
	Fillable    []string          `json:"fillable,omitempty"`
	Guarded     []string          `json:"guarded,omitempty"`
	Hidden      []string          `json:"hidden,omitempty"`
	Casts       map[string]string `json:"casts,omitempty"`
	Appends     []string          `json:"appends,omitempty"`
	Timestamps  bool              `json:"timestamps"`
	SoftDeletes bool              `json:"softDeletes,omitempty"`
	Traits      []string          `json:"traits,omitempty"`
	Relations   []struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Related string `json:"related,omitempty"`
	} `json:"relations,omitempty"`
	Count *int   `json:"count"`
	Error string `json:"error,omitempty"`
}

// Models is what models prints.
type Models []Model

func (ms Models) Text() string {
	if len(ms) == 0 {
		return "No models in app/Models.\n"
	}
	var b strings.Builder
	for i, m := range ms {
		if i > 0 {
			b.WriteString("\n")
		}
		if m.Error != "" {
			fmt.Fprintf(&b, "%s: %s\n", m.Class, m.Error)
			continue
		}
		fmt.Fprintf(&b, "%s (%s)", m.Class, m.Table)
		if m.Count != nil {
			fmt.Fprintf(&b, " · %d rows", *m.Count)
		}
		if m.SoftDeletes {
			b.WriteString(" · soft deletes")
		}
		b.WriteString("\n")
		if len(m.Relations) > 0 {
			var rel []string
			for _, r := range m.Relations {
				s := r.Name + " " + r.Type
				if r.Related != "" {
					s += " " + shortClass(r.Related)
				}
				rel = append(rel, s)
			}
			fmt.Fprintf(&b, "  relations: %s\n", strings.Join(rel, ", "))
		}
		list := func(label string, xs []string) {
			if len(xs) > 0 {
				fmt.Fprintf(&b, "  %s: %s\n", label, strings.Join(xs, ", "))
			}
		}
		list("fillable", m.Fillable)
		if len(m.Fillable) == 0 && !(len(m.Guarded) == 1 && m.Guarded[0] == "*") {
			list("guarded", m.Guarded)
		}
		if len(m.Casts) > 0 {
			keys := make([]string, 0, len(m.Casts))
			for k := range m.Casts {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var casts []string
			for _, k := range keys {
				casts = append(casts, k+" "+m.Casts[k])
			}
			list("casts", casts)
		}
		list("hidden", m.Hidden)
		list("appends", m.Appends)
		list("traits", m.Traits)
	}
	return b.String()
}

func shortClass(c string) string {
	if i := strings.LastIndex(c, `\`); i >= 0 {
		return c[i+1:]
	}
	return c
}

func apiModels(c api.Caller, name string) (Models, error) {
	var res struct {
		Models Models `json:"models"`
	}
	if err := bridgeCall(c, "models", nil, &res); err != nil {
		return nil, err
	}
	out := Models{}
	for _, m := range res.Models {
		if name == "" || strings.Contains(strings.ToLower(m.Name), strings.ToLower(name)) {
			out = append(out, m)
		}
	}
	if name != "" && len(out) == 0 {
		return nil, fmt.Errorf("no model named like %q", name)
	}
	return out, nil
}

// DBTables is what db tables prints.
type DBTables struct {
	Driver   string `json:"driver"`
	Database string `json:"database"`
	Tables   []struct {
		Name string `json:"name"`
		Rows *int   `json:"rows"`
	} `json:"tables"`
}

func (d DBTables) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s · %d tables\n\n", d.Driver, d.Database, len(d.Tables))
	width := 0
	for _, t := range d.Tables {
		width = max(width, len(t.Name))
	}
	for _, t := range d.Tables {
		rows := "?"
		if t.Rows != nil {
			rows = fmt.Sprint(*t.Rows)
		}
		fmt.Fprintf(&b, "%-*s  %s\n", width, t.Name, rows)
	}
	return b.String()
}

func apiDBTables(c api.Caller) (DBTables, error) {
	var res DBTables
	err := bridgeCall(c, "db.overview", nil, &res)
	return res, err
}

// DBTable is what db table prints.
type DBTable struct {
	Name    string `json:"name"`
	Columns []struct {
		Name          string `json:"name"`
		Type          string `json:"type"`
		Nullable      bool   `json:"nullable"`
		Default       any    `json:"default"`
		AutoIncrement bool   `json:"auto_increment"`
	} `json:"columns"`
	Indexes []struct {
		Name    string   `json:"name"`
		Columns []string `json:"columns"`
		Unique  bool     `json:"unique"`
		Primary bool     `json:"primary"`
	} `json:"indexes"`
	ForeignKeys []struct {
		Columns        []string `json:"columns"`
		ForeignTable   string   `json:"foreign_table"`
		ForeignColumns []string `json:"foreign_columns"`
		OnDelete       string   `json:"on_delete"`
	} `json:"foreignKeys"`
}

func (t DBTable) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", t.Name)
	width := 0
	for _, c := range t.Columns {
		width = max(width, len(c.Name))
	}
	for _, c := range t.Columns {
		fmt.Fprintf(&b, "  %-*s  %s", width, c.Name, c.Type)
		if !c.Nullable {
			b.WriteString(" not null")
		}
		if c.AutoIncrement {
			b.WriteString(" auto_increment")
		}
		if c.Default != nil {
			fmt.Fprintf(&b, " default %v", c.Default)
		}
		b.WriteString("\n")
	}
	if len(t.Indexes) > 0 {
		b.WriteString("\nindexes:\n")
		for _, i := range t.Indexes {
			kind := ""
			switch {
			case i.Primary:
				kind = " primary"
			case i.Unique:
				kind = " unique"
			}
			fmt.Fprintf(&b, "  %s (%s)%s\n", i.Name, strings.Join(i.Columns, ", "), kind)
		}
	}
	if len(t.ForeignKeys) > 0 {
		b.WriteString("\nforeign keys:\n")
		for _, f := range t.ForeignKeys {
			fmt.Fprintf(&b, "  %s → %s(%s)", strings.Join(f.Columns, ", "), f.ForeignTable, strings.Join(f.ForeignColumns, ", "))
			if f.OnDelete != "" && !strings.EqualFold(f.OnDelete, "no action") {
				fmt.Fprintf(&b, " on delete %s", strings.ToLower(f.OnDelete))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func apiDBTable(c api.Caller, name string) (DBTable, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return DBTable{}, errors.New("which table? larvyn db tables lists them")
	}
	res := DBTable{Name: name}
	if err := bridgeCall(c, "db.table", map[string]any{"table": name}, &res); err != nil {
		return DBTable{}, err
	}
	if len(res.Columns) == 0 {
		return DBTable{}, fmt.Errorf("no table %q (larvyn db tables lists them)", name)
	}
	return res, nil
}

// DBRows is what db query prints.
type DBRows struct {
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
	Total   int              `json:"total"`
	MS      float64          `json:"ms"`
}

func (r DBRows) Text() string {
	if len(r.Rows) == 0 {
		return fmt.Sprintf("No rows (%.1f ms).\n", r.MS)
	}
	cells := make([][]string, len(r.Rows))
	width := make([]int, len(r.Columns))
	for i, c := range r.Columns {
		width[i] = len(c)
	}
	for i, row := range r.Rows {
		cells[i] = make([]string, len(r.Columns))
		for j, c := range r.Columns {
			s := "NULL"
			if v := row[c]; v != nil {
				s = fmt.Sprint(v)
			}
			s = strings.ReplaceAll(s, "\n", `\n`)
			if len([]rune(s)) > 60 {
				s = string([]rune(s)[:59]) + "…"
			}
			cells[i][j] = s
			width[j] = max(width[j], len([]rune(s)))
		}
	}
	var b strings.Builder
	line := func(vals []string) {
		for j, v := range vals {
			if j > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(v + strings.Repeat(" ", width[j]-len([]rune(v))))
		}
		b.WriteString("\n")
	}
	line(r.Columns)
	sep := make([]string, len(r.Columns))
	for j := range sep {
		sep[j] = strings.Repeat("-", width[j])
	}
	line(sep)
	for _, row := range cells {
		line(row)
	}
	if r.Total > len(r.Rows) {
		fmt.Fprintf(&b, "\n%d of %d rows (--limit for more) · %.1f ms\n", len(r.Rows), r.Total, r.MS)
	} else {
		fmt.Fprintf(&b, "\n%d rows · %.1f ms\n", len(r.Rows), r.MS)
	}
	return b.String()
}

func apiDBQuery(c api.Caller, sql string, limit int) (DBRows, error) {
	if strings.TrimSpace(sql) == "" {
		return DBRows{}, errors.New("give the query")
	}
	if limit <= 0 {
		limit = 100
	}
	var res DBRows
	err := bridgeCall(c, "db.read", map[string]any{"sql": sql, "limit": limit}, &res)
	return res, err
}
