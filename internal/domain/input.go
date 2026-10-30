package domain

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Limits documented by the Things URL scheme. They are enforced for every
// backend so the tools behave the same in local and hosted mode.
const (
	MaxStringLen     = 4000
	MaxNotesLen      = 10000
	MaxChecklistLen  = 100
	MaxProjectTasks  = 250 // URL scheme: 250 items per 10 seconds
	MaxSearchLen     = 200
	maxIDLen         = 64
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

var idRe = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// ValidateID checks that an identifier is a plain Things ID. IDs are later
// interpolated into URLs and passed to AppleScript, so the format is strict.
func ValidateID(field, id string) error {
	if id == "" {
		return Invalid(field, "must not be empty")
	}
	if len(id) > maxIDLen || !idRe.MatchString(id) {
		return Invalid(field, "not a valid Things ID")
	}
	return nil
}

func validateOptionalID(field, id string) error {
	if id == "" {
		return nil
	}
	return ValidateID(field, id)
}

func validateText(field, s string, max int, required bool) error {
	if required && strings.TrimSpace(s) == "" {
		return Invalid(field, "must not be empty")
	}
	if n := utf8.RuneCountInString(s); n > max {
		return Invalid(field, "too long (%d characters, max %d)", n, max)
	}
	return nil
}

func validateTitle(field, s string) error {
	if err := validateText(field, s, MaxStringLen, true); err != nil {
		return err
	}
	if strings.ContainsAny(s, "\r\n") {
		return Invalid(field, "must be a single line")
	}
	return nil
}

func validateTags(field string, tags []string) error {
	for _, t := range tags {
		if strings.TrimSpace(t) == "" {
			return Invalid(field, "tag names must not be empty")
		}
		// The URL scheme separates tags with commas.
		if strings.Contains(t, ",") {
			return Invalid(field, "tag %q must not contain a comma", t)
		}
	}
	return nil
}

func validateChecklist(field string, items []string) error {
	if len(items) > MaxChecklistLen {
		return Invalid(field, "too many items (%d, max %d)", len(items), MaxChecklistLen)
	}
	for _, it := range items {
		if err := validateTitle(field, it); err != nil {
			return err
		}
	}
	return nil
}

// NewTask describes a to-do to create.
type NewTask struct {
	Title     string
	Notes     string
	When      When
	Deadline  Date
	Tags      []string
	Checklist []string
	ProjectID string
	AreaID    string
	HeadingID string // requires ProjectID
}

// Validate checks the domain rules for a new task.
func (n NewTask) Validate() error {
	return errors.Join(
		validateTitle("title", n.Title),
		validateText("notes", n.Notes, MaxNotesLen, false),
		validateTags("tags", n.Tags),
		validateChecklist("checklist", n.Checklist),
		validateOptionalID("project_id", n.ProjectID),
		validateOptionalID("area_id", n.AreaID),
		validateOptionalID("heading_id", n.HeadingID),
		validateLocation(n.ProjectID, n.AreaID, n.HeadingID),
	)
}

func validateLocation(projectID, areaID, headingID string) error {
	if projectID != "" && areaID != "" {
		return Invalid("project_id", "set either project_id or area_id, not both")
	}
	if headingID != "" && projectID == "" {
		return Invalid("heading_id", "requires project_id")
	}
	return nil
}

// NewHeading is a heading created together with a project.
type NewHeading struct {
	Title string
	Tasks []string // titles of tasks created under this heading
}

// NewProject describes a project to create, optionally with its content.
type NewProject struct {
	Title    string
	Notes    string
	When     When
	Deadline Date
	Tags     []string
	AreaID   string
	Tasks    []string // titles of tasks created at the top of the project
	Headings []NewHeading
}

// Validate checks the domain rules for a new project.
func (n NewProject) Validate() error {
	errs := []error{
		validateTitle("title", n.Title),
		validateText("notes", n.Notes, MaxNotesLen, false),
		validateTags("tags", n.Tags),
		validateOptionalID("area_id", n.AreaID),
	}
	count := len(n.Tasks)
	for _, t := range n.Tasks {
		errs = append(errs, validateTitle("tasks", t))
	}
	for _, h := range n.Headings {
		errs = append(errs, validateTitle("headings", h.Title))
		count += 1 + len(h.Tasks)
		for _, t := range h.Tasks {
			errs = append(errs, validateTitle("headings.tasks", t))
		}
	}
	if count > MaxProjectTasks {
		errs = append(errs, Invalid("tasks", "too many items (%d, max %d)", count, MaxProjectTasks))
	}
	return errors.Join(errs...)
}

// TaskPatch is a partial update of a task. Nil pointers leave the field
// unchanged. Replace fields (Notes, Tags, Checklist) and their additive
// counterparts (AppendNotes, AddTags, AppendChecklist) are mutually exclusive.
// A pointer to a zero When or Date clears the field.
type TaskPatch struct {
	Title           *string
	Notes           *string
	AppendNotes     string
	When            *When
	Deadline        *Date
	Tags            *[]string
	AddTags         []string
	Checklist       *[]string
	AppendChecklist []string
	ProjectID       *string // move to a project
	AreaID          *string // move to an area
	HeadingID       *string // move under a heading of the (new or current) project
}

// IsEmpty reports whether the patch changes nothing.
func (p TaskPatch) IsEmpty() bool {
	return p.Title == nil && p.Notes == nil && p.AppendNotes == "" && p.When == nil &&
		p.Deadline == nil && p.Tags == nil && len(p.AddTags) == 0 && p.Checklist == nil &&
		len(p.AppendChecklist) == 0 && p.ProjectID == nil && p.AreaID == nil && p.HeadingID == nil
}

// Validate checks the domain rules for a task patch.
func (p TaskPatch) Validate() error {
	if p.IsEmpty() {
		return Invalid("patch", "nothing to update")
	}
	errs := []error{
		validateText("append_notes", p.AppendNotes, MaxNotesLen, false),
		validateTags("add_tags", p.AddTags),
		validateChecklist("append_checklist", p.AppendChecklist),
		exclusive("notes", p.Notes != nil, "append_notes", p.AppendNotes != ""),
		exclusive("tags", p.Tags != nil, "add_tags", len(p.AddTags) > 0),
		exclusive("checklist", p.Checklist != nil, "append_checklist", len(p.AppendChecklist) > 0),
		exclusive("project_id", p.ProjectID != nil, "area_id", p.AreaID != nil),
	}
	if p.Title != nil {
		errs = append(errs, validateTitle("title", *p.Title))
	}
	if p.Notes != nil {
		errs = append(errs, validateText("notes", *p.Notes, MaxNotesLen, false))
	}
	if p.Tags != nil {
		errs = append(errs, validateTags("tags", *p.Tags))
	}
	if p.Checklist != nil {
		errs = append(errs, validateChecklist("checklist", *p.Checklist))
	}
	if p.ProjectID != nil {
		errs = append(errs, ValidateID("project_id", *p.ProjectID))
	}
	if p.AreaID != nil {
		errs = append(errs, ValidateID("area_id", *p.AreaID))
	}
	if p.HeadingID != nil {
		errs = append(errs, ValidateID("heading_id", *p.HeadingID))
	}
	return errors.Join(errs...)
}

// ProjectPatch is a partial update of a project, with the same conventions as
// TaskPatch.
type ProjectPatch struct {
	Title       *string
	Notes       *string
	AppendNotes string
	When        *When
	Deadline    *Date
	Tags        *[]string
	AddTags     []string
	AreaID      *string
}

// IsEmpty reports whether the patch changes nothing.
func (p ProjectPatch) IsEmpty() bool {
	return p.Title == nil && p.Notes == nil && p.AppendNotes == "" && p.When == nil &&
		p.Deadline == nil && p.Tags == nil && len(p.AddTags) == 0 && p.AreaID == nil
}

// Validate checks the domain rules for a project patch.
func (p ProjectPatch) Validate() error {
	if p.IsEmpty() {
		return Invalid("patch", "nothing to update")
	}
	errs := []error{
		validateText("append_notes", p.AppendNotes, MaxNotesLen, false),
		validateTags("add_tags", p.AddTags),
		exclusive("notes", p.Notes != nil, "append_notes", p.AppendNotes != ""),
		exclusive("tags", p.Tags != nil, "add_tags", len(p.AddTags) > 0),
	}
	if p.Title != nil {
		errs = append(errs, validateTitle("title", *p.Title))
	}
	if p.Notes != nil {
		errs = append(errs, validateText("notes", *p.Notes, MaxNotesLen, false))
	}
	if p.Tags != nil {
		errs = append(errs, validateTags("tags", *p.Tags))
	}
	if p.AreaID != nil {
		errs = append(errs, ValidateID("area_id", *p.AreaID))
	}
	return errors.Join(errs...)
}

func exclusive(a string, hasA bool, b string, hasB bool) error {
	if hasA && hasB {
		return Invalid(a, "cannot be combined with %s", b)
	}
	return nil
}

// TaskQuery filters a task listing. Empty fields do not filter; with no View,
// every open, non-trashed task matches.
type TaskQuery struct {
	View      View
	ProjectID string
	AreaID    string
	Tag       string
	Limit     int
	Cursor    string
}

// Normalize validates q and applies the default page size.
func (q *TaskQuery) Normalize() error {
	if q.View != "" {
		if _, err := ParseView(string(q.View)); err != nil {
			return err
		}
	}
	return errors.Join(
		validateOptionalID("project_id", q.ProjectID),
		validateOptionalID("area_id", q.AreaID),
		normalizeLimit(&q.Limit),
	)
}

// ProjectQuery filters a project listing. An empty Status means open projects.
type ProjectQuery struct {
	AreaID string
	Status Status
	Limit  int
	Cursor string
}

// Normalize validates q and applies defaults.
func (q *ProjectQuery) Normalize() error {
	if q.Status == "" {
		q.Status = StatusOpen
	}
	_, err := ParseStatus(string(q.Status))
	return errors.Join(err, validateOptionalID("area_id", q.AreaID), normalizeLimit(&q.Limit))
}

// ItemKind distinguishes search hits.
type ItemKind string

// Kinds of searchable items.
const (
	KindTask    ItemKind = "task"
	KindProject ItemKind = "project"
)

// SearchQuery is a case-insensitive text search over titles and notes.
// Trashed items are never returned. Empty Kind/Status do not filter.
type SearchQuery struct {
	Text   string
	Kind   ItemKind
	Status Status
	Limit  int
	Cursor string
}

// Normalize validates q and applies defaults.
func (q *SearchQuery) Normalize() error {
	errs := []error{validateText("query", q.Text, MaxSearchLen, true), normalizeLimit(&q.Limit)}
	if q.Kind != "" && q.Kind != KindTask && q.Kind != KindProject {
		errs = append(errs, Invalid("kind", "expected task or project; got %q", q.Kind))
	}
	if q.Status != "" {
		_, err := ParseStatus(string(q.Status))
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// SearchHit is one search result.
type SearchHit struct {
	Kind   ItemKind
	ID     string
	Title  string
	Status Status
}

func normalizeLimit(limit *int) error {
	switch {
	case *limit == 0:
		*limit = DefaultPageLimit
	case *limit < 0 || *limit > MaxPageLimit:
		return Invalid("limit", "must be between 1 and %d", MaxPageLimit)
	}
	return nil
}
