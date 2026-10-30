// Package backendtest is a contract test suite shared by every TaskBackend
// implementation. The same scenarios run against the in-memory fake in CI and
// against real backends (local fixture, Things Cloud test account) later.
//
// Scenarios only assert on items they create themselves, so the suite stays
// valid on a backend that already contains data.
package backendtest

import (
	"context"
	"errors"
	"testing"

	"github.com/TitouanSC/Things3-MCP/internal/backend"
	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

// Harness is a backend under test plus the fixtures the API cannot create
// (areas and tags cannot be created through the URL scheme).
type Harness struct {
	Backend backend.TaskBackend
	Area    domain.Area // an existing area
	Tag     domain.Tag  // an existing tag
	Tag2    domain.Tag  // another existing tag
}

// Run executes the contract suite. newHarness is called once per scenario.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	scenarios := []struct {
		name string
		fn   func(t *testing.T, h Harness)
	}{
		{"CreateTaskRoundTrip", testCreateTaskRoundTrip},
		{"CreateTaskRejectsInvalidInput", testCreateTaskRejectsInvalidInput},
		{"CreateTaskUnknownReferences", testCreateTaskUnknownReferences},
		{"GetTaskUnknown", testGetTaskUnknown},
		{"Views", testViews},
		{"Pagination", testPagination},
		{"CreateProjectWithContent", testCreateProjectWithContent},
		{"ListProjectsFilters", testListProjectsFilters},
		{"ListTasksByArea", testListTasksByArea},
		{"UpdateTaskFields", testUpdateTaskFields},
		{"UpdateTaskMove", testUpdateTaskMove},
		{"UpdateTaskErrors", testUpdateTaskErrors},
		{"UpdateProject", testUpdateProject},
		{"SetTaskStatus", testSetTaskStatus},
		{"MoveToTrash", testMoveToTrash},
		{"Search", testSearch},
		{"AreasAndTags", testAreasAndTags},
	}
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) { s.fn(t, newHarness(t)) })
	}
}

var ctx = context.Background()

func mustCreateTask(t *testing.T, b backend.TaskBackend, n domain.NewTask) domain.Task {
	t.Helper()
	task, err := b.CreateTask(ctx, n)
	if err != nil {
		t.Fatalf("CreateTask(%q): %v", n.Title, err)
	}
	if b.Capabilities().ReturnsIDs && task.ID == "" {
		t.Fatalf("CreateTask(%q) returned no ID", n.Title)
	}
	return task
}

func mustCreateProject(t *testing.T, b backend.TaskBackend, n domain.NewProject) domain.Project {
	t.Helper()
	p, err := b.CreateProject(ctx, n)
	if err != nil {
		t.Fatalf("CreateProject(%q): %v", n.Title, err)
	}
	return p
}

func mustGetTask(t *testing.T, b backend.TaskBackend, id string) domain.Task {
	t.Helper()
	task, err := b.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("GetTask(%q): %v", id, err)
	}
	return task
}

// taskIDs walks every page of a listing and returns the IDs seen.
func taskIDs(t *testing.T, b backend.TaskBackend, q domain.TaskQuery) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	for pages := 0; ; pages++ {
		if pages > 1000 {
			t.Fatal("pagination does not terminate")
		}
		p, err := b.ListTasks(ctx, q)
		if err != nil {
			t.Fatalf("ListTasks(%+v): %v", q, err)
		}
		for _, task := range p.Items {
			if ids[task.ID] {
				t.Fatalf("ListTasks returned %q twice", task.ID)
			}
			ids[task.ID] = true
		}
		if p.NextCursor == "" {
			return ids
		}
		q.Cursor = p.NextCursor
	}
}

func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want %v", err, target)
	}
}

