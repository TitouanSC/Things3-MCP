package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TitouanSC/Things3-MCP/internal/backend"
	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

const whenHelp = "today, evening, tomorrow, anytime, someday or YYYY-MM-DD; append @HH:MM (24h) to add a reminder, e.g. 2026-11-03@09:30"

// CreateTaskInput describes create_task.
type CreateTaskInput struct {
	Title     string   `json:"title" jsonschema:"single line, max 4000 characters"`
	Notes     string   `json:"notes,omitempty" jsonschema:"max 10000 characters"`
	When      string   `json:"when,omitempty" jsonschema:"today, evening, tomorrow, anytime, someday or YYYY-MM-DD; append @HH:MM (24h) to add a reminder. Omit to put the task in the Inbox (or Anytime inside a project or area)"`
	Deadline  string   `json:"deadline,omitempty" jsonschema:"YYYY-MM-DD"`
	Tags      []string `json:"tags,omitempty" jsonschema:"names of existing tags (see list_tags)"`
	Checklist []string `json:"checklist,omitempty" jsonschema:"checklist item titles, max 100"`
	ProjectID string   `json:"project_id,omitempty" jsonschema:"create inside this project"`
	AreaID    string   `json:"area_id,omitempty" jsonschema:"create inside this area (not with project_id)"`
	HeadingID string   `json:"heading_id,omitempty" jsonschema:"heading of project_id to file the task under"`
}

// HeadingInput is a heading created with a project.
type HeadingInput struct {
	Title string   `json:"title"`
	Tasks []string `json:"tasks,omitempty" jsonschema:"titles of to-dos under this heading"`
}

// CreateProjectInput describes create_project.
type CreateProjectInput struct {
	Title    string         `json:"title" jsonschema:"single line, max 4000 characters"`
	Notes    string         `json:"notes,omitempty" jsonschema:"max 10000 characters"`
	When     string         `json:"when,omitempty" jsonschema:"today, evening, tomorrow, anytime, someday or YYYY-MM-DD, optional @HH:MM reminder"`
	Deadline string         `json:"deadline,omitempty" jsonschema:"YYYY-MM-DD"`
	Tags     []string       `json:"tags,omitempty" jsonschema:"names of existing tags"`
	AreaID   string         `json:"area_id,omitempty" jsonschema:"create inside this area"`
	Tasks    []string       `json:"tasks,omitempty" jsonschema:"titles of to-dos at the top of the project"`
	Headings []HeadingInput `json:"headings,omitempty" jsonschema:"headings, each with its own to-dos"`
}

// UpdateTaskInput describes update_task. Omitted fields are left unchanged.
type UpdateTaskInput struct {
	ID              string    `json:"id" jsonschema:"ID of the to-do to update"`
	Title           *string   `json:"title,omitempty" jsonschema:"new title"`
	Notes           *string   `json:"notes,omitempty" jsonschema:"REPLACES the notes; empty string clears them"`
	AppendNotes     string    `json:"append_notes,omitempty" jsonschema:"text added on a new line after the current notes"`
	When            *string   `json:"when,omitempty" jsonschema:"new schedule (today, evening, tomorrow, anytime, someday, YYYY-MM-DD, optional @HH:MM); empty string clears it"`
	Deadline        *string   `json:"deadline,omitempty" jsonschema:"YYYY-MM-DD; empty string clears it"`
	Tags            *[]string `json:"tags,omitempty" jsonschema:"REPLACES all tags; empty array removes them"`
	AddTags         []string  `json:"add_tags,omitempty" jsonschema:"tags added to the current ones"`
	Checklist       *[]string `json:"checklist,omitempty" jsonschema:"REPLACES the whole checklist"`
	AppendChecklist []string  `json:"append_checklist,omitempty" jsonschema:"checklist items added at the end"`
	ProjectID       *string   `json:"project_id,omitempty" jsonschema:"move to this project"`
	AreaID          *string   `json:"area_id,omitempty" jsonschema:"move to this area"`
	HeadingID       *string   `json:"heading_id,omitempty" jsonschema:"move under this heading of the task's (new) project"`
}

