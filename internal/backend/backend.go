// Package backend defines the TaskBackend port implemented by every storage
// adapter (in-memory fake, local macOS app, Things Cloud). The MCP layer
// depends only on this interface (see docs/adr/002).
package backend

import (
	"context"

	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

// Capabilities describes optional features. Tools backed by a missing
// capability are not registered at all, rather than failing at call time.
type Capabilities struct {
	// Trash: MoveToTrash is available.
	Trash bool
	// ShowInApp: ShowInApp opens the item in the Things app (local only).
	ShowInApp bool
	// ReturnsIDs: create operations reliably return the new item's ID.
	ReturnsIDs bool
}

// Page is one page of a paginated listing. NextCursor is empty on the last page.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// TaskBackend is the contract between the MCP tools and a Things data source.
//
// Implementations must:
//   - return errors wrapping the domain sentinels (domain.ErrNotFound, …);
//   - never delete permanently: MoveToTrash keeps items restorable, and there
//     is deliberately no method to empty the trash;
//   - accept inputs already validated by the caller, but must not trust them
//     blindly when interpolating them into URLs, scripts or queries.
type TaskBackend interface {
	Capabilities() Capabilities

	// Read
	ListTasks(ctx context.Context, q domain.TaskQuery) (Page[domain.Task], error)
	GetTask(ctx context.Context, id string) (domain.Task, error)
	ListProjects(ctx context.Context, q domain.ProjectQuery) (Page[domain.Project], error)
	GetProject(ctx context.Context, id string) (domain.ProjectDetail, error)
	ListAreas(ctx context.Context) ([]domain.Area, error)
	ListTags(ctx context.Context) ([]domain.Tag, error)
	Search(ctx context.Context, q domain.SearchQuery) (Page[domain.SearchHit], error)

	// Write
	CreateTask(ctx context.Context, n domain.NewTask) (domain.Task, error)
	CreateProject(ctx context.Context, n domain.NewProject) (domain.Project, error)
	UpdateTask(ctx context.Context, id string, p domain.TaskPatch) (domain.Task, error)
	UpdateProject(ctx context.Context, id string, p domain.ProjectPatch) (domain.Project, error)
	SetTaskStatus(ctx context.Context, id string, s domain.Status) (domain.Task, error)
	// MoveToTrash moves a task or project to the Things trash. It is
	// idempotent. Returns domain.ErrUnsupported without the Trash capability.
	MoveToTrash(ctx context.Context, id string) error
	// ShowInApp reveals an item in the Things app. Returns
	// domain.ErrUnsupported without the ShowInApp capability.
	ShowInApp(ctx context.Context, id string) error
}
