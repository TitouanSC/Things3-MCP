package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TitouanSC/Things3-MCP/internal/backend"
	"github.com/TitouanSC/Things3-MCP/internal/backend/fake"
	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

var fixedNow = time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

type env struct {
	cs   *mcp.ClientSession
	fake *fake.Backend
	area domain.Area
}

// connect starts the server over in-memory transports and returns a client
// session, exercising the full MCP protocol path (schemas, validation, JSON).
func connect(t *testing.T, b backend.TaskBackend) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv := New(b, Options{Now: func() time.Time { return fixedNow }})
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func newEnv(t *testing.T, opts ...fake.Option) env {
	t.Helper()
	f := fake.New(append([]fake.Option{fake.WithClock(func() time.Time { return fixedNow })}, opts...)...)
	area := f.SeedArea("Work")
	f.SeedTag("errand", "")
	f.SeedTag("home", "")
	return env{cs: connect(t, f), fake: f, area: area}
}

// call invokes a tool and decodes its structured output into T. It fails the
// test if the tool returns an error.
func call[T any](t *testing.T, e env, name string, args map[string]any) T {
	t.Helper()
	res, err := e.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s: tool error: %s", name, text(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: decoding %s: %v", name, raw, err)
	}
	return out
}

// callErr invokes a tool that must fail and returns the error text.
func callErr(t *testing.T, e env, name string, args map[string]any) string {
	t.Helper()
	res, err := e.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error()
	}
	if !res.IsError {
		t.Fatalf("%s: expected an error, got %s", name, text(res))
	}
	return text(res)
}

func text(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func tools(t *testing.T, cs *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		out[tool.Name] = tool
	}
	return out
}

func TestToolCatalog(t *testing.T) {
	e := newEnv(t)
	got := tools(t, e.cs)

	readOnlyTools := []string{"list_tasks", "get_task", "list_projects", "get_project", "list_areas", "list_tags", "search", "show_in_things"}
	writeTools := map[string]struct{ destructive, idempotent bool }{
		"create_task":     {false, false},
		"create_project":  {false, false},
		"update_task":     {true, false},
		"update_project":  {true, false},
		"set_task_status": {false, true},
		"move_to_trash":   {true, true},
	}
	if len(got) != len(readOnlyTools)+len(writeTools) {
		t.Errorf("got %d tools, want %d", len(got), len(readOnlyTools)+len(writeTools))
	}
	for name, tool := range got {
		if tool.Description == "" || tool.Annotations == nil || tool.Annotations.Title == "" {
			t.Errorf("%s: missing description or annotations", name)
		}
		if tool.Annotations != nil && (tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint) {
			t.Errorf("%s: openWorldHint should be false", name)
		}
	}
	for _, name := range readOnlyTools {
		if tool, ok := got[name]; !ok || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s: missing or not read-only", name)
		}
	}
	for name, want := range writeTools {
		tool, ok := got[name]
		if !ok {
			t.Errorf("%s: missing", name)
			continue
		}
		a := tool.Annotations
		if a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint != want.destructive || a.IdempotentHint != want.idempotent {
			t.Errorf("%s: annotations = %+v, want destructive=%v idempotent=%v", name, a, want.destructive, want.idempotent)
		}
	}
}

func TestToolsDependOnCapabilities(t *testing.T) {
	e := newEnv(t, fake.WithCapabilities(backend.Capabilities{ReturnsIDs: true}))
	got := tools(t, e.cs)
	for _, name := range []string{"move_to_trash", "show_in_things"} {
		if _, ok := got[name]; ok {
			t.Errorf("%s registered without its capability", name)
		}
	}
	if len(got) != 12 {
		t.Errorf("got %d tools, want 12", len(got))
	}
}

func TestEnumsInSchemas(t *testing.T) {
	e := newEnv(t)
	schema, _ := json.Marshal(tools(t, e.cs)["list_tasks"].InputSchema)
	for _, v := range domain.Views {
		if !strings.Contains(string(schema), `"`+string(v)+`"`) {
			t.Errorf("list_tasks schema lacks view %q: %s", v, schema)
		}
	}
}

