package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

// fixtureSchema is the subset of the Things schema (version 29) used by the
// backend. Fixtures are synthetic: tests never read a real Things database.
const fixtureSchema = `
PRAGMA journal_mode = WAL;
CREATE TABLE TMTask (
	uuid TEXT PRIMARY KEY, type INTEGER, status INTEGER, stopDate REAL, trashed INTEGER,
	title TEXT, notes TEXT, start INTEGER, startDate INTEGER, startBucket INTEGER,
	reminderTime INTEGER, deadline INTEGER, "index" INTEGER, area TEXT, project TEXT,
	heading TEXT, creationDate REAL, userModificationDate REAL, rt1_recurrenceRule BLOB
);
CREATE TABLE TMArea (uuid TEXT PRIMARY KEY, title TEXT, visible INTEGER, "index" INTEGER);
CREATE TABLE TMTag (uuid TEXT PRIMARY KEY, title TEXT, shortcut TEXT, usedDate REAL, parent TEXT, "index" INTEGER);
CREATE TABLE TMTaskTag (tasks TEXT NOT NULL, tags TEXT NOT NULL);
CREATE TABLE TMAreaTag (areas TEXT NOT NULL, tags TEXT NOT NULL);
CREATE TABLE TMChecklistItem (uuid TEXT PRIMARY KEY, title TEXT, status INTEGER, "index" INTEGER, task TEXT);
`

// simulator stands in for the Things app: it applies URL scheme commands and
// the trash AppleScript to a synthetic database, following the documented
// behaviour of Things. It lets the contract suite exercise the whole local
// backend (URL building, read-back, polling) without the real app.
type simulator struct {
	t     *testing.T
	db    *sql.DB
	token string

	mu     sync.Mutex
	seq    int
	calls  [][]string
	ignore bool  // accept commands but do nothing (Things ignoring a URL)
	fail   error // make the next command fail
}

func newFixture(t *testing.T) (path string, sim *simulator) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "main.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(fixtureSchema); err != nil {
		t.Fatal(err)
	}
	return path, &simulator{t: t, db: db, token: "test-token"}
}

func (s *simulator) exec(q string, args ...any) {
	s.t.Helper()
	if _, err := s.db.Exec(q, args...); err != nil {
		s.t.Fatalf("fixture: %v\n%s", err, q)
	}
}

func (s *simulator) newID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s%019d", prefix, s.seq)
}

func (s *simulator) addArea(title string) domain.Area {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.newID("A")
	s.exec(`INSERT INTO TMArea VALUES (?, ?, 1, ?)`, id, title, s.seq)
	return domain.Area{ID: id, Title: title}
}

func (s *simulator) addTag(title string) domain.Tag {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.newID("G")
	s.exec(`INSERT INTO TMTag (uuid, title, "index") VALUES (?, ?, ?)`, id, title, s.seq)
	return domain.Tag{ID: id, Title: title}
}

func (s *simulator) lastCall() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return nil
	}
	return s.calls[len(s.calls)-1]
}

// Run implements Runner.
func (s *simulator) Run(_ context.Context, name string, args ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, append([]string{name}, args...))
	if err := s.fail; err != nil {
		s.fail = nil
		return err
	}
	if s.ignore {
		return nil
	}
	switch name {
	case "osascript":
		if len(args) != 4 || args[0] != "-e" || args[1] != trashScript {
			s.t.Fatalf("unexpected osascript call: %q", args)
		}
		s.exec(`UPDATE TMTask SET trashed = 1, userModificationDate = ? WHERE uuid = ?`, s.stamp(), args[2])
		return nil
	case "open":
		raw := args[len(args)-1]
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "things" {
			s.t.Fatalf("not a things URL: %q", raw)
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			s.t.Fatalf("bad query in %q: %v", raw, err)
		}
		return s.apply(strings.TrimPrefix(u.Path, "/"), q)
	}
	s.t.Fatalf("unexpected command %q", name)
	return nil
}

// stamp returns a strictly increasing timestamp.
func (s *simulator) stamp() float64 {
	s.seq++
	return float64(time.Now().UnixNano())/1e9 + float64(s.seq)*1e-6
}

