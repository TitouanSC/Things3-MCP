package domain

import (
	"time"
)

// Status is the completion state of a task, project or checklist item.
type Status string

// Statuses, mirroring Things (open, completed, canceled).
const (
	StatusOpen      Status = "open"
	StatusCompleted Status = "completed"
	StatusCanceled  Status = "canceled"
)

// ParseStatus validates a status string.
func ParseStatus(s string) (Status, error) {
	switch st := Status(s); st {
	case StatusOpen, StatusCompleted, StatusCanceled:
		return st, nil
	}
	return "", Invalid("status", "expected open, completed or canceled; got %q", s)
}

// Closed reports whether the status is completed or canceled.
func (s Status) Closed() bool { return s == StatusCompleted || s == StatusCanceled }

// ChecklistItem is one line of a task's checklist.
type ChecklistItem struct {
	Title  string
	Status Status
}

// Task is a Things to-do.
type Task struct {
	ID          string
	Title       string
	Notes       string
	Status      Status
	When        When
	Deadline    Date
	Tags        []string // tag titles
	Checklist   []ChecklistItem
	ProjectID   string
	AreaID      string
	HeadingID   string
	Trashed     bool
	CreatedAt   time.Time
	ModifiedAt  time.Time
	CompletedAt time.Time // zero unless Status.Closed()
}

// Project is a Things project.
type Project struct {
	ID          string
	Title       string
	Notes       string
	Status      Status
	When        When
	Deadline    Date
	Tags        []string
	AreaID      string
	Trashed     bool
	CreatedAt   time.Time
	ModifiedAt  time.Time
	CompletedAt time.Time
}

// Heading groups tasks inside a project.
type Heading struct {
	ID        string
	Title     string
	ProjectID string
	Archived  bool
}

// ProjectDetail is a project with its headings and tasks.
type ProjectDetail struct {
	Project  Project
	Headings []Heading
	Tasks    []Task
}

// Area is a Things area of responsibility.
type Area struct {
	ID    string
	Title string
	Tags  []string
}

// Tag is a Things tag; tags can be nested.
type Tag struct {
	ID       string
	Title    string
	ParentID string
}

// View is one of the built-in Things lists.
type View string

// Built-in lists.
const (
	ViewInbox    View = "inbox"
	ViewToday    View = "today"
	ViewUpcoming View = "upcoming"
	ViewAnytime  View = "anytime"
	ViewSomeday  View = "someday"
	ViewLogbook  View = "logbook"
	ViewTrash    View = "trash"
)

// Views lists every built-in view, in sidebar order.
var Views = []View{ViewInbox, ViewToday, ViewUpcoming, ViewAnytime, ViewSomeday, ViewLogbook, ViewTrash}

// ParseView validates a view name.
func ParseView(s string) (View, error) {
	for _, v := range Views {
		if string(v) == s {
			return v, nil
		}
	}
	return "", Invalid("view", "unknown view %q", s)
}

// InView reports whether a task belongs to a built-in view, following Things
// semantics: Today also appears in Anytime, closed tasks only in the Logbook,
// trashed tasks only in the Trash. Backends that compute views themselves
// (e.g. via SQLite) must stay consistent with this reference definition.
func InView(t Task, v View, today Date) bool {
	if v == ViewTrash {
		return t.Trashed
	}
	if t.Trashed {
		return false
	}
	if v == ViewLogbook {
		return t.Status.Closed()
	}
	if t.Status.Closed() {
		return false
	}
	dated := t.When.Kind == WhenDate
	switch v {
	case ViewInbox:
		return t.When.IsZero() && t.ProjectID == "" && t.AreaID == "" && t.HeadingID == ""
	case ViewToday:
		return t.When.Kind == WhenToday || t.When.Kind == WhenEvening || (dated && !today.Before(t.When.Date))
	case ViewUpcoming:
		return dated && today.Before(t.When.Date)
	case ViewAnytime:
		return !InView(t, ViewInbox, today) && t.When.Kind != WhenSomeday && !InView(t, ViewUpcoming, today)
	case ViewSomeday:
		return t.When.Kind == WhenSomeday
	}
	return false
}
