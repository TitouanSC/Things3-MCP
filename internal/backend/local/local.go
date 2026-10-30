// Package local implements backend.TaskBackend on macOS, next to the
// Things 3 app (see docs/adr/002):
//   - reads come from the Things SQLite database, opened read-only;
//   - writes go through the official URL scheme (things:///), opened in
//     the background with `open -g`;
//   - moving to the trash uses AppleScript, the only interface able to do it.
//
// The URL scheme is fire-and-forget: after each write the backend polls the
// database to confirm the change and, for creations, to find the new ID.
package local

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/TitouanSC/Things3-MCP/internal/backend"
	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

// Config configures the local backend.
type Config struct {
	// DBPath is the Things database. Required (see FindDatabase).
	DBPath string
	// AuthToken is the Things URL scheme token, required for updates.
	AuthToken string
	// Runner executes `open` and `osascript`. Defaults to ExecRunner.
	Runner Runner
	// Now is the clock. Defaults to time.Now.
	Now func() time.Time
	// WriteTimeout bounds how long a write waits for Things to apply it.
	// Defaults to 5s.
	WriteTimeout time.Duration
	// PollInterval is the delay between two database checks. Defaults to 100ms.
	PollInterval time.Duration
}

// Backend is the local macOS backend.
type Backend struct {
	store *store
	cfg   Config
}

var _ backend.TaskBackend = (*Backend)(nil)

