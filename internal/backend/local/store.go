package local

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no CGO

	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

// DefaultDatabaseGlob locates the Things 3 database relative to $HOME.
const DefaultDatabaseGlob = "Library/Group Containers/JLMPQHK86H.com.culturedcode.ThingsMac/ThingsData-*/Things Database.thingsdatabase/main.sqlite"

// FindDatabase returns the path of the Things database under home.
func FindDatabase(home string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(home, DefaultDatabaseGlob))
	if err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%w: Things database not found (is Things 3 installed and opened once?)", domain.ErrUnavailable)
	case 1:
		return matches[0], nil
	}
	return "", fmt.Errorf("several Things databases found; set THINGS_DB_PATH to choose one")
}

// store reads the Things SQLite database. It never writes: the connection is
// opened read-only and with query_only, so even a bug cannot modify the
// database that Things owns. Writes go through the app (URL scheme,
// AppleScript).
type store struct {
	db *sql.DB
}

func openStore(path string) (*store, error) {
	// mode=ro (not immutable=1): Things keeps writing through its WAL and
	// we must see those writes.
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     path,
		RawQuery: "mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(2000)",
	}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("%w: cannot open the Things database: %v", domain.ErrUnavailable, err)
	}
	return &store{db: db}, nil
}

func (s *store) Close() error { return s.db.Close() }

// taskFilter narrows the rows loaded from TMTask. Fine-grained filtering
// (views, area, tag) happens in Go, with domain.InView as the reference.
type taskFilter struct {
	id     string
	status string // "open", "closed" or "" for any
}

const taskColumns = `
	t.uuid, t.type, coalesce(t.title, ''), coalesce(t.notes, ''), t.status, t.trashed,
	t.start, t.startDate, coalesce(t.startBucket, 0), t.reminderTime, t.deadline,
	coalesce(t.project, h.project, ''), coalesce(t.area, ''), coalesce(t.heading, ''),
	t.creationDate, t.userModificationDate, t.stopDate,
	coalesce(p.trashed, 0)`

// Tasks under a heading have project = NULL: their project is the heading's.
const taskJoins = `
	FROM TMTask t
	LEFT JOIN TMTask h ON h.uuid = t.heading
	LEFT JOIN TMTask p ON p.uuid = coalesce(t.project, h.project)`

func (s *store) tasks(ctx context.Context, f taskFilter, today domain.Date) ([]domain.Task, error) {
	q := `SELECT` + taskColumns + taskJoins + `
	WHERE t.type = ? AND t.rt1_recurrenceRule IS NULL`
	args := []any{typeTask}
	if f.id != "" {
		q += ` AND t.uuid = ?`
		args = append(args, f.id)
	}
	switch f.status {
	case "open":
		q += ` AND t.status = 0`
	case "closed":
		q += ` AND t.status != 0`
	}
	q += ` ORDER BY t."index", t.creationDate`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		var (
			t                         domain.Task
			typ, status, start        int64
			trashed, bucket, pTrashed int64
			startDate, rem, deadline  sql.NullInt64
			created, modified, stop   sql.NullFloat64
		)
		if err := rows.Scan(&t.ID, &typ, &t.Title, &t.Notes, &status, &trashed,
			&start, &startDate, &bucket, &rem, &deadline,
			&t.ProjectID, &t.AreaID, &t.HeadingID,
			&created, &modified, &stop, &pTrashed); err != nil {
			return nil, err
		}
		t.Status = decodeStatus(status)
		// A task of a trashed project is in the trash with it.
		t.Trashed = trashed != 0 || pTrashed != 0
		t.When = decodeWhen(start, startDate, bucket, rem, today)
		t.Deadline = decodeDate(deadline)
		t.CreatedAt, t.ModifiedAt = decodeTime(created), decodeTime(modified)
		if t.Status.Closed() {
			t.CompletedAt = decodeTime(stop)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.attachTags(ctx, out)
}

func (s *store) task(ctx context.Context, id string, today domain.Date) (domain.Task, error) {
	ts, err := s.tasks(ctx, taskFilter{id: id}, today)
	if err != nil {
		return domain.Task{}, err
	}
	if len(ts) == 0 {
		return domain.Task{}, domain.NotFound("task", id)
	}
	t := ts[0]
	rows, err := s.db.QueryContext(ctx,
		`SELECT coalesce(title, ''), status FROM TMChecklistItem WHERE task = ? ORDER BY "index"`, id)
	if err != nil {
		return domain.Task{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.ChecklistItem
		var status int64
		if err := rows.Scan(&item.Title, &status); err != nil {
			return domain.Task{}, err
		}
		item.Status = decodeStatus(status)
		t.Checklist = append(t.Checklist, item)
	}
	return t, rows.Err()
}

// tagsByOwner maps a task, project or area ID to its tag titles.
func (s *store) tagsByOwner(ctx context.Context, query string) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var owner, title string
		if err := rows.Scan(&owner, &title); err != nil {
			return nil, err
		}
		out[owner] = append(out[owner], title)
	}
	return out, rows.Err()
}

const taskTagsQuery = `SELECT tt.tasks, coalesce(g.title, '') FROM TMTaskTag tt
	JOIN TMTag g ON g.uuid = tt.tags ORDER BY g."index"`

func (s *store) attachTags(ctx context.Context, ts []domain.Task) error {
	if len(ts) == 0 {
		return nil
	}
	tags, err := s.tagsByOwner(ctx, taskTagsQuery)
	if err != nil {
		return err
	}
	for i := range ts {
		ts[i].Tags = tags[ts[i].ID]
	}
	return nil
}