// UpdateProjectInput describes update_project. Omitted fields are left unchanged.
type UpdateProjectInput struct {
	ID          string    `json:"id" jsonschema:"ID of the project to update"`
	Title       *string   `json:"title,omitempty"`
	Notes       *string   `json:"notes,omitempty" jsonschema:"REPLACES the notes; empty string clears them"`
	AppendNotes string    `json:"append_notes,omitempty" jsonschema:"text added on a new line after the current notes"`
	When        *string   `json:"when,omitempty" jsonschema:"new schedule; empty string clears it"`
	Deadline    *string   `json:"deadline,omitempty" jsonschema:"YYYY-MM-DD; empty string clears it"`
	Tags        *[]string `json:"tags,omitempty" jsonschema:"REPLACES all tags"`
	AddTags     []string  `json:"add_tags,omitempty" jsonschema:"tags added to the current ones"`
	AreaID      *string   `json:"area_id,omitempty" jsonschema:"move to this area"`
}

// SetTaskStatusInput describes set_task_status.
type SetTaskStatusInput struct {
	ID     string `json:"id" jsonschema:"ID of the to-do"`
	Status string `json:"status" jsonschema:"completed or canceled closes the to-do (it moves to the Logbook); open reopens it"`
}

func (s *server) registerWriteTools(srv *mcp.Server, caps backend.Capabilities) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_task",
		Description: "Create a to-do. Tags must already exist. Returns the created to-do.",
		Annotations: writes("Create task", false, false),
	}, handle(s, "create_task", s.createTask))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_project",
		Description: "Create a project, optionally with to-dos and headings (max 250 items in total). Tags must already exist.",
		Annotations: writes("Create project", false, false),
	}, handle(s, "create_project", s.createProject))

	mcp.AddTool(srv, &mcp.Tool{
		Name: "update_task",
		Description: "Update a to-do. Only the fields you pass change. notes, tags and checklist REPLACE the current values; " +
			"prefer append_notes, add_tags and append_checklist to add without losing data. Can also move the to-do " +
			"to a project, heading or area. Use set_task_status to complete it. Repeating to-dos may reject schedule changes.",
		Annotations: writes("Update task", true, false),
	}, handle(s, "update_task", s.updateTask))

	mcp.AddTool(srv, &mcp.Tool{
		Name: "update_project",
		Description: "Update a project. Only the fields you pass change. notes and tags REPLACE the current values; " +
			"prefer append_notes and add_tags. To change its to-dos, use the task tools.",
		Annotations: writes("Update project", true, false),
	}, handle(s, "update_project", s.updateProject))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_task_status",
		Description: "Complete, cancel or reopen a to-do. Reversible: reopen with status open.",
		InputSchema: schemaFor[SetTaskStatusInput](map[string][]string{"status": statusNames}),
		Annotations: writes("Set task status", false, true),
	}, handle(s, "set_task_status", s.setTaskStatus))

	if caps.Trash {
		mcp.AddTool(srv, &mcp.Tool{
			Name: "move_to_trash",
			Description: "Move a to-do or project to the Things trash (a project's to-dos go with it). " +
				"This is NOT a permanent deletion: the user can restore it from the Trash list in Things. " +
				"Confirm with the user before trashing.",
			Annotations: writes("Move to trash", true, true),
		}, handle(s, "move_to_trash", s.moveToTrash))
	}

	if caps.ShowInApp {
		mcp.AddTool(srv, &mcp.Tool{
			Name:        "show_in_things",
			Description: "Open a to-do, project or area in the Things app on this Mac so the user can see it.",
			Annotations: readOnly("Show in Things"),
		}, handle(s, "show_in_things", s.showInThings))
	}
}

func (s *server) parseWhen(raw string) (domain.When, error) {
	if raw == "" {
		return domain.When{}, nil
	}
	return domain.ParseWhen(raw, s.today())
}

func parseDeadline(raw string) (domain.Date, error) {
	if raw == "" {
		return domain.Date{}, nil
	}
	return domain.ParseDeadline(raw)
}

func (s *server) createTask(ctx context.Context, in CreateTaskInput) (TaskDetail, error) {
	when, errW := s.parseWhen(in.When)
	deadline, errD := parseDeadline(in.Deadline)
	if err := errors.Join(errW, errD); err != nil {
		return TaskDetail{}, err
	}
	n := domain.NewTask{
		Title: in.Title, Notes: in.Notes, When: when, Deadline: deadline, Tags: in.Tags,
		Checklist: in.Checklist, ProjectID: in.ProjectID, AreaID: in.AreaID, HeadingID: in.HeadingID,
	}
	if err := n.Validate(); err != nil {
		return TaskDetail{}, err
	}
	t, err := s.b.CreateTask(ctx, n)
	if err != nil {
		return TaskDetail{}, err
	}
	return toTaskDetail(t), nil
}

