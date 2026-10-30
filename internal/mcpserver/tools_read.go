package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

// IDInput identifies a single item.
type IDInput struct {
	ID string `json:"id" jsonschema:"Things ID of the item"`
}

// ListTasksInput filters list_tasks.
type ListTasksInput struct {
	View      string `json:"view,omitempty" jsonschema:"built-in Things list; omit to list every open task"`
	ProjectID string `json:"project_id,omitempty" jsonschema:"only tasks of this project"`
	AreaID    string `json:"area_id,omitempty" jsonschema:"only tasks of this area, directly or through one of its projects"`
	Tag       string `json:"tag,omitempty" jsonschema:"only tasks with this tag name"`
	Limit     int    `json:"limit,omitempty" jsonschema:"page size, 1 to 200, default 50"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous call"`
}

// ListProjectsInput filters list_projects.
type ListProjectsInput struct {
	AreaID string `json:"area_id,omitempty" jsonschema:"only projects of this area"`
	Status string `json:"status,omitempty" jsonschema:"default open"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size, 1 to 200, default 50"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous call"`
}

// SearchInput configures search.
type SearchInput struct {
	Query  string `json:"query" jsonschema:"case-insensitive text matched against titles and notes"`
	Kind   string `json:"kind,omitempty" jsonschema:"restrict to tasks or projects"`
	Status string `json:"status,omitempty" jsonschema:"restrict to a status; default any"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size, 1 to 200, default 50"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous call"`
}

// NoInput is used by tools without parameters.
type NoInput struct{}

func (s *server) registerReadTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_tasks",
		Description: "List to-dos, optionally from a built-in Things list (inbox, today, upcoming, anytime, someday, logbook, trash) and filtered by project, area or tag. Returns compact summaries without notes; use get_task for details. Paginated: follow next_cursor.",
		InputSchema: schemaFor[ListTasksInput](map[string][]string{"view": viewNames()}),
		Annotations: readOnly("List tasks"),
	}, handle(s, "list_tasks", s.listTasks))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_task",
		Description: "Get one to-do with its notes, checklist, tags, schedule and location (project, heading, area).",
		Annotations: readOnly("Get task"),
	}, handle(s, "get_task", s.getTask))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_projects",
		Description: "List projects (open by default), optionally within an area. Paginated: follow next_cursor.",
		InputSchema: schemaFor[ListProjectsInput](map[string][]string{"status": statusNames}),
		Annotations: readOnly("List projects"),
	}, handle(s, "list_projects", s.listProjects))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_project",
		Description: "Get one project with its notes, headings and to-dos (trashed to-dos excluded).",
		Annotations: readOnly("Get project"),
	}, handle(s, "get_project", s.getProject))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_areas",
		Description: "List all areas of responsibility.",
		Annotations: readOnly("List areas"),
	}, handle(s, "list_areas", s.listAreas))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_tags",
		Description: "List all tags. Tags must already exist to be used on tasks or projects.",
		Annotations: readOnly("List tags"),
	}, handle(s, "list_tags", s.listTags))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search",
		Description: "Search to-dos and projects whose title or notes contain the query (case-insensitive). Trashed items are excluded. Paginated: follow next_cursor.",
		InputSchema: schemaFor[SearchInput](map[string][]string{
			"kind":   {string(domain.KindTask), string(domain.KindProject)},
			"status": statusNames,
		}),
		Annotations: readOnly("Search"),
	}, handle(s, "search", s.search))
}

func (s *server) listTasks(ctx context.Context, in ListTasksInput) (TaskList, error) {
	page, err := s.b.ListTasks(ctx, domain.TaskQuery{
		View: domain.View(in.View), ProjectID: in.ProjectID, AreaID: in.AreaID,
		Tag: in.Tag, Limit: in.Limit, Cursor: in.Cursor,
	})
	if err != nil {
		return TaskList{}, err
	}
	return TaskList{Tasks: toTaskSummaries(page.Items), NextCursor: page.NextCursor}, nil
}

func (s *server) getTask(ctx context.Context, in IDInput) (TaskDetail, error) {
	if err := domain.ValidateID("id", in.ID); err != nil {
		return TaskDetail{}, err
	}
	t, err := s.b.GetTask(ctx, in.ID)
	if err != nil {
		return TaskDetail{}, err
	}
	return toTaskDetail(t), nil
}

func (s *server) listProjects(ctx context.Context, in ListProjectsInput) (ProjectList, error) {
	page, err := s.b.ListProjects(ctx, domain.ProjectQuery{
		AreaID: in.AreaID, Status: domain.Status(in.Status), Limit: in.Limit, Cursor: in.Cursor,
	})
	if err != nil {
		return ProjectList{}, err
	}
	out := ProjectList{Projects: make([]ProjectSummary, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, p := range page.Items {
		out.Projects = append(out.Projects, toProjectSummary(p))
	}
	return out, nil
}

func (s *server) getProject(ctx context.Context, in IDInput) (ProjectDetail, error) {
	if err := domain.ValidateID("id", in.ID); err != nil {
		return ProjectDetail{}, err
	}
	d, err := s.b.GetProject(ctx, in.ID)
	if err != nil {
		return ProjectDetail{}, err
	}
	return toProjectDetail(d), nil
}

func (s *server) listAreas(ctx context.Context, _ NoInput) (AreaList, error) {
	areas, err := s.b.ListAreas(ctx)
	if err != nil {
		return AreaList{}, err
	}
	out := AreaList{Areas: make([]Area, 0, len(areas))}
	for _, a := range areas {
		out.Areas = append(out.Areas, Area{ID: a.ID, Title: a.Title, Tags: a.Tags})
	}
	return out, nil
}

func (s *server) listTags(ctx context.Context, _ NoInput) (TagList, error) {
	tags, err := s.b.ListTags(ctx)
	if err != nil {
		return TagList{}, err
	}
	out := TagList{Tags: make([]Tag, 0, len(tags))}
	for _, t := range tags {
		out.Tags = append(out.Tags, Tag{ID: t.ID, Title: t.Title, ParentID: t.ParentID})
	}
	return out, nil
}

func (s *server) search(ctx context.Context, in SearchInput) (SearchResult, error) {
	page, err := s.b.Search(ctx, domain.SearchQuery{
		Text: in.Query, Kind: domain.ItemKind(in.Kind), Status: domain.Status(in.Status),
		Limit: in.Limit, Cursor: in.Cursor,
	})
	if err != nil {
		return SearchResult{}, err
	}
	out := SearchResult{Hits: make([]SearchHit, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, h := range page.Items {
		out.Hits = append(out.Hits, SearchHit{Kind: string(h.Kind), ID: h.ID, Title: h.Title, Status: string(h.Status)})
	}
	return out, nil
}
