// Package fake provides an in-memory TaskBackend for tests. It follows the
// reference semantics of the domain package (views, validation, trash) and
// is deterministic when given a fixed clock.
package fake

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TitouanSC/Things3-MCP/internal/backend"
	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

// Backend is an in-memory TaskBackend. It is safe for concurrent use.
type Backend struct {
	mu    sync.Mutex
	now   func() time.Time
	caps  backend.Capabilities
	seq   int
	shown []string

	tasks    []*domain.Task
	projects []*domain.Project
	headings []*domain.Heading
	areas    []*domain.Area
	tags     []*domain.Tag
}

var _ backend.TaskBackend = (*Backend)(nil)

// Option configures a Backend.
type Option func(*Backend)

// WithClock sets the clock used for timestamps and view computation.
func WithClock(now func() time.Time) Option { return func(b *Backend) { b.now = now } }

// WithCapabilities overrides the advertised capabilities (all enabled by default).
func WithCapabilities(c backend.Capabilities) Option { return func(b *Backend) { b.caps = c } }

// New returns an empty in-memory backend.
func New(opts ...Option) *Backend {
	b := &Backend{
		now:  time.Now,
		caps: backend.Capabilities{Trash: true, ShowInApp: true, ReturnsIDs: true},
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// SeedArea adds an area. The URL scheme cannot create areas, so test setups
// create them out of band, as a user would in the app.
func (b *Backend) SeedArea(title string) domain.Area {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := &domain.Area{ID: b.newID("A"), Title: title}
	b.areas = append(b.areas, a)
	return *a
}

// SeedTag adds a tag, optionally nested under parentID.
func (b *Backend) SeedTag(title, parentID string) domain.Tag {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := &domain.Tag{ID: b.newID("G"), Title: title, ParentID: parentID}
	b.tags = append(b.tags, t)
	return *t
}

// Shown returns the IDs passed to ShowInApp, in call order.
func (b *Backend) Shown() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.shown)
}

// Capabilities implements backend.TaskBackend.
func (b *Backend) Capabilities() backend.Capabilities { return b.caps }

// --- Read -------------------------------------------------------------------

// ListTasks implements backend.TaskBackend.
func (b *Backend) ListTasks(_ context.Context, q domain.TaskQuery) (backend.Page[domain.Task], error) {
	if err := q.Normalize(); err != nil {
		return backend.Page[domain.Task]{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	today := domain.DateOf(b.now())
	var out []domain.Task
	for _, t := range b.tasks {
		if q.View != "" {
			if !domain.InView(*t, q.View, today) {
				continue
			}
		} else if t.Trashed || t.Status.Closed() {
			continue
		}
		if q.ProjectID != "" && t.ProjectID != q.ProjectID {
			continue
		}
		if q.AreaID != "" && b.areaOf(t) != q.AreaID {
			continue
		}
		if q.Tag != "" && !hasTag(t.Tags, q.Tag) {
			continue
		}
		out = append(out, cloneTask(t))
	}
	if q.View == domain.ViewLogbook {
		sort.SliceStable(out, func(i, j int) bool { return out[i].CompletedAt.After(out[j].CompletedAt) })
	}
	return backend.Paginate(out, q.Cursor, q.Limit)
}

// GetTask implements backend.TaskBackend.
func (b *Backend) GetTask(_ context.Context, id string) (domain.Task, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, err := b.task(id)
	if err != nil {
		return domain.Task{}, err
	}
	return cloneTask(t), nil
}

// ListProjects implements backend.TaskBackend.
func (b *Backend) ListProjects(_ context.Context, q domain.ProjectQuery) (backend.Page[domain.Project], error) {
	if err := q.Normalize(); err != nil {
		return backend.Page[domain.Project]{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []domain.Project
	for _, p := range b.projects {
		if p.Trashed || p.Status != q.Status || (q.AreaID != "" && p.AreaID != q.AreaID) {
			continue
		}
		out = append(out, cloneProject(p))
	}
	return backend.Paginate(out, q.Cursor, q.Limit)
}

// GetProject implements backend.TaskBackend.
func (b *Backend) GetProject(_ context.Context, id string) (domain.ProjectDetail, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, err := b.project(id)
	if err != nil {
		return domain.ProjectDetail{}, err
	}
	d := domain.ProjectDetail{Project: cloneProject(p), Headings: []domain.Heading{}, Tasks: []domain.Task{}}
	for _, h := range b.headings {
		if h.ProjectID == id {
			d.Headings = append(d.Headings, *h)
		}
	}
	for _, t := range b.tasks {
		if t.ProjectID == id && !t.Trashed {
			d.Tasks = append(d.Tasks, cloneTask(t))
		}
	}
	return d, nil
}

// ListAreas implements backend.TaskBackend.
func (b *Backend) ListAreas(context.Context) ([]domain.Area, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]domain.Area, 0, len(b.areas))
	for _, a := range b.areas {
		c := *a
		c.Tags = slices.Clone(a.Tags)
		out = append(out, c)
	}
	return out, nil
}

// ListTags implements backend.TaskBackend.
func (b *Backend) ListTags(context.Context) ([]domain.Tag, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]domain.Tag, 0, len(b.tags))
	for _, t := range b.tags {
		out = append(out, *t)
	}
	return out, nil
}

