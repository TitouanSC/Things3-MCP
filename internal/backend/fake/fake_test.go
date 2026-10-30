package fake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TitouanSC/Things3-MCP/internal/backend"
	"github.com/TitouanSC/Things3-MCP/internal/backend/backendtest"
	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

var fixedNow = time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

func TestContract(t *testing.T) {
	backendtest.Run(t, func(t *testing.T) backendtest.Harness {
		b := New(WithClock(func() time.Time { return fixedNow }))
		return backendtest.Harness{
			Backend: b,
			Area:    b.SeedArea("Work"),
			Tag:     b.SeedTag("errand", ""),
			Tag2:    b.SeedTag("home", ""),
		}
	})
}

func TestContractWithoutTrash(t *testing.T) {
	backendtest.Run(t, func(t *testing.T) backendtest.Harness {
		b := New(WithCapabilities(backend.Capabilities{ReturnsIDs: true}))
		return backendtest.Harness{Backend: b, Area: b.SeedArea("A"), Tag: b.SeedTag("t1", ""), Tag2: b.SeedTag("t2", "")}
	})
}

func TestDeterministicIDs(t *testing.T) {
	ids := func() []string {
		b := New(WithClock(func() time.Time { return fixedNow }))
		a, _ := b.CreateTask(context.Background(), domain.NewTask{Title: "a"})
		p, _ := b.CreateProject(context.Background(), domain.NewProject{Title: "p"})
		return []string{a.ID, p.ID}
	}
	first, second := ids(), ids()
	if first[0] != second[0] || first[1] != second[1] {
		t.Fatalf("IDs differ between runs: %v vs %v", first, second)
	}
}

func TestLogbookNewestFirst(t *testing.T) {
	now := fixedNow
	b := New(WithClock(func() time.Time { return now }))
	ctx := context.Background()
	older, _ := b.CreateTask(ctx, domain.NewTask{Title: "older"})
	newer, _ := b.CreateTask(ctx, domain.NewTask{Title: "newer"})
	b.SetTaskStatus(ctx, older.ID, domain.StatusCompleted)
	now = now.Add(time.Hour)
	b.SetTaskStatus(ctx, newer.ID, domain.StatusCanceled)

	p, err := b.ListTasks(ctx, domain.TaskQuery{View: domain.ViewLogbook})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || p.Items[0].ID != newer.ID {
		t.Fatalf("logbook order = %+v", p.Items)
	}
}

func TestShowInApp(t *testing.T) {
	b := New()
	ctx := context.Background()
	task, _ := b.CreateTask(ctx, domain.NewTask{Title: "a"})
	if err := b.ShowInApp(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := b.ShowInApp(ctx, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ShowInApp(missing) = %v", err)
	}
	if got := b.Shown(); len(got) != 1 || got[0] != task.ID {
		t.Fatalf("Shown() = %v", got)
	}
	off := New(WithCapabilities(backend.Capabilities{}))
	if err := off.ShowInApp(ctx, task.ID); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("ShowInApp without capability = %v", err)
	}
}

func TestReturnedValuesAreCopies(t *testing.T) {
	b := New()
	ctx := context.Background()
	b.SeedTag("x", "")
	task, _ := b.CreateTask(ctx, domain.NewTask{Title: "a", Tags: []string{"x"}, Checklist: []string{"c"}})
	task.Tags[0] = "mutated"
	task.Checklist[0].Title = "mutated"
	got, _ := b.GetTask(ctx, task.ID)
	if got.Tags[0] != "x" || got.Checklist[0].Title != "c" {
		t.Fatal("caller mutation leaked into the backend")
	}
}
