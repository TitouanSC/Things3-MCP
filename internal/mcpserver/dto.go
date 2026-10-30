package mcpserver

import (
	"time"

	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

// Output types are kept separate from the domain model: they define the
// public JSON contract of the tools (and their output schemas) and use plain
// strings for dates so clients and models can read them directly.

// TaskSummary is the compact form of a task used in listings.
type TaskSummary struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Status    string   `json:"status" jsonschema:"open, completed or canceled"`
	When      string   `json:"when,omitempty" jsonschema:"today, evening, anytime, someday or YYYY-MM-DD, with an optional @HH:MM reminder"`
	Deadline  string   `json:"deadline,omitempty" jsonschema:"YYYY-MM-DD"`
	Tags      []string `json:"tags,omitempty"`
	ProjectID string   `json:"project_id,omitempty"`
	AreaID    string   `json:"area_id,omitempty"`
	HeadingID string   `json:"heading_id,omitempty"`
	Trashed   bool     `json:"trashed,omitempty"`
}

// ChecklistItem is one checklist line.
type ChecklistItem struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}

// TaskDetail is the full form of a task.
type TaskDetail struct {
	TaskSummary
	Notes       string          `json:"notes,omitempty"`
	Checklist   []ChecklistItem `json:"checklist,omitempty"`
	CreatedAt   string          `json:"created_at,omitempty" jsonschema:"RFC 3339 timestamp"`
	ModifiedAt  string          `json:"modified_at,omitempty" jsonschema:"RFC 3339 timestamp"`
	CompletedAt string          `json:"completed_at,omitempty" jsonschema:"RFC 3339 timestamp"`
}

// TaskList is a page of tasks.
type TaskList struct {
	Tasks      []TaskSummary `json:"tasks"`
	NextCursor string        `json:"next_cursor,omitempty" jsonschema:"pass it as cursor to get the next page; absent on the last page"`
}

// ProjectSummary is the compact form of a project.
type ProjectSummary struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	When     string   `json:"when,omitempty"`
	Deadline string   `json:"deadline,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	AreaID   string   `json:"area_id,omitempty"`
	Trashed  bool     `json:"trashed,omitempty"`
}

// Heading is a section of a project.
type Heading struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Archived bool   `json:"archived,omitempty"`
}

// ProjectDetail is a project with its notes, headings and tasks.
type ProjectDetail struct {
	ProjectSummary
	Notes       string        `json:"notes,omitempty"`
	CreatedAt   string        `json:"created_at,omitempty"`
	ModifiedAt  string        `json:"modified_at,omitempty"`
	CompletedAt string        `json:"completed_at,omitempty"`
	Headings    []Heading     `json:"headings"`
	Tasks       []TaskSummary `json:"tasks"`
}

// ProjectList is a page of projects.
type ProjectList struct {
	Projects   []ProjectSummary `json:"projects"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// Area is a Things area.
type Area struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Tags  []string `json:"tags,omitempty"`
}

// AreaList lists every area.
type AreaList struct {
	Areas []Area `json:"areas"`
}

// Tag is a Things tag.
type Tag struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ParentID string `json:"parent_id,omitempty"`
}

// TagList lists every tag.
type TagList struct {
	Tags []Tag `json:"tags"`
}

// SearchHit is one search result.
type SearchHit struct {
	Kind   string `json:"kind" jsonschema:"task or project"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// SearchResult is a page of search hits.
type SearchResult struct {
	Hits       []SearchHit `json:"hits"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

// TrashResult confirms a move to the trash.
type TrashResult struct {
	ID      string `json:"id"`
	Trashed bool   `json:"trashed"`
	Message string `json:"message"`
}

// ShowResult confirms that an item was revealed in the app.
type ShowResult struct {
	ID    string `json:"id"`
	Shown bool   `json:"shown"`
}

func timestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func toTaskSummary(t domain.Task) TaskSummary {
	return TaskSummary{
		ID: t.ID, Title: t.Title, Status: string(t.Status),
		When: t.When.String(), Deadline: t.Deadline.String(), Tags: t.Tags,
		ProjectID: t.ProjectID, AreaID: t.AreaID, HeadingID: t.HeadingID, Trashed: t.Trashed,
	}
}

func toTaskSummaries(ts []domain.Task) []TaskSummary {
	out := make([]TaskSummary, 0, len(ts))
	for _, t := range ts {
		out = append(out, toTaskSummary(t))
	}
	return out
}

func toTaskDetail(t domain.Task) TaskDetail {
	d := TaskDetail{
		TaskSummary: toTaskSummary(t), Notes: t.Notes,
		CreatedAt: timestamp(t.CreatedAt), ModifiedAt: timestamp(t.ModifiedAt), CompletedAt: timestamp(t.CompletedAt),
	}
	for _, c := range t.Checklist {
		d.Checklist = append(d.Checklist, ChecklistItem{Title: c.Title, Status: string(c.Status)})
	}
	return d
}

func toProjectSummary(p domain.Project) ProjectSummary {
	return ProjectSummary{
		ID: p.ID, Title: p.Title, Status: string(p.Status), When: p.When.String(),
		Deadline: p.Deadline.String(), Tags: p.Tags, AreaID: p.AreaID, Trashed: p.Trashed,
	}
}

func toProjectDetail(d domain.ProjectDetail) ProjectDetail {
	out := ProjectDetail{
		ProjectSummary: toProjectSummary(d.Project), Notes: d.Project.Notes,
		CreatedAt: timestamp(d.Project.CreatedAt), ModifiedAt: timestamp(d.Project.ModifiedAt),
		CompletedAt: timestamp(d.Project.CompletedAt),
		Headings:    make([]Heading, 0, len(d.Headings)), Tasks: toTaskSummaries(d.Tasks),
	}
	for _, h := range d.Headings {
		out.Headings = append(out.Headings, Heading{ID: h.ID, Title: h.Title, Archived: h.Archived})
	}
	return out
}