func testCreateTaskRoundTrip(t *testing.T, h Harness) {
	b := h.Backend
	created := mustCreateTask(t, b, domain.NewTask{
		Title:     "Contract: round trip",
		Notes:     "line 1\nline 2",
		When:      domain.When{Kind: domain.WhenSomeday},
		Deadline:  domain.Date{Year: 2099, Month: 1, Day: 2},
		Tags:      []string{h.Tag.Title},
		Checklist: []string{"first", "second"},
		AreaID:    h.Area.ID,
	})
	got := mustGetTask(t, b, created.ID)
	if got.Title != "Contract: round trip" || got.Notes != "line 1\nline 2" {
		t.Errorf("title/notes = %q / %q", got.Title, got.Notes)
	}
	if got.Status != domain.StatusOpen || got.Trashed {
		t.Errorf("status = %q, trashed = %v; want open, not trashed", got.Status, got.Trashed)
	}
	if got.When.Kind != domain.WhenSomeday || got.Deadline.String() != "2099-01-02" {
		t.Errorf("when/deadline = %v / %v", got.When, got.Deadline)
	}
	if len(got.Tags) != 1 || got.Tags[0] != h.Tag.Title {
		t.Errorf("tags = %v, want [%s]", got.Tags, h.Tag.Title)
	}
	if len(got.Checklist) != 2 || got.Checklist[1].Title != "second" || got.Checklist[0].Status != domain.StatusOpen {
		t.Errorf("checklist = %+v", got.Checklist)
	}
	if got.AreaID != h.Area.ID || got.CreatedAt.IsZero() {
		t.Errorf("area = %q, created = %v", got.AreaID, got.CreatedAt)
	}
}

func testCreateTaskRejectsInvalidInput(t *testing.T, h Harness) {
	_, err := h.Backend.CreateTask(ctx, domain.NewTask{Title: ""})
	wantErr(t, err, domain.ErrValidation)
}

func testCreateTaskUnknownReferences(t *testing.T, h Harness) {
	b := h.Backend
	_, err := b.CreateTask(ctx, domain.NewTask{Title: "x", ProjectID: "doesNotExist0000000000"})
	wantErr(t, err, domain.ErrNotFound)
	_, err = b.CreateTask(ctx, domain.NewTask{Title: "x", AreaID: "doesNotExist0000000000"})
	wantErr(t, err, domain.ErrNotFound)
	_, err = b.CreateTask(ctx, domain.NewTask{Title: "x", Tags: []string{"contract-no-such-tag"}})
	wantErr(t, err, domain.ErrNotFound)
}

func testGetTaskUnknown(t *testing.T, h Harness) {
	_, err := h.Backend.GetTask(ctx, "doesNotExist0000000000")
	wantErr(t, err, domain.ErrNotFound)
}

func testViews(t *testing.T, h Harness) {
	b := h.Backend
	inbox := mustCreateTask(t, b, domain.NewTask{Title: "Contract: inbox"})
	today := mustCreateTask(t, b, domain.NewTask{Title: "Contract: today", When: domain.When{Kind: domain.WhenToday}})
	someday := mustCreateTask(t, b, domain.NewTask{Title: "Contract: someday", When: domain.When{Kind: domain.WhenSomeday}})
	upcoming := mustCreateTask(t, b, domain.NewTask{Title: "Contract: upcoming",
		When: domain.When{Kind: domain.WhenDate, Date: domain.Date{Year: 2099, Month: 6, Day: 1}}})

	want := map[domain.View][]string{
		domain.ViewInbox:    {inbox.ID},
		domain.ViewToday:    {today.ID},
		domain.ViewAnytime:  {today.ID},
		domain.ViewSomeday:  {someday.ID},
		domain.ViewUpcoming: {upcoming.ID},
	}
	all := []string{inbox.ID, today.ID, someday.ID, upcoming.ID}
	for _, v := range domain.Views {
		ids := taskIDs(t, b, domain.TaskQuery{View: v})
		expected := map[string]bool{}
		for _, id := range want[v] {
			expected[id] = true
		}
		for _, id := range all {
			if ids[id] != expected[id] {
				t.Errorf("view %s: contains %s = %v, want %v", v, id, ids[id], expected[id])
			}
		}
	}
}

