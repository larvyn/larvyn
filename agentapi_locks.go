package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"larvyn/internal/api"
	"larvyn/internal/terminal"
)

// Locks let agents working side by side take turns on what they share: the
// database (two agents migrating or seeding it at once break each other),
// the main branch, a port. A lock is a lease: it ends on its own when its
// time is up or when the terminal that took it is gone, so an agent that
// crashes holding one doesn't block the rest. Locks live in the app's
// memory and are per project.
func init() {
	api.Register("lock.acquire", "Take a lock of the caller's project, such as db before migrating, waiting while another terminal holds it; exits 1 when it is still held after the wait. Taking a lock you hold again renews it", apiLockAcquire,
		"name: what it guards, such as db, migrations or main",
		"wait?: seconds to wait while it is held, 300 by default",
		"nowait?: give up at once when it is held",
		"ttl?: seconds the lock lasts unless renewed or released, 600 by default",
		"note?: what you hold it for, shown to whoever waits")
	api.Register("lock.release", "Release a lock you hold", apiLockRelease,
		"name: the lock")
	api.Register("lock.list", "List the locks held in the caller's project", apiLockList,
		"all?: every project's locks")
	api.MarkReadOnly("lock.list")
}

type lease struct {
	project string
	name    string
	owner   string // the terminal that took it, or the folder outside one
	title   string
	note    string
	since   time.Time
	expires time.Time
}

var locks = struct {
	sync.Mutex
	m map[string]*lease // project id + "\x00" + name
}{m: map[string]*lease{}}

// LockRow is a lock in lock list and what acquire and release print.
type LockRow struct {
	Name    string `json:"name"`
	Owner   string `json:"owner"`
	Title   string `json:"title,omitempty"`
	Note    string `json:"note,omitempty"`
	Seconds int    `json:"heldFor"`
	Expires int    `json:"expiresIn"`
	Mine    bool   `json:"mine,omitempty"`
}

func lockRow(l *lease, me string, now time.Time) LockRow {
	return LockRow{Name: l.name, Owner: l.owner, Title: l.title, Note: l.note,
		Seconds: int(now.Sub(l.since).Seconds()), Expires: int(l.expires.Sub(now).Seconds()), Mine: l.owner == me}
}

func (r LockRow) describe() string {
	who := r.Owner
	if r.Title != "" {
		who = r.Title + " (" + r.Owner + ")"
	}
	s := fmt.Sprintf("%s is held by %s for %ds, %ds left", r.Name, who, r.Seconds, r.Expires)
	if r.Note != "" {
		s += ": " + r.Note
	}
	return s
}

// LockResult is what lock acquire prints.
type LockResult struct {
	Acquired bool    `json:"acquired"`
	Lock     LockRow `json:"lock"`
	Waited   int     `json:"waited,omitempty"`
}

func (r LockResult) Text() string {
	if r.Acquired {
		return fmt.Sprintf("%s is yours for %ds; release it with larvyn lock release %s\n", r.Lock.Name, r.Lock.Expires, r.Lock.Name)
	}
	return r.Lock.describe() + fmt.Sprintf("\nstill held after waiting %ds\n", r.Waited)
}

func (r LockResult) ExitCode() int {
	if r.Acquired {
		return 0
	}
	return 1
}

// lockOwner is who holds a caller's locks: its terminal, or outside
// Larvyn's terminals the folder it runs in.
func lockOwner(c api.Caller) (owner, title string) {
	if c.Session != "" {
		if s, err := findTerminal(c.Session); err == nil {
			return s.ID, s.Title
		}
		return c.Session, ""
	}
	return c.Cwd, ""
}

// alive reports whether a lease still counts: not expired, and its
// terminal still open and running.
func alive(l *lease, now time.Time) bool {
	if !now.Before(l.expires) {
		return false
	}
	if strings.ContainsAny(l.owner, `/\`) {
		// A folder: only time ends it.
		return true
	}
	for _, s := range terminal.Sessions.List() {
		if s.ID == l.owner {
			return s.Running
		}
	}
	return false
}

func lockKey(project, name string) string { return project + "\x00" + name }

func lockName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name the lock: db, migrations, main…")
	}
	return name, nil
}

func apiLockAcquire(c api.Caller, name string, wait int, nowait bool, ttl int, note string) (LockResult, error) {
	name, err := lockName(name)
	if err != nil {
		return LockResult{}, err
	}
	p, _, _, err := callerProject(c)
	if err != nil {
		return LockResult{}, err
	}
	switch {
	case nowait:
		wait = 0
	case wait <= 0:
		wait = 300
	}
	if ttl <= 0 {
		ttl = 600
	}
	owner, title := lockOwner(c)
	key := lockKey(p.ID, name)
	start := time.Now()
	limit := time.Duration(wait) * time.Second
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		now := time.Now()
		locks.Lock()
		l := locks.m[key]
		if l == nil || !alive(l, now) || l.owner == owner {
			if l == nil || l.owner != owner {
				l = &lease{project: p.ID, name: name, owner: owner, since: now}
				locks.m[key] = l
			}
			l.title, l.expires = title, now.Add(time.Duration(ttl)*time.Second)
			if note != "" {
				l.note = note
			}
			row := lockRow(l, owner, now)
			locks.Unlock()
			return LockResult{Acquired: true, Lock: row, Waited: int(now.Sub(start).Seconds())}, nil
		}
		row := lockRow(l, owner, now)
		locks.Unlock()
		if now.Sub(start) >= limit {
			return LockResult{Lock: row, Waited: int(now.Sub(start).Seconds())}, nil
		}
		select {
		case <-c.Ctx.Done():
			return LockResult{}, errors.New("stopped waiting")
		case <-tick.C:
		}
	}
}

func apiLockRelease(c api.Caller, name string) error {
	name, err := lockName(name)
	if err != nil {
		return err
	}
	p, _, _, err := callerProject(c)
	if err != nil {
		return err
	}
	owner, _ := lockOwner(c)
	now := time.Now()
	locks.Lock()
	defer locks.Unlock()
	key := lockKey(p.ID, name)
	l := locks.m[key]
	if l == nil || !alive(l, now) {
		delete(locks.m, key)
		return fmt.Errorf("%s is not held", name)
	}
	if l.owner != owner {
		return errors.New(lockRow(l, owner, now).describe() + ", not by you")
	}
	delete(locks.m, key)
	return nil
}

func apiLockList(c api.Caller, all bool) ([]LockRow, error) {
	var project string
	if !all {
		p, _, _, err := callerProject(c)
		if err != nil {
			return nil, fmt.Errorf("%v (or pass --all)", err)
		}
		project = p.ID
	}
	owner, _ := lockOwner(c)
	now := time.Now()
	locks.Lock()
	defer locks.Unlock()
	out := []LockRow{}
	for key, l := range locks.m {
		if !alive(l, now) {
			delete(locks.m, key)
			continue
		}
		if all || l.project == project {
			out = append(out, lockRow(l, owner, now))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