func (s *simulator) apply(cmd string, q url.Values) error {
	switch cmd {
	case "show":
		return nil
	case "add":
		s.insertTask(q.Get("title"), q, "", "")
		return nil
	case "json":
		var items []jsonItem
		if err := json.Unmarshal([]byte(q.Get("data")), &items); err != nil {
			s.t.Fatalf("json command: %v", err)
		}
		for _, it := range items {
			s.insertProject(it.Attributes)
		}
		return nil
	case "update", "update-project":
		if q.Get("auth-token") != s.token {
			return nil // Things refuses silently
		}
		s.update(q.Get("id"), q)
		return nil
	}
	s.t.Fatalf("unsupported command %q", cmd)
	return nil
}

// schedule maps a "when" value to start/startDate/startBucket/reminderTime.
func schedule(when string, located bool) (start int, startDate, bucket, reminder any) {
	today := domain.DateOf(time.Now())
	if when == "" {
		if located {
			return startAnytime, nil, 0, nil
		}
		return startInbox, nil, 0, nil
	}
	w, err := domain.ParseWhen(when, today)
	if err != nil {
		panic(err)
	}
	if w.Reminder != "" {
		var h, m int64
		fmt.Sscanf(w.Reminder, "%d:%d", &h, &m)
		reminder = h<<26 | m<<20
	}
	switch w.Kind {
	case domain.WhenToday:
		return startAnytime, encodeDate(today), 0, reminder
	case domain.WhenEvening:
		return startAnytime, encodeDate(today), 1, reminder
	case domain.WhenSomeday:
		return startSomeday, nil, 0, nil
	case domain.WhenAnytime:
		return startAnytime, nil, 0, nil
	}
	if today.Before(w.Date) {
		return startSomeday, encodeDate(w.Date), 0, reminder
	}
	return startAnytime, encodeDate(w.Date), 0, reminder
}

func (s *simulator) deadline(v string) any {
	if v == "" {
		return nil
	}
	d, err := domain.ParseDate(v)
	if err != nil {
		s.t.Fatalf("deadline %q: %v", v, err)
	}
	return encodeDate(d)
}