func testPagination(t *testing.T, h Harness) {
	b := h.Backend
	p := mustCreateProject(t, b, domain.NewProject{
		Title: "Contract: pagination",
		Tasks: []string{"p1", "p2", "p3", "p4", "p5"},
	})
	first, err := b.ListTasks(ctx, domain.TaskQuery{ProjectID: p.ID, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %d items, cursor %q; want 2 items and a cursor", len(first.Items), first.NextCursor)
	}
	if ids := taskIDs(t, b, domain.TaskQuery{ProjectID: p.ID, Limit: 2}); len(ids) != 5 {
		t.Fatalf("walked %d tasks, want 5", len(ids))
	}
	_, err = b.ListTasks(ctx, domain.TaskQuery{Cursor: "not-a-cursor"})
	wantErr(t, err, domain.ErrValidation)
}

func testCreateProjectWithContent(t *testing.T, h Harness) {
	b := h.Backend
	p := mustCreateProject(t, b, domain.NewProject{
		Title:    "Contract: project",
		Notes:    "project notes",
		Tags:     []string{h.Tag.Title},
		AreaID:   h.Area.ID,
		Tasks:    []string{"loose task"},
		Headings: []domain.NewHeading{{Title: "Phase A", Tasks: []string{"a1", "a2"}}},
	})
	d, err := b.GetProject(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Project.Title != "Contract: project" || d.Project.AreaID != h.Area.ID || d.Project.Status != domain.StatusOpen {
		t.Errorf("project = %+v", d.Project)
	}
	if len(d.Headings) != 1 || d.Headings[0].Title != "Phase A" {
		t.Fatalf("headings = %+v", d.Headings)
	}
	if len(d.Tasks) != 3 {
		t.Fatalf("tasks = %d, want 3", len(d.Tasks))
	}
	underHeading := 0
	for _, task := range d.Tasks {
		if task.ProjectID != p.ID {
			t.Errorf("task %q project = %q", task.Title, task.ProjectID)
		}
		if task.HeadingID == d.Headings[0].ID {
			underHeading++
		}
	}
	if underHeading != 2 {
		t.Errorf("%d tasks under heading, want 2", underHeading)
	}
	_, err = b.GetProject(ctx, "doesNotExist0000000000")
	wantErr(t, err, domain.ErrNotFound)
}

func testListProjectsFilters(t *testing.T, h Harness) {
	b := h.Backend
	inArea := mustCreateProject(t, b, domain.NewProject{Title: "Contract: in area", AreaID: h.Area.ID})
	loose := mustCreateProject(t, b, domain.NewProject{Title: "Contract: no area"})
	seen := func(q domain.ProjectQuery) map[string]bool {
		ids := map[string]bool{}
		for {
			p, err := b.ListProjects(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			for _, pr := range p.Items {
				ids[pr.ID] = true
			}
			if p.NextCursor == "" {
				return ids
			}
			q.Cursor = p.NextCursor
		}
	}
	all := seen(domain.ProjectQuery{})
	if !all[inArea.ID] || !all[loose.ID] {
		t.Errorf("open projects missing created ones: %v", all)
	}
	area := seen(domain.ProjectQuery{AreaID: h.Area.ID})
	if !area[inArea.ID] || area[loose.ID] {
		t.Errorf("area filter wrong: %v", area)
	}
	if closed := seen(domain.ProjectQuery{Status: domain.StatusCompleted}); closed[inArea.ID] {
		t.Error("open project listed as completed")
	}
}

func testListTasksByArea(t *testing.T, h Harness) {
	b := h.Backend
	direct := mustCreateTask(t, b, domain.NewTask{Title: "Contract: direct", AreaID: h.Area.ID})
	p := mustCreateProject(t, b, domain.NewProject{Title: "Contract: area project", AreaID: h.Area.ID, Tasks: []string{"nested"}})
	outside := mustCreateTask(t, b, domain.NewTask{Title: "Contract: outside"})
	ids := taskIDs(t, b, domain.TaskQuery{AreaID: h.Area.ID})
	if !ids[direct.ID] || ids[outside.ID] {
		t.Errorf("area listing = %v", ids)
	}
	d, err := b.GetProject(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ids[d.Tasks[0].ID] {
		t.Error("task of a project in the area is missing from the area listing")
	}
}

func testUpdateTaskFields(t *testing.T, h Harness) {
	b := h.Backend
	task := mustCreateTask(t, b, domain.NewTask{
		Title: "Contract: before", Notes: "first", Tags: []string{h.Tag.Title},
		Checklist: []string{"c1"}, When: domain.When{Kind: domain.WhenToday},
		Deadline: domain.Date{Year: 2099, Month: 1, Day: 1},
	})
	title := "Contract: after"
	got, err := b.UpdateTask(ctx, task.ID, domain.TaskPatch{
		Title: &title, AppendNotes: "second", AddTags: []string{h.Tag2.Title},
		AppendChecklist: []string{"c2"}, When: &domain.When{}, Deadline: &domain.Date{},
	})
	if err != nil {
		t.Fatal(err)
	}
	got = mustGetTask(t, b, task.ID)
	if got.Title != title || got.Notes != "first\nsecond" {
		t.Errorf("title/notes = %q / %q", got.Title, got.Notes)
	}
	if len(got.Tags) != 2 || len(got.Checklist) != 2 {
		t.Errorf("tags = %v, checklist = %+v", got.Tags, got.Checklist)
	}
	if !got.When.IsZero() || !got.Deadline.IsZero() {
		t.Errorf("when/deadline not cleared: %v / %v", got.When, got.Deadline)
	}

	notes := "replaced"
	got, err = b.UpdateTask(ctx, task.ID, domain.TaskPatch{Notes: &notes, Tags: &[]string{}, Checklist: &[]string{"only"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Notes != "replaced" || len(got.Tags) != 0 || len(got.Checklist) != 1 {
		t.Errorf("replace: notes %q, tags %v, checklist %+v", got.Notes, got.Tags, got.Checklist)
	}
}

func testUpdateTaskMove(t *testing.T, h Harness) {
	b := h.Backend
	p := mustCreateProject(t, b, domain.NewProject{Title: "Contract: target",
		Headings: []domain.NewHeading{{Title: "H"}}})
	other := mustCreateProject(t, b, domain.NewProject{Title: "Contract: other",
		Headings: []domain.NewHeading{{Title: "OH"}}})
	d, _ := b.GetProject(ctx, p.ID)
	od, _ := b.GetProject(ctx, other.ID)
	task := mustCreateTask(t, b, domain.NewTask{Title: "Contract: mover"})

	got, err := b.UpdateTask(ctx, task.ID, domain.TaskPatch{ProjectID: &p.ID, HeadingID: &d.Headings[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != p.ID || got.HeadingID != d.Headings[0].ID || got.AreaID != "" {
		t.Errorf("after move to project: %+v", got)
	}

	_, err = b.UpdateTask(ctx, task.ID, domain.TaskPatch{HeadingID: &od.Headings[0].ID})
	wantErr(t, err, domain.ErrValidation)

	got, err = b.UpdateTask(ctx, task.ID, domain.TaskPatch{AreaID: &h.Area.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.AreaID != h.Area.ID || got.ProjectID != "" || got.HeadingID != "" {
		t.Errorf("after move to area: %+v", got)
	}
}

func testUpdateTaskErrors(t *testing.T, h Harness) {
	b := h.Backend
	title := "x"
	_, err := b.UpdateTask(ctx, "doesNotExist0000000000", domain.TaskPatch{Title: &title})
	wantErr(t, err, domain.ErrNotFound)
	task := mustCreateTask(t, b, domain.NewTask{Title: "Contract: errors"})
	_, err = b.UpdateTask(ctx, task.ID, domain.TaskPatch{})
	wantErr(t, err, domain.ErrValidation)
	missing := "doesNotExist0000000000"
	_, err = b.UpdateTask(ctx, task.ID, domain.TaskPatch{ProjectID: &missing})
	wantErr(t, err, domain.ErrNotFound)
	if got := mustGetTask(t, b, task.ID); got.ProjectID != "" {
		t.Error("failed move modified the task")
	}
}

func testUpdateProject(t *testing.T, h Harness) {
	b := h.Backend
	p := mustCreateProject(t, b, domain.NewProject{Title: "Contract: p before", Notes: "n"})
	title := "Contract: p after"
	got, err := b.UpdateProject(ctx, p.ID, domain.ProjectPatch{
		Title: &title, AppendNotes: "more", AddTags: []string{h.Tag.Title}, AreaID: &h.Area.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != title || got.Notes != "n\nmore" || got.AreaID != h.Area.ID || len(got.Tags) != 1 {
		t.Errorf("updated project = %+v", got)
	}
	_, err = b.UpdateProject(ctx, "doesNotExist0000000000", domain.ProjectPatch{Title: &title})
	wantErr(t, err, domain.ErrNotFound)
}

func testSetTaskStatus(t *testing.T, h Harness) {
	b := h.Backend
	task := mustCreateTask(t, b, domain.NewTask{Title: "Contract: status", When: domain.When{Kind: domain.WhenToday}})
	got, err := b.SetTaskStatus(ctx, task.ID, domain.StatusCompleted)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusCompleted || got.CompletedAt.IsZero() {
		t.Errorf("after complete: status %q, completed_at %v", got.Status, got.CompletedAt)
	}
	if !taskIDs(t, b, domain.TaskQuery{View: domain.ViewLogbook})[task.ID] {
		t.Error("completed task missing from logbook")
	}
	if taskIDs(t, b, domain.TaskQuery{View: domain.ViewToday})[task.ID] {
		t.Error("completed task still in today")
	}
	// Idempotent.
	if _, err := b.SetTaskStatus(ctx, task.ID, domain.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	got, err = b.SetTaskStatus(ctx, task.ID, domain.StatusOpen)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusOpen || !got.CompletedAt.IsZero() {
		t.Errorf("after reopen: %q %v", got.Status, got.CompletedAt)
	}
	_, err = b.SetTaskStatus(ctx, task.ID, "done")
	wantErr(t, err, domain.ErrValidation)
	_, err = b.SetTaskStatus(ctx, "doesNotExist0000000000", domain.StatusOpen)
	wantErr(t, err, domain.ErrNotFound)
}

func testMoveToTrash(t *testing.T, h Harness) {
	b := h.Backend
	task := mustCreateTask(t, b, domain.NewTask{Title: "Contract: trash me", When: domain.When{Kind: domain.WhenToday}})
	if !b.Capabilities().Trash {
		wantErr(t, b.MoveToTrash(ctx, task.ID), domain.ErrUnsupported)
		return
	}
	for range 2 { // idempotent
		if err := b.MoveToTrash(ctx, task.ID); err != nil {
			t.Fatal(err)
		}
	}
	if got := mustGetTask(t, b, task.ID); !got.Trashed {
		t.Error("task not flagged as trashed")
	}
	if !taskIDs(t, b, domain.TaskQuery{View: domain.ViewTrash})[task.ID] {
		t.Error("trashed task missing from trash view")
	}
	if taskIDs(t, b, domain.TaskQuery{View: domain.ViewToday})[task.ID] {
		t.Error("trashed task still in today")
	}

	p := mustCreateProject(t, b, domain.NewProject{Title: "Contract: trash project", Tasks: []string{"child"}})
	if err := b.MoveToTrash(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if open, _ := b.ListProjects(ctx, domain.ProjectQuery{Limit: domain.MaxPageLimit}); containsProject(open.Items, p.ID) {
		t.Error("trashed project still listed")
	}
	wantErr(t, b.MoveToTrash(ctx, "doesNotExist0000000000"), domain.ErrNotFound)
}

func containsProject(ps []domain.Project, id string) bool {
	for _, p := range ps {
		if p.ID == id {
			return true
		}
	}
	return false
}

func testSearch(t *testing.T, h Harness) {
	b := h.Backend
	byTitle := mustCreateTask(t, b, domain.NewTask{Title: "Contract: Zebra quarterly"})
	byNotes := mustCreateTask(t, b, domain.NewTask{Title: "Contract: plain", Notes: "talk about the ZEBRA"})
	proj := mustCreateProject(t, b, domain.NewProject{Title: "Contract: zebra project"})

	hits := func(q domain.SearchQuery) map[string]domain.ItemKind {
		res, err := b.Search(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]domain.ItemKind{}
		for _, h := range res.Items {
			out[h.ID] = h.Kind
		}
		return out
	}
	all := hits(domain.SearchQuery{Text: "zebra", Limit: domain.MaxPageLimit})
	if all[byTitle.ID] != domain.KindTask || all[byNotes.ID] != domain.KindTask || all[proj.ID] != domain.KindProject {
		t.Errorf("search hits = %v", all)
	}
	onlyProjects := hits(domain.SearchQuery{Text: "zebra", Kind: domain.KindProject, Limit: domain.MaxPageLimit})
	if _, ok := onlyProjects[byTitle.ID]; ok {
		t.Error("kind filter ignored")
	}
	if b.Capabilities().Trash {
		if err := b.MoveToTrash(ctx, byTitle.ID); err != nil {
			t.Fatal(err)
		}
		if _, ok := hits(domain.SearchQuery{Text: "zebra", Limit: domain.MaxPageLimit})[byTitle.ID]; ok {
			t.Error("trashed task returned by search")
		}
	}
	_, err := b.Search(ctx, domain.SearchQuery{Text: ""})
	wantErr(t, err, domain.ErrValidation)
}

func testAreasAndTags(t *testing.T, h Harness) {
	areas, err := h.Backend.ListAreas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range areas {
		found = found || a.ID == h.Area.ID
	}
	if !found {
		t.Error("fixture area not listed")
	}
	tags, err := h.Backend.ListTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, tg := range tags {
		found = found || tg.ID == h.Tag.ID
	}
	if !found {
		t.Error("fixture tag not listed")
	}
}