// New opens the Things database and returns a backend.
func New(cfg Config) (*Backend, error) {
	if cfg.DBPath == "" {
		return nil, errors.New("local backend: DBPath is required")
	}
	if cfg.Runner == nil {
		cfg.Runner = ExecRunner{}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 5 * time.Second
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 100 * time.Millisecond
	}
	s, err := openStore(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	return &Backend{store: s, cfg: cfg}, nil
}

// Close releases the database.
func (b *Backend) Close() error { return b.store.Close() }

// Capabilities implements backend.TaskBackend. IDs of created items are
// read back from the database on a best-effort basis, hence ReturnsIDs=false.
func (b *Backend) Capabilities() backend.Capabilities {
	return backend.Capabilities{Trash: true, ShowInApp: true, ReturnsIDs: false}
}

func (b *Backend) today() domain.Date { return domain.DateOf(b.cfg.Now()) }

// --- Read -------------------------------------------------------------------

// ListTasks implements backend.TaskBackend.
func (b *Backend) ListTasks(ctx context.Context, q domain.TaskQuery) (backend.Page[domain.Task], error) {
	if err := q.Normalize(); err != nil {
		return backend.Page[domain.Task]{}, err
	}
	f := taskFilter{status: "open"}
	switch q.View {
	case domain.ViewLogbook:
		f.status = "closed"
	case domain.ViewTrash:
		f.status = ""
	}
	today := b.today()
	all, err := b.store.tasks(ctx, f, today)
	if err != nil {
		return backend.Page[domain.Task]{}, err
	}
	var projectArea map[string]string
	if q.AreaID != "" {
		ps, err := b.store.projects(ctx, "", today)
		if err != nil {
			return backend.Page[domain.Task]{}, err
		}
		projectArea = make(map[string]string, len(ps))
		for _, p := range ps {
			projectArea[p.ID] = p.AreaID
		}
	}
	var out []domain.Task
	for _, t := range all {
		if q.View != "" {
			if !domain.InView(t, q.View, today) {
				continue
			}
		} else if t.Trashed {
			continue
		}
		if q.ProjectID != "" && t.ProjectID != q.ProjectID {
			continue
		}
		if q.AreaID != "" && t.AreaID != q.AreaID && projectArea[t.ProjectID] != q.AreaID {
			continue
		}
		if q.Tag != "" && !slices.ContainsFunc(t.Tags, func(s string) bool { return strings.EqualFold(s, q.Tag) }) {
			continue
		}
		out = append(out, t)
	}
	if q.View == domain.ViewLogbook {
		sort.SliceStable(out, func(i, j int) bool { return out[i].CompletedAt.After(out[j].CompletedAt) })
	}
	return backend.Paginate(out, q.Cursor, q.Limit)
}

// GetTask implements backend.TaskBackend.
func (b *Backend) GetTask(ctx context.Context, id string) (domain.Task, error) {
	if err := domain.ValidateID("id", id); err != nil {
		return domain.Task{}, err
	}
	return b.store.task(ctx, id, b.today())
}

// ListProjects implements backend.TaskBackend.
func (b *Backend) ListProjects(ctx context.Context, q domain.ProjectQuery) (backend.Page[domain.Project], error) {
	if err := q.Normalize(); err != nil {
		return backend.Page[domain.Project]{}, err
	}
	all, err := b.store.projects(ctx, "", b.today())
	if err != nil {
		return backend.Page[domain.Project]{}, err
	}
	var out []domain.Project
	for _, p := range all {
		if !p.Trashed && p.Status == q.Status && (q.AreaID == "" || p.AreaID == q.AreaID) {
			out = append(out, p)
		}
	}
	return backend.Paginate(out, q.Cursor, q.Limit)
}

// GetProject implements backend.TaskBackend.
func (b *Backend) GetProject(ctx context.Context, id string) (domain.ProjectDetail, error) {
	if err := domain.ValidateID("id", id); err != nil {
		return domain.ProjectDetail{}, err
	}
	today := b.today()
	p, err := b.store.project(ctx, id, today)
	if err != nil {
		return domain.ProjectDetail{}, err
	}
	headings, err := b.store.headings(ctx, id)
	if err != nil {
		return domain.ProjectDetail{}, err
	}
	all, err := b.store.tasks(ctx, taskFilter{}, today)
	if err != nil {
		return domain.ProjectDetail{}, err
	}
	d := domain.ProjectDetail{Project: p, Headings: headings, Tasks: []domain.Task{}}
	for _, t := range all {
		if t.ProjectID == id && (!t.Trashed || p.Trashed) {
			d.Tasks = append(d.Tasks, t)
		}
	}
	return d, nil
}

// ListAreas implements backend.TaskBackend.
func (b *Backend) ListAreas(ctx context.Context) ([]domain.Area, error) { return b.store.areas(ctx) }

// ListTags implements backend.TaskBackend.
func (b *Backend) ListTags(ctx context.Context) ([]domain.Tag, error) { return b.store.tags(ctx) }

// Search implements backend.TaskBackend. Matching is done in Go because
// SQLite's LIKE is case-insensitive for ASCII only.
func (b *Backend) Search(ctx context.Context, q domain.SearchQuery) (backend.Page[domain.SearchHit], error) {
	if err := q.Normalize(); err != nil {
		return backend.Page[domain.SearchHit]{}, err
	}
	today := b.today()
	needle := strings.ToLower(q.Text)
	match := func(kind domain.ItemKind, title, notes string, st domain.Status, trashed bool) bool {
		return !trashed && (q.Kind == "" || q.Kind == kind) && (q.Status == "" || q.Status == st) &&
			(strings.Contains(strings.ToLower(title), needle) || strings.Contains(strings.ToLower(notes), needle))
	}
	var out []domain.SearchHit
	if q.Kind != domain.KindTask {
		ps, err := b.store.projects(ctx, "", today)
		if err != nil {
			return backend.Page[domain.SearchHit]{}, err
		}
		for _, p := range ps {
			if match(domain.KindProject, p.Title, p.Notes, p.Status, p.Trashed) {
				out = append(out, domain.SearchHit{Kind: domain.KindProject, ID: p.ID, Title: p.Title, Status: p.Status})
			}
		}
	}
	if q.Kind != domain.KindProject {
		ts, err := b.store.tasks(ctx, taskFilter{}, today)
		if err != nil {
			return backend.Page[domain.SearchHit]{}, err
		}
		for _, t := range ts {
			if match(domain.KindTask, t.Title, t.Notes, t.Status, t.Trashed) {
				out = append(out, domain.SearchHit{Kind: domain.KindTask, ID: t.ID, Title: t.Title, Status: t.Status})
			}
		}
	}
	return backend.Paginate(out, q.Cursor, q.Limit)
}

// --- Write ------------------------------------------------------------------

// CreateTask implements backend.TaskBackend with the `add` command.
func (b *Backend) CreateTask(ctx context.Context, n domain.NewTask) (domain.Task, error) {
	if err := n.Validate(); err != nil {
		return domain.Task{}, err
	}
	if err := b.checkLocation(ctx, n.ProjectID, n.AreaID, n.HeadingID); err != nil {
		return domain.Task{}, err
	}
	tags, err := b.store.canonicalTags(ctx, n.Tags)
	if err != nil {
		return domain.Task{}, err
	}
	u := newURL("add").set("title", n.Title).setIf("notes", n.Notes).
		setIf("when", n.When.String()).setIf("deadline", n.Deadline.String()).
		setIf("tags", strings.Join(tags, ",")).
		setIf("checklist-items", strings.Join(n.Checklist, "\n")).
		setIf("list-id", firstNonEmpty(n.ProjectID, n.AreaID)).setIf("heading-id", n.HeadingID)

	id, err := b.createAndFind(ctx, u.String(), typeTask, n.Title)
	if err != nil {
		return domain.Task{}, err
	}
	if id == "" {
		// Things accepted the URL but the item was not seen in time: report
		// success without an ID rather than an error, which could make the
		// client retry and create a duplicate.
		return domain.Task{Title: n.Title, Notes: n.Notes, Status: domain.StatusOpen, When: n.When,
			Deadline: n.Deadline, Tags: tags, ProjectID: n.ProjectID, AreaID: n.AreaID, HeadingID: n.HeadingID}, nil
	}
	return b.store.task(ctx, id, b.today())
}

// CreateProject implements backend.TaskBackend with the `json` command, the
// only one able to create headings.
func (b *Backend) CreateProject(ctx context.Context, n domain.NewProject) (domain.Project, error) {
	if err := n.Validate(); err != nil {
		return domain.Project{}, err
	}
	if err := b.checkLocation(ctx, "", n.AreaID, ""); err != nil {
		return domain.Project{}, err
	}
	tags, err := b.store.canonicalTags(ctx, n.Tags)
	if err != nil {
		return domain.Project{}, err
	}
	attrs := map[string]any{"title": n.Title}
	setAttr(attrs, "notes", n.Notes)
	setAttr(attrs, "when", n.When.String())
	setAttr(attrs, "deadline", n.Deadline.String())
	setAttr(attrs, "area-id", n.AreaID)
	if len(tags) > 0 {
		attrs["tags"] = tags
	}
	// To-dos listed after a heading belong to it, so loose to-dos come first.
	var items []jsonItem
	for _, t := range n.Tasks {
		items = append(items, jsonItem{Type: "to-do", Attributes: map[string]any{"title": t}})
	}
	for _, h := range n.Headings {
		items = append(items, jsonItem{Type: "heading", Attributes: map[string]any{"title": h.Title}})
		for _, t := range h.Tasks {
			items = append(items, jsonItem{Type: "to-do", Attributes: map[string]any{"title": t}})
		}
	}
	if len(items) > 0 {
		attrs["items"] = items
	}
	data, err := jsonData([]jsonItem{{Type: "project", Attributes: attrs}})
	if err != nil {
		return domain.Project{}, err
	}
	id, err := b.createAndFind(ctx, newURL("json").set("data", data).String(), typeProject, n.Title)
	if err != nil {
		return domain.Project{}, err
	}
	if id == "" {
		return domain.Project{Title: n.Title, Notes: n.Notes, Status: domain.StatusOpen, When: n.When,
			Deadline: n.Deadline, Tags: tags, AreaID: n.AreaID}, nil
	}
	return b.store.project(ctx, id, b.today())
}

// UpdateTask implements backend.TaskBackend with the `update` command.
func (b *Backend) UpdateTask(ctx context.Context, id string, p domain.TaskPatch) (domain.Task, error) {
	if err := errors.Join(domain.ValidateID("id", id), p.Validate()); err != nil {
		return domain.Task{}, err
	}
	if err := b.requireToken(); err != nil {
		return domain.Task{}, err
	}
	cur, err := b.store.task(ctx, id, b.today())
	if err != nil {
		return domain.Task{}, err
	}
	projectID, areaID, headingID := cur.ProjectID, cur.AreaID, cur.HeadingID
	moved := p.ProjectID != nil || p.AreaID != nil || p.HeadingID != nil
	if p.ProjectID != nil {
		projectID, areaID, headingID = *p.ProjectID, "", ""
	}
	if p.AreaID != nil {
		projectID, areaID, headingID = "", *p.AreaID, ""
	}
	if p.HeadingID != nil {
		headingID = *p.HeadingID
	}
	if moved {
		if err := b.checkLocation(ctx, projectID, areaID, headingID); err != nil {
			return domain.Task{}, err
		}
	}

	u := newURL("update").set("id", id).set("auth-token", b.cfg.AuthToken)
	if p.Title != nil {
		u.set("title", *p.Title)
	}
	if err := b.setNotesAndTags(ctx, u, cur.Notes, p.Notes, p.AppendNotes, p.Tags, p.AddTags); err != nil {
		return domain.Task{}, err
	}
	setSchedule(u, p.When, p.Deadline)
	if p.Checklist != nil {
		u.set("checklist-items", strings.Join(*p.Checklist, "\n"))
	}
	u.setIf("append-checklist-items", strings.Join(p.AppendChecklist, "\n"))
	if moved {
		u.setIf("list-id", firstNonEmpty(projectID, areaID)).setIf("heading-id", headingID)
	}
	if err := b.updateAndWait(ctx, id, u.String(), nil); err != nil {
		return domain.Task{}, err
	}
	return b.store.task(ctx, id, b.today())
}

// UpdateProject implements backend.TaskBackend with the `update-project` command.
func (b *Backend) UpdateProject(ctx context.Context, id string, p domain.ProjectPatch) (domain.Project, error) {
	if err := errors.Join(domain.ValidateID("id", id), p.Validate()); err != nil {
		return domain.Project{}, err
	}
	if err := b.requireToken(); err != nil {
		return domain.Project{}, err
	}
	cur, err := b.store.project(ctx, id, b.today())
	if err != nil {
		return domain.Project{}, err
	}
	if p.AreaID != nil {
		if err := b.checkLocation(ctx, "", *p.AreaID, ""); err != nil {
			return domain.Project{}, err
		}
	}
	u := newURL("update-project").set("id", id).set("auth-token", b.cfg.AuthToken)
	if p.Title != nil {
		u.set("title", *p.Title)
	}
	if err := b.setNotesAndTags(ctx, u, cur.Notes, p.Notes, p.AppendNotes, p.Tags, p.AddTags); err != nil {
		return domain.Project{}, err
	}
	setSchedule(u, p.When, p.Deadline)
	if p.AreaID != nil {
		u.set("area-id", *p.AreaID)
	}
	if err := b.updateAndWait(ctx, id, u.String(), nil); err != nil {
		return domain.Project{}, err
	}
	return b.store.project(ctx, id, b.today())
}

// SetTaskStatus implements backend.TaskBackend. The result is verified:
// Things silently ignores status changes on repeating to-dos.
func (b *Backend) SetTaskStatus(ctx context.Context, id string, s domain.Status) (domain.Task, error) {
	if _, err := domain.ParseStatus(string(s)); err != nil {
		return domain.Task{}, err
	}
	if err := domain.ValidateID("id", id); err != nil {
		return domain.Task{}, err
	}
	cur, err := b.store.task(ctx, id, b.today())
	if err != nil {
		return domain.Task{}, err
	}
	if cur.Status == s {
		return cur, nil
	}
	if err := b.requireToken(); err != nil {
		return domain.Task{}, err
	}
	u := newURL("update").set("id", id).set("auth-token", b.cfg.AuthToken)
	switch s {
	case domain.StatusCompleted:
		u.set("completed", "true")
	case domain.StatusCanceled:
		u.set("canceled", "true")
	default:
		u.set("completed", "false")
	}
	applied := func(_ bool, st domain.Status) bool { return st == s }
	if err := b.updateAndWait(ctx, id, u.String(), applied); err != nil {
		return domain.Task{}, err
	}
	return b.store.task(ctx, id, b.today())
}

// MoveToTrash implements backend.TaskBackend with AppleScript.
func (b *Backend) MoveToTrash(ctx context.Context, id string) error {
	if err := domain.ValidateID("id", id); err != nil {
		return err
	}
	typ, err := b.store.itemType(ctx, id)
	if err != nil {
		return err
	}
	if typ != typeTask && typ != typeProject {
		return domain.NotFound("task or project", id)
	}
	if trashed, _, _, err := b.store.state(ctx, id); err != nil || trashed {
		return err // already in the trash: idempotent
	}
	kind := "to do"
	if typ == typeProject {
		kind = "project"
	}
	if err := b.run(ctx, "osascript", "-e", trashScript, id, kind); err != nil {
		return err
	}
	return b.waitFor(ctx, id, func(trashed bool, _ domain.Status) bool { return trashed })
}

// ShowInApp implements backend.TaskBackend.
func (b *Backend) ShowInApp(ctx context.Context, id string) error {
	if err := domain.ValidateID("id", id); err != nil {
		return err
	}
	typ, err := b.store.itemType(ctx, id)
	if err != nil {
		return err
	}
	if typ < 0 {
		ok, err := b.store.areaExists(ctx, id)
		if err != nil {
			return err
		}
		if !ok {
			return domain.NotFound("item", id)
		}
	}
	// Without -g: the point is to bring Things to the front.
	return b.run(ctx, "open", newURL("show").set("id", id).String())
}

// --- Helpers ----------------------------------------------------------------

func (b *Backend) requireToken() error {
	if b.cfg.AuthToken == "" {
		return fmt.Errorf("%w: updates need the Things URL token; set THINGS_AUTH_TOKEN (Things → Settings → General → Enable Things URLs → Manage)", domain.ErrUnsupported)
	}
	return nil
}

// run executes a command and scrubs the auth token from any error.
func (b *Backend) run(ctx context.Context, name string, args ...string) error {
	err := b.cfg.Runner.Run(ctx, name, args...)
	if err == nil {
		return nil
	}
	msg := err.Error()
	if b.cfg.AuthToken != "" {
		msg = strings.ReplaceAll(msg, b.cfg.AuthToken, "[REDACTED]")
		msg = strings.ReplaceAll(msg, escape(b.cfg.AuthToken), "[REDACTED]")
	}
	return fmt.Errorf("%w: %s", domain.ErrUnavailable, msg)
}

// createAndFind opens a creation URL and polls the database for the new item.
// It returns "" if the item did not show up before the timeout.
func (b *Backend) createAndFind(ctx context.Context, rawURL string, typ int, title string) (string, error) {
	// Small margin: Things stamps creationDate with its own reading of the clock.
	since := b.cfg.Now().Add(-2 * time.Second)
	if err := b.run(ctx, "open", "-g", rawURL); err != nil {
		return "", err
	}
	var id string
	err := b.poll(ctx, func() (bool, error) {
		var err error
		id, err = b.store.findCreated(ctx, typ, title, since)
		return id != "", err
	})
	if errors.Is(err, context.DeadlineExceeded) {
		return "", nil
	}
	return id, err
}

// updateAndWait opens an update URL and waits until the item's modification
// date changes (and, if given, until applied holds). A timeout is not an
// error: Things may legitimately leave the item unchanged.
func (b *Backend) updateAndWait(ctx context.Context, id, rawURL string, applied func(bool, domain.Status) bool) error {
	_, _, before, err := b.store.state(ctx, id)
	if err != nil {
		return err
	}
	if err := b.run(ctx, "open", "-g", rawURL); err != nil {
		return err
	}
	err = b.poll(ctx, func() (bool, error) {
		trashed, st, mod, err := b.store.state(ctx, id)
		if err != nil {
			return false, err
		}
		if applied != nil {
			return applied(trashed, st), nil
		}
		return mod.After(before), nil
	})
	if errors.Is(err, context.DeadlineExceeded) {
		if applied != nil {
			return fmt.Errorf("%w: Things did not apply the change (repeating to-dos cannot be completed through the URL scheme)", domain.ErrUnsupported)
		}
		return nil
	}
	return err
}

func (b *Backend) waitFor(ctx context.Context, id string, applied func(bool, domain.Status) bool) error {
	err := b.poll(ctx, func() (bool, error) {
		trashed, st, _, err := b.store.state(ctx, id)
		return err == nil && applied(trashed, st), err
	})
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: Things did not confirm the change in time", domain.ErrUnavailable)
	}
	return err
}