// Search implements backend.TaskBackend.
func (b *Backend) Search(_ context.Context, q domain.SearchQuery) (backend.Page[domain.SearchHit], error) {
	if err := q.Normalize(); err != nil {
		return backend.Page[domain.SearchHit]{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	needle := strings.ToLower(q.Text)
	match := func(kind domain.ItemKind, title, notes string, st domain.Status, trashed bool) bool {
		return !trashed &&
			(q.Kind == "" || q.Kind == kind) &&
			(q.Status == "" || q.Status == st) &&
			(strings.Contains(strings.ToLower(title), needle) || strings.Contains(strings.ToLower(notes), needle))
	}
	var out []domain.SearchHit
	for _, p := range b.projects {
		if match(domain.KindProject, p.Title, p.Notes, p.Status, p.Trashed) {
			out = append(out, domain.SearchHit{Kind: domain.KindProject, ID: p.ID, Title: p.Title, Status: p.Status})
		}
	}
	for _, t := range b.tasks {
		if match(domain.KindTask, t.Title, t.Notes, t.Status, t.Trashed) {
			out = append(out, domain.SearchHit{Kind: domain.KindTask, ID: t.ID, Title: t.Title, Status: t.Status})
		}
	}
	return backend.Paginate(out, q.Cursor, q.Limit)
}

// --- Write ------------------------------------------------------------------

// CreateTask implements backend.TaskBackend.
func (b *Backend) CreateTask(_ context.Context, n domain.NewTask) (domain.Task, error) {
	if err := n.Validate(); err != nil {
		return domain.Task{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocation(n.ProjectID, n.AreaID, n.HeadingID); err != nil {
		return domain.Task{}, err
	}
	tags, err := b.resolveTags(n.Tags)
	if err != nil {
		return domain.Task{}, err
	}
	now := b.now()
	t := &domain.Task{
		ID: b.newID("T"), Title: n.Title, Notes: n.Notes, Status: domain.StatusOpen,
		When: n.When, Deadline: n.Deadline, Tags: tags, Checklist: newChecklist(n.Checklist),
		ProjectID: n.ProjectID, AreaID: n.AreaID, HeadingID: n.HeadingID,
		CreatedAt: now, ModifiedAt: now,
	}
	b.tasks = append(b.tasks, t)
	return cloneTask(t), nil
}

// CreateProject implements backend.TaskBackend.
func (b *Backend) CreateProject(_ context.Context, n domain.NewProject) (domain.Project, error) {
	if err := n.Validate(); err != nil {
		return domain.Project{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocation("", n.AreaID, ""); err != nil {
		return domain.Project{}, err
	}
	tags, err := b.resolveTags(n.Tags)
	if err != nil {
		return domain.Project{}, err
	}
	now := b.now()
	p := &domain.Project{
		ID: b.newID("P"), Title: n.Title, Notes: n.Notes, Status: domain.StatusOpen,
		When: n.When, Deadline: n.Deadline, Tags: tags, AreaID: n.AreaID,
		CreatedAt: now, ModifiedAt: now,
	}
	b.projects = append(b.projects, p)
	addTask := func(title, headingID string) {
		b.tasks = append(b.tasks, &domain.Task{
			ID: b.newID("T"), Title: title, Status: domain.StatusOpen,
			ProjectID: p.ID, HeadingID: headingID, CreatedAt: now, ModifiedAt: now,
		})
	}
	for _, title := range n.Tasks {
		addTask(title, "")
	}
	for _, nh := range n.Headings {
		h := &domain.Heading{ID: b.newID("H"), Title: nh.Title, ProjectID: p.ID}
		b.headings = append(b.headings, h)
		for _, title := range nh.Tasks {
			addTask(title, h.ID)
		}
	}
	return cloneProject(p), nil
}

// UpdateTask implements backend.TaskBackend.
func (b *Backend) UpdateTask(_ context.Context, id string, p domain.TaskPatch) (domain.Task, error) {
	if err := p.Validate(); err != nil {
		return domain.Task{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	t, err := b.task(id)
	if err != nil {
		return domain.Task{}, err
	}
	// Compute the destination first so a failed move leaves the task untouched.
	projectID, areaID, headingID := t.ProjectID, t.AreaID, t.HeadingID
	if p.ProjectID != nil {
		projectID, areaID, headingID = *p.ProjectID, "", ""
	}
	if p.AreaID != nil {
		projectID, areaID, headingID = "", *p.AreaID, ""
	}
	if p.HeadingID != nil {
		headingID = *p.HeadingID
	}
	if err := b.checkLocation(projectID, areaID, headingID); err != nil {
		return domain.Task{}, err
	}
	tags, err := b.patchTags(t.Tags, p.Tags, p.AddTags)
	if err != nil {
		return domain.Task{}, err
	}

	t.ProjectID, t.AreaID, t.HeadingID, t.Tags = projectID, areaID, headingID, tags
	if p.Title != nil {
		t.Title = *p.Title
	}
	t.Notes = patchNotes(t.Notes, p.Notes, p.AppendNotes)
	if p.When != nil {
		t.When = *p.When
	}
	if p.Deadline != nil {
		t.Deadline = *p.Deadline
	}
	if p.Checklist != nil {
		t.Checklist = newChecklist(*p.Checklist)
	}
	t.Checklist = append(t.Checklist, newChecklist(p.AppendChecklist)...)
	t.ModifiedAt = b.now()
	return cloneTask(t), nil
}

// UpdateProject implements backend.TaskBackend.
func (b *Backend) UpdateProject(_ context.Context, id string, p domain.ProjectPatch) (domain.Project, error) {
	if err := p.Validate(); err != nil {
		return domain.Project{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	pr, err := b.project(id)
	if err != nil {
		return domain.Project{}, err
	}
	if p.AreaID != nil {
		if err := b.checkLocation("", *p.AreaID, ""); err != nil {
			return domain.Project{}, err
		}
	}
	tags, err := b.patchTags(pr.Tags, p.Tags, p.AddTags)
	if err != nil {
		return domain.Project{}, err
	}

	pr.Tags = tags
	if p.AreaID != nil {
		pr.AreaID = *p.AreaID
	}
	if p.Title != nil {
		pr.Title = *p.Title
	}
	pr.Notes = patchNotes(pr.Notes, p.Notes, p.AppendNotes)
	if p.When != nil {
		pr.When = *p.When
	}
	if p.Deadline != nil {
		pr.Deadline = *p.Deadline
	}
	pr.ModifiedAt = b.now()
	return cloneProject(pr), nil
}

// SetTaskStatus implements backend.TaskBackend.
func (b *Backend) SetTaskStatus(_ context.Context, id string, s domain.Status) (domain.Task, error) {
	if _, err := domain.ParseStatus(string(s)); err != nil {
		return domain.Task{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	t, err := b.task(id)
	if err != nil {
		return domain.Task{}, err
	}
	if t.Status != s {
		now := b.now()
		t.Status, t.ModifiedAt, t.CompletedAt = s, now, time.Time{}
		if s.Closed() {
			t.CompletedAt = now
		}
	}
	return cloneTask(t), nil
}

// MoveToTrash implements backend.TaskBackend. Trashing a project also trashes
// its tasks, as in Things.
func (b *Backend) MoveToTrash(_ context.Context, id string) error {
	if !b.caps.Trash {
		return domain.ErrUnsupported
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if t, err := b.task(id); err == nil {
		if !t.Trashed {
			t.Trashed, t.ModifiedAt = true, now
		}
		return nil
	}
	p, err := b.project(id)
	if err != nil {
		return domain.NotFound("task or project", id)
	}
	if !p.Trashed {
		p.Trashed, p.ModifiedAt = true, now
	}
	for _, t := range b.tasks {
		if t.ProjectID == id && !t.Trashed {
			t.Trashed, t.ModifiedAt = true, now
		}
	}
	return nil
}

// ShowInApp implements backend.TaskBackend by recording the call.
func (b *Backend) ShowInApp(_ context.Context, id string) error {
	if !b.caps.ShowInApp {
		return domain.ErrUnsupported
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	_, errT := b.task(id)
	_, errP := b.project(id)
	if errT != nil && errP != nil && b.area(id) == nil {
		return domain.NotFound("item", id)
	}
	b.shown = append(b.shown, id)
	return nil
}

// --- Helpers (callers hold b.mu) ---------------------------------------------

func (b *Backend) newID(prefix string) string {
	b.seq++
	return fmt.Sprintf("%s%06d", prefix, b.seq)
}

func (b *Backend) task(id string) (*domain.Task, error) {
	for _, t := range b.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, domain.NotFound("task", id)
}

func (b *Backend) project(id string) (*domain.Project, error) {
	for _, p := range b.projects {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, domain.NotFound("project", id)
}

func (b *Backend) area(id string) *domain.Area {
	for _, a := range b.areas {
		if a.ID == id {
			return a
		}
	}
	return nil
}

// areaOf returns the area of a task, directly or through its project.
func (b *Backend) areaOf(t *domain.Task) string {
	if t.AreaID != "" || t.ProjectID == "" {
		return t.AreaID
	}
	if p, err := b.project(t.ProjectID); err == nil {
		return p.AreaID
	}
	return ""
}

func (b *Backend) checkLocation(projectID, areaID, headingID string) error {
	if projectID != "" {
		if _, err := b.project(projectID); err != nil {
			return err
		}
	}
	if areaID != "" && b.area(areaID) == nil {
		return domain.NotFound("area", areaID)
	}
	if headingID != "" {
		for _, h := range b.headings {
			if h.ID == headingID {
				if h.ProjectID != projectID {
					return domain.Invalid("heading_id", "heading %q does not belong to project %q", headingID, projectID)
				}
				return nil
			}
		}
		return domain.NotFound("heading", headingID)
	}
	return nil
}

// resolveTags maps tag names (case-insensitive) to their canonical titles.
func (b *Backend) resolveTags(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, n := range names {
		var found string
		for _, t := range b.tags {
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

func (b *Backend) patchTags(current []string, replace *[]string, add []string) ([]string, error) {
	base := current
	if replace != nil {
		base = *replace
	}
	return b.resolveTags(append(slices.Clone(base), add...))
}

func patchNotes(current string, replace *string, appendText string) string {
	switch {
	case replace != nil:
		return *replace
	case appendText == "":
		return current
	case current == "":
		return appendText
	default:
		return current + "\n" + appendText
	}
}

func hasTag(tags []string, name string) bool {
	return slices.ContainsFunc(tags, func(t string) bool { return strings.EqualFold(t, name) })
}

func newChecklist(titles []string) []domain.ChecklistItem {
	items := make([]domain.ChecklistItem, 0, len(titles))
	for _, t := range titles {
		items = append(items, domain.ChecklistItem{Title: t, Status: domain.StatusOpen})
	}
	return items
}

func cloneTask(t *domain.Task) domain.Task {
	c := *t
	c.Tags = slices.Clone(t.Tags)
	c.Checklist = slices.Clone(t.Checklist)
	return c
}

func cloneProject(p *domain.Project) domain.Project {
	c := *p
	c.Tags = slices.Clone(p.Tags)
	return c
}