func (s *server) createProject(ctx context.Context, in CreateProjectInput) (ProjectSummary, error) {
	when, errW := s.parseWhen(in.When)
	deadline, errD := parseDeadline(in.Deadline)
	if err := errors.Join(errW, errD); err != nil {
		return ProjectSummary{}, err
	}
	n := domain.NewProject{
		Title: in.Title, Notes: in.Notes, When: when, Deadline: deadline,
		Tags: in.Tags, AreaID: in.AreaID, Tasks: in.Tasks,
	}
	for _, h := range in.Headings {
		n.Headings = append(n.Headings, domain.NewHeading{Title: h.Title, Tasks: h.Tasks})
	}
	if err := n.Validate(); err != nil {
		return ProjectSummary{}, err
	}
	p, err := s.b.CreateProject(ctx, n)
	if err != nil {
		return ProjectSummary{}, err
	}
	return toProjectSummary(p), nil
}

// optionalSchedule converts optional when/deadline strings, where an empty
// string means "clear the field".
func (s *server) optionalSchedule(when, deadline *string) (*domain.When, *domain.Date, error) {
	var w *domain.When
	var d *domain.Date
	if when != nil {
		v, err := s.parseWhen(*when)
		if err != nil {
			return nil, nil, err
		}
		w = &v
	}
	if deadline != nil {
		v, err := parseDeadline(*deadline)
		if err != nil {
			return nil, nil, err
		}
		d = &v
	}
	return w, d, nil
}

func (s *server) updateTask(ctx context.Context, in UpdateTaskInput) (TaskDetail, error) {
	if err := domain.ValidateID("id", in.ID); err != nil {
		return TaskDetail{}, err
	}
	when, deadline, err := s.optionalSchedule(in.When, in.Deadline)
	if err != nil {
		return TaskDetail{}, err
	}
	p := domain.TaskPatch{
		Title: in.Title, Notes: in.Notes, AppendNotes: in.AppendNotes, When: when, Deadline: deadline,
		Tags: in.Tags, AddTags: in.AddTags, Checklist: in.Checklist, AppendChecklist: in.AppendChecklist,
		ProjectID: in.ProjectID, AreaID: in.AreaID, HeadingID: in.HeadingID,
	}
	if err := p.Validate(); err != nil {
		return TaskDetail{}, err
	}
	t, err := s.b.UpdateTask(ctx, in.ID, p)
	if err != nil {
		return TaskDetail{}, err
	}
	return toTaskDetail(t), nil
}

func (s *server) updateProject(ctx context.Context, in UpdateProjectInput) (ProjectSummary, error) {
	if err := domain.ValidateID("id", in.ID); err != nil {
		return ProjectSummary{}, err
	}
	when, deadline, err := s.optionalSchedule(in.When, in.Deadline)
	if err != nil {
		return ProjectSummary{}, err
	}
	p := domain.ProjectPatch{
		Title: in.Title, Notes: in.Notes, AppendNotes: in.AppendNotes, When: when, Deadline: deadline,
		Tags: in.Tags, AddTags: in.AddTags, AreaID: in.AreaID,
	}
	if err := p.Validate(); err != nil {
		return ProjectSummary{}, err
	}
	pr, err := s.b.UpdateProject(ctx, in.ID, p)
	if err != nil {
		return ProjectSummary{}, err
	}
	return toProjectSummary(pr), nil
}

func (s *server) setTaskStatus(ctx context.Context, in SetTaskStatusInput) (TaskSummary, error) {
	st, errS := domain.ParseStatus(in.Status)
	if err := errors.Join(domain.ValidateID("id", in.ID), errS); err != nil {
		return TaskSummary{}, err
	}
	t, err := s.b.SetTaskStatus(ctx, in.ID, st)
	if err != nil {
		return TaskSummary{}, err
	}
	return toTaskSummary(t), nil
}

func (s *server) moveToTrash(ctx context.Context, in IDInput) (TrashResult, error) {
	if err := domain.ValidateID("id", in.ID); err != nil {
		return TrashResult{}, err
	}
	if err := s.b.MoveToTrash(ctx, in.ID); err != nil {
		return TrashResult{}, err
	}
	return TrashResult{
		ID: in.ID, Trashed: true,
		Message: "Moved to the Things trash. It can be restored from the Trash list in Things until the user empties the trash.",
	}, nil
}

func (s *server) showInThings(ctx context.Context, in IDInput) (ShowResult, error) {
	if err := domain.ValidateID("id", in.ID); err != nil {
		return ShowResult{}, err
	}
	if err := s.b.ShowInApp(ctx, in.ID); err != nil {
		return ShowResult{}, err
	}
	return ShowResult{ID: in.ID, Shown: true}, nil
}