// poll calls check until it returns true, an error, or WriteTimeout elapses
// (reported as context.DeadlineExceeded).
func (b *Backend) poll(ctx context.Context, check func() (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, b.cfg.WriteTimeout)
	defer cancel()
	tick := time.NewTicker(b.cfg.PollInterval)
	defer tick.Stop()
	for {
		ok, err := check()
		if err != nil || ok {
			return err
		}
		select {
		case <-ctx.Done():
			return context.DeadlineExceeded
		case <-tick.C:
		}
	}
}

// checkLocation verifies references before writing: the URL scheme silently
// ignores unknown projects, areas and headings.
func (b *Backend) checkLocation(ctx context.Context, projectID, areaID, headingID string) error {
	if projectID != "" {
		typ, err := b.store.itemType(ctx, projectID)
		if err != nil {
			return err
		}
		if typ != typeProject {
			return domain.NotFound("project", projectID)
		}
	}
	if areaID != "" {
		ok, err := b.store.areaExists(ctx, areaID)
		if err != nil {
			return err
		}
		if !ok {
			return domain.NotFound("area", areaID)
		}
	}
	if headingID != "" {
		owner, err := b.store.headingProject(ctx, headingID)
		if err != nil {
			return err
		}
		if owner != projectID {
			return domain.Invalid("heading_id", "heading %q does not belong to project %q", headingID, projectID)
		}
	}
	return nil
}

// setNotesAndTags adds the notes and tags parameters of an update.
func (b *Backend) setNotesAndTags(ctx context.Context, u *thingsURL, current string, notes *string, appendNotes string, tags *[]string, addTags []string) error {
	if notes != nil {
		u.set("notes", *notes)
	}
	if appendNotes != "" {
		// Same convention as the other backends: appended text goes on a new line.
		if current != "" {
			appendNotes = "\n" + appendNotes
		}
		u.set("append-notes", appendNotes)
	}
	if tags != nil {
		canon, err := b.store.canonicalTags(ctx, *tags)
		if err != nil {
			return err
		}
		u.set("tags", strings.Join(canon, ","))
	}
	if len(addTags) > 0 {
		canon, err := b.store.canonicalTags(ctx, addTags)
		if err != nil {
			return err
		}
		u.set("add-tags", strings.Join(canon, ","))
	}
	return nil
}

func setSchedule(u *thingsURL, when *domain.When, deadline *domain.Date) {
	if when != nil {
		u.set("when", when.String()) // empty clears
	}
	if deadline != nil {
		u.set("deadline", deadline.String())
	}
}

func setAttr(m map[string]any, key, value string) {
	if value != "" {
		m[key] = value
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