func TestCreateAndGetTask(t *testing.T) {
	e := newEnv(t)
	created := call[TaskDetail](t, e, "create_task", map[string]any{
		"title": "Call the bank", "notes": "about the loan", "when": "tomorrow@9:30",
		"deadline": "2026-11-15", "tags": []string{"Errand"}, "checklist": []string{"find IBAN"},
		"area_id": e.area.ID,
	})
	got := call[TaskDetail](t, e, "get_task", map[string]any{"id": created.ID})
	if got.Title != "Call the bank" || got.Notes != "about the loan" || got.AreaID != e.area.ID {
		t.Errorf("task = %+v", got)
	}
	if got.When != "2026-10-31@09:30" || got.Deadline != "2026-11-15" {
		t.Errorf("when/deadline = %q / %q", got.When, got.Deadline)
	}
	if !slices.Equal(got.Tags, []string{"errand"}) || len(got.Checklist) != 1 || got.Status != "open" {
		t.Errorf("tags %v, checklist %+v, status %q", got.Tags, got.Checklist, got.Status)
	}
	if got.CreatedAt != "2026-10-30T09:00:00Z" {
		t.Errorf("created_at = %q", got.CreatedAt)
	}
}

func TestInputErrors(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"create_task", map[string]any{"title": "x", "when": "next monday"}, "invalid input"},
		{"create_task", map[string]any{"title": "x", "deadline": "tomorrow"}, "invalid input"},
		{"create_task", map[string]any{"title": "x", "tags": []string{"nope"}}, "not found"},
		{"create_task", map[string]any{"notes": "missing title"}, "title"},
		{"get_task", map[string]any{"id": "T999999"}, "not found"},
		{"get_task", map[string]any{"id": `x" & do shell script "id`}, "invalid input"},
		{"update_task", map[string]any{"id": "T999999"}, "nothing to update"},
		{"set_task_status", map[string]any{"id": "T1", "status": "done"}, "status"},
		{"list_tasks", map[string]any{"view": "later"}, "view"},
		{"list_tasks", map[string]any{"cursor": "garbage"}, "cursor"},
		{"search", map[string]any{"query": ""}, "query"},
	}
	for _, c := range cases {
		t.Run(c.tool+"/"+c.want, func(t *testing.T) {
			if msg := callErr(t, e, c.tool, c.args); !strings.Contains(msg, c.want) {
				t.Errorf("error %q does not mention %q", msg, c.want)
			}
		})
	}
}

func TestUpdateTaskAppendAndClear(t *testing.T) {
	e := newEnv(t)
	task := call[TaskDetail](t, e, "create_task", map[string]any{
		"title": "Draft", "notes": "v1", "when": "today", "deadline": "2026-12-01", "tags": []string{"home"},
	})
	got := call[TaskDetail](t, e, "update_task", map[string]any{
		"id": task.ID, "append_notes": "v2", "add_tags": []string{"errand"}, "when": "", "deadline": "",
	})
	if got.Notes != "v1\nv2" || len(got.Tags) != 2 || got.When != "" || got.Deadline != "" {
		t.Errorf("after update = %+v", got)
	}
	got = call[TaskDetail](t, e, "update_task", map[string]any{"id": task.ID, "tags": []string{}})
	if len(got.Tags) != 0 {
		t.Errorf("tags not replaced: %v", got.Tags)
	}
	msg := callErr(t, e, "update_task", map[string]any{"id": task.ID, "notes": "x", "append_notes": "y"})
	if !strings.Contains(msg, "cannot be combined") {
		t.Errorf("error = %q", msg)
	}
}

func TestProjectLifecycle(t *testing.T) {
	e := newEnv(t)
	p := call[ProjectSummary](t, e, "create_project", map[string]any{
		"title": "Move house", "area_id": e.area.ID, "tasks": []string{"book van"},
		"headings": []map[string]any{{"title": "Packing", "tasks": []string{"boxes", "tape"}}},
	})
	d := call[ProjectDetail](t, e, "get_project", map[string]any{"id": p.ID})
	if len(d.Headings) != 1 || len(d.Tasks) != 3 || d.AreaID != e.area.ID {
		t.Fatalf("project detail = %+v", d)
	}
	list := call[ProjectList](t, e, "list_projects", map[string]any{"area_id": e.area.ID})
	if len(list.Projects) != 1 || list.Projects[0].ID != p.ID {
		t.Errorf("list_projects = %+v", list)
	}
	up := call[ProjectSummary](t, e, "update_project", map[string]any{"id": p.ID, "title": "Move flat", "add_tags": []string{"home"}})
	if up.Title != "Move flat" || len(up.Tags) != 1 {
		t.Errorf("update_project = %+v", up)
	}
}