// location resolves a list-id to a project or area column.
func (s *simulator) location(listID string) (project, area any) {
	if listID == "" {
		return nil, nil
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM TMArea WHERE uuid = ?`, listID).Scan(&n)
	if n > 0 {
		return nil, listID
	}
	return listID, nil
}

func (s *simulator) insertTask(title string, q url.Values, project, heading string) string {
	id := s.newID("T")
	var proj, area any
	if project != "" {
		proj = project
	} else {
		proj, area = s.location(q.Get("list-id"))
	}
	var head any
	if h := firstNonEmpty(heading, q.Get("heading-id")); h != "" {
		head, proj = h, nil // tasks under a heading have project = NULL
	}
	start, sd, bucket, rem := schedule(q.Get("when"), proj != nil || area != nil || head != nil)
	now := s.stamp()
	s.exec(`INSERT INTO TMTask (uuid, type, status, trashed, title, notes, start, startDate, startBucket,
		reminderTime, deadline, "index", area, project, heading, creationDate, userModificationDate)
		VALUES (?, 0, 0, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, title, q.Get("notes"), start, sd, bucket, rem, s.deadline(q.Get("deadline")), s.seq,
		area, proj, head, now, now)
	s.setTags(id, q.Get("tags"))
	s.addChecklist(id, q.Get("checklist-items"))
	return id
}

func (s *simulator) insertProject(attrs map[string]any) {
	str := func(k string) string { v, _ := attrs[k].(string); return v }
	id := s.newID("P")
	start, sd, bucket, rem := schedule(str("when"), true)
	now := s.stamp()
	var area any
	if a := str("area-id"); a != "" {
		area = a
	}
	s.exec(`INSERT INTO TMTask (uuid, type, status, trashed, title, notes, start, startDate, startBucket,
		reminderTime, deadline, "index", area, creationDate, userModificationDate)
		VALUES (?, 1, 0, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, str("title"), str("notes"), start, sd, bucket, rem, s.deadline(str("deadline")), s.seq, area, now, now)
	if tags, ok := attrs["tags"].([]any); ok {
		var names []string
		for _, t := range tags {
			names = append(names, t.(string))
		}
		s.setTags(id, strings.Join(names, ","))
	}
	items, _ := attrs["items"].([]any)
	heading := ""
	for _, raw := range items {
		it := raw.(map[string]any)
		title := it["attributes"].(map[string]any)["title"].(string)
		switch it["type"] {
		case "heading":
			heading = s.newID("H")
			s.exec(`INSERT INTO TMTask (uuid, type, status, trashed, title, project, "index", creationDate)
				VALUES (?, 2, 0, 0, ?, ?, ?, ?)`, heading, title, id, s.seq, s.stamp())
		case "to-do":
			s.insertTask(title, url.Values{}, id, heading)
		}
	}
}

func (s *simulator) setTags(id, csv string) {
	s.exec(`DELETE FROM TMTaskTag WHERE tasks = ?`, id)
	s.addTags(id, csv)
}

func (s *simulator) addTags(id, csv string) {
	for _, name := range strings.Split(csv, ",") {
		if name == "" {
			continue
		}
		// Unknown tags are skipped, as Things does.
		s.exec(`INSERT INTO TMTaskTag SELECT ?, uuid FROM TMTag WHERE title = ?`, id, name)
	}
}

func (s *simulator) addChecklist(id, lines string) {
	if lines == "" {
		return
	}
	for _, l := range strings.Split(lines, "\n") {
		s.exec(`INSERT INTO TMChecklistItem VALUES (?, ?, 0, ?, ?)`, s.newID("C"), l, s.seq, id)
	}
}

func (s *simulator) update(id string, q url.Values) {
	set := func(col string, v any) {
		s.exec(fmt.Sprintf(`UPDATE TMTask SET %s = ? WHERE uuid = ?`, col), v, id)
	}
	if q.Has("title") {
		set("title", q.Get("title"))
	}
	if q.Has("notes") {
		set("notes", q.Get("notes"))
	}
	if q.Has("append-notes") {
		s.exec(`UPDATE TMTask SET notes = coalesce(notes, '') || ? WHERE uuid = ?`, q.Get("append-notes"), id)
	}
	if q.Has("list-id") {
		proj, area := s.location(q.Get("list-id"))
		set("project", proj)
		set("area", area)
		set("heading", nil)
	}
	if q.Has("area-id") {
		set("area", q.Get("area-id"))
	}
	if h := q.Get("heading-id"); h != "" {
		set("heading", h)
		set("project", nil)
	}
	if q.Has("when") {
		var located int
		s.db.QueryRow(`SELECT (project IS NOT NULL OR area IS NOT NULL OR heading IS NOT NULL) FROM TMTask WHERE uuid = ?`, id).Scan(&located)
		start, sd, bucket, rem := schedule(q.Get("when"), located == 1)
		set("start", start)
		set("startDate", sd)
		set("startBucket", bucket)
		set("reminderTime", rem)
	}
	if q.Has("deadline") {
		set("deadline", s.deadline(q.Get("deadline")))
	}
	if q.Has("tags") {
		s.setTags(id, q.Get("tags"))
	}
	if q.Has("add-tags") {
		s.addTags(id, q.Get("add-tags"))
	}
	if q.Has("checklist-items") {
		s.exec(`DELETE FROM TMChecklistItem WHERE task = ?`, id)
		s.addChecklist(id, q.Get("checklist-items"))
	}
	s.addChecklist(id, q.Get("append-checklist-items"))
	switch {
	case q.Get("canceled") == "true":
		set("status", 2)
		set("stopDate", s.stamp())
	case q.Get("completed") == "true":
		set("status", 3)
		set("stopDate", s.stamp())
	case q.Get("completed") == "false":
		set("status", 0)
		set("stopDate", nil)
	}
	set("userModificationDate", s.stamp())
}