func (s *store) projects(ctx context.Context, id string, today domain.Date) ([]domain.Project, error) {
	q := `SELECT uuid, coalesce(title, ''), coalesce(notes, ''), status, trashed,
		start, startDate, coalesce(startBucket, 0), reminderTime, deadline, coalesce(area, ''),
		creationDate, userModificationDate, stopDate
		FROM TMTask WHERE type = ? AND rt1_recurrenceRule IS NULL`
	args := []any{typeProject}
	if id != "" {
		q += ` AND uuid = ?`
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY "index"`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var (
			p                        domain.Project
			status, trashed, start   int64
			bucket                   int64
			startDate, rem, deadline sql.NullInt64
			created, modified, stop  sql.NullFloat64
		)
		if err := rows.Scan(&p.ID, &p.Title, &p.Notes, &status, &trashed, &start, &startDate,
			&bucket, &rem, &deadline, &p.AreaID, &created, &modified, &stop); err != nil {
			return nil, err
		}
		p.Status, p.Trashed = decodeStatus(status), trashed != 0
		p.When = decodeWhen(start, startDate, bucket, rem, today)
		p.Deadline = decodeDate(deadline)
		p.CreatedAt, p.ModifiedAt = decodeTime(created), decodeTime(modified)
		if p.Status.Closed() {
			p.CompletedAt = decodeTime(stop)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tags, err := s.tagsByOwner(ctx, taskTagsQuery)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Tags = tags[out[i].ID]
	}
	return out, nil
}

func (s *store) project(ctx context.Context, id string, today domain.Date) (domain.Project, error) {
	ps, err := s.projects(ctx, id, today)
	if err != nil {
		return domain.Project{}, err
	}
	if len(ps) == 0 {
		return domain.Project{}, domain.NotFound("project", id)
	}
	return ps[0], nil
}

func (s *store) headings(ctx context.Context, projectID string) ([]domain.Heading, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT uuid, coalesce(title, ''), status FROM TMTask
		WHERE type = ? AND project = ? AND trashed = 0 ORDER BY "index"`, typeHeading, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Heading{}
	for rows.Next() {
		h := domain.Heading{ProjectID: projectID}
		var status int64
		if err := rows.Scan(&h.ID, &h.Title, &status); err != nil {
			return nil, err
		}
		h.Archived = status != 0
		out = append(out, h)
	}
	return out, rows.Err()
}

// headingProject returns the project owning a heading.
func (s *store) headingProject(ctx context.Context, id string) (string, error) {
	var project sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT project FROM TMTask WHERE uuid = ? AND type = ?`, id, typeHeading).Scan(&project)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.NotFound("heading", id)
	}
	return project.String, err
}

func (s *store) areas(ctx context.Context) ([]domain.Area, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT uuid, coalesce(title, '') FROM TMArea ORDER BY "index"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Area
	for rows.Next() {
		var a domain.Area
		if err := rows.Scan(&a.ID, &a.Title); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tags, err := s.tagsByOwner(ctx, `SELECT at.areas, coalesce(g.title, '') FROM TMAreaTag at
		JOIN TMTag g ON g.uuid = at.tags ORDER BY g."index"`)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Tags = tags[out[i].ID]
	}
	return out, nil
}

func (s *store) tags(ctx context.Context) ([]domain.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT uuid, coalesce(title, ''), coalesce(parent, '') FROM TMTag ORDER BY "index"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Tag
	for rows.Next() {
		var t domain.Tag
		if err := rows.Scan(&t.ID, &t.Title, &t.ParentID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *store) areaExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM TMArea WHERE uuid = ?`, id).Scan(&n)
	return n > 0, err
}

// itemType returns the TMTask type of an ID, or -1 if it does not exist.
func (s *store) itemType(ctx context.Context, id string) (int, error) {
	var typ int
	err := s.db.QueryRowContext(ctx, `SELECT type FROM TMTask WHERE uuid = ?`, id).Scan(&typ)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, nil
	}
	return typ, err
}

// state returns the trashed flag, status and modification time of an item,
// used to confirm that the app applied a write.
func (s *store) state(ctx context.Context, id string) (trashed bool, status domain.Status, modified time.Time, err error) {
	var tr, st int64
	var mod sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `SELECT trashed, status, userModificationDate FROM TMTask WHERE uuid = ?`, id).Scan(&tr, &st, &mod)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.NotFound("item", id)
	}
	return tr != 0, decodeStatus(st), decodeTime(mod), err
}

// findCreated looks for the newest item of a type with this title created
// at or after since. The URL scheme does not return the ID of what it
// creates, so the backend reads it back from the database.
func (s *store) findCreated(ctx context.Context, typ int, title string, since time.Time) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT uuid FROM TMTask
		WHERE type = ? AND title = ? AND creationDate >= ? ORDER BY creationDate DESC LIMIT 1`,
		typ, title, float64(since.UnixNano())/1e9).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// canonicalTags resolves tag names case-insensitively to their exact
// titles, and reports unknown tags (the URL scheme would drop them silently).
func (s *store) canonicalTags(ctx context.Context, names []string) ([]string, error) {
	if len(names) == 0 {
		return names, nil
	}
	all, err := s.tags(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		found := ""
		for _, t := range all {
			if strings.EqualFold(t.Title, n) {
				found = t.Title
				break
			}
		}
		if found == "" {
			return nil, domain.NotFound("tag", n)
		}
		if !slices.Contains(out, found) {
			out = append(out, found)
		}
	}
	return out, nil
}