func TestListTasksPagination(t *testing.T) {
	e := newEnv(t)
	for _, title := range []string{"a", "b", "c"} {
		call[TaskDetail](t, e, "create_task", map[string]any{"title": title})
	}
	first := call[TaskList](t, e, "list_tasks", map[string]any{"view": "inbox", "limit": 2})
	if len(first.Tasks) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	second := call[TaskList](t, e, "list_tasks", map[string]any{"view": "inbox", "limit": 2, "cursor": first.NextCursor})
	if len(second.Tasks) != 1 || second.NextCursor != "" {
		t.Fatalf("second page = %+v", second)
	}
}

func TestStatusTrashAndShow(t *testing.T) {
	e := newEnv(t)
	task := call[TaskDetail](t, e, "create_task", map[string]any{"title": "Pay rent", "when": "today"})

	done := call[TaskSummary](t, e, "set_task_status", map[string]any{"id": task.ID, "status": "completed"})
	if done.Status != "completed" {
		t.Errorf("status = %q", done.Status)
	}
	logbook := call[TaskList](t, e, "list_tasks", map[string]any{"view": "logbook"})
	if len(logbook.Tasks) != 1 {
		t.Errorf("logbook = %+v", logbook)
	}

	trash := call[TrashResult](t, e, "move_to_trash", map[string]any{"id": task.ID})
	if !trash.Trashed || !strings.Contains(trash.Message, "restored") {
		t.Errorf("move_to_trash = %+v", trash)
	}
	if got := call[TaskDetail](t, e, "get_task", map[string]any{"id": task.ID}); !got.Trashed {
		t.Error("task not trashed")
	}

	call[ShowResult](t, e, "show_in_things", map[string]any{"id": task.ID})
	if shown := e.fake.Shown(); len(shown) != 1 || shown[0] != task.ID {
		t.Errorf("Shown() = %v", shown)
	}
}

func TestSearchAndCatalogs(t *testing.T) {
	e := newEnv(t)
	call[TaskDetail](t, e, "create_task", map[string]any{"title": "Renew passport"})
	call[ProjectSummary](t, e, "create_project", map[string]any{"title": "Passport trip"})
	res := call[SearchResult](t, e, "search", map[string]any{"query": "PASSPORT"})
	if len(res.Hits) != 2 {
		t.Errorf("search = %+v", res)
	}
	res = call[SearchResult](t, e, "search", map[string]any{"query": "passport", "kind": "project"})
	if len(res.Hits) != 1 || res.Hits[0].Kind != "project" {
		t.Errorf("search kind=project = %+v", res)
	}
	if areas := call[AreaList](t, e, "list_areas", nil); len(areas.Areas) != 1 {
		t.Errorf("list_areas = %+v", areas)
	}
	if tags := call[TagList](t, e, "list_tags", nil); len(tags.Tags) != 2 {
		t.Errorf("list_tags = %+v", tags)
	}
}

// failingBackend simulates a backend leaking internal details in its errors.
type failingBackend struct{ *fake.Backend }

func (failingBackend) GetTask(context.Context, string) (domain.Task, error) {
	return domain.Task{}, errors.New("sqlite: open /Users/someone/Library/secret.db: permission denied")
}

func (failingBackend) ListAreas(context.Context) ([]domain.Area, error) {
	return nil, domain.ErrUnavailable
}

func TestInternalErrorsAreMasked(t *testing.T) {
	e := env{cs: connect(t, failingBackend{fake.New()})}
	msg := callErr(t, e, "get_task", map[string]any{"id": "T1"})
	if msg != "internal error" {
		t.Errorf("error = %q, want a generic message", msg)
	}
	if msg := callErr(t, e, "list_areas", nil); !strings.Contains(msg, "unavailable") {
		t.Errorf("error = %q", msg)
	}
}
