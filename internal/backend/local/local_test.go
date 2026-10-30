package local

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TitouanSC/Things3-MCP/internal/backend/backendtest"
	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

var ctx = context.Background()

func newBackend(t *testing.T, mutate ...func(*Config)) (*Backend, *simulator) {
	t.Helper()
	path, sim := newFixture(t)
	cfg := Config{DBPath: path, AuthToken: sim.token, Runner: sim, WriteTimeout: 500 * time.Millisecond, PollInterval: time.Millisecond}
	for _, m := range mutate {
		m(&cfg)
	}
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b, sim
}

func TestContract(t *testing.T) {
	backendtest.Run(t, func(t *testing.T) backendtest.Harness {
		b, sim := newBackend(t)
		return backendtest.Harness{Backend: b, Area: sim.addArea("Work"), Tag: sim.addTag("errand"), Tag2: sim.addTag("home")}
	})
}

func TestDecode(t *testing.T) {
	d := domain.Date{Year: 2026, Month: 5, Day: 15}
	if got := decodeDate(sql.NullInt64{Int64: 132798336, Valid: true}); got != d {
		t.Errorf("decodeDate = %v, want %v", got, d) // value observed in a real database
	}
	if encodeDate(d) != 132798336 {
		t.Errorf("encodeDate(%v) = %d", d, encodeDate(d))
	}
	if got := decodeReminder(sql.NullInt64{Int64: 651165696, Valid: true}); got != "09:45" {
		t.Errorf("decodeReminder = %q", got)
	}
	today := domain.Date{Year: 2026, Month: 10, Day: 30}
	packed := func(d domain.Date) sql.NullInt64 { return sql.NullInt64{Int64: encodeDate(d), Valid: true} }
	none := sql.NullInt64{}
	cases := []struct {
		start  int64
		date   sql.NullInt64
		bucket int64
		want   domain.When
	}{
		{startInbox, none, 0, domain.When{}},
		{startAnytime, none, 0, domain.When{Kind: domain.WhenAnytime}},
		{startSomeday, none, 0, domain.When{Kind: domain.WhenSomeday}},
		{startAnytime, packed(today), 0, domain.When{Kind: domain.WhenToday}},
		{startAnytime, packed(today), 1, domain.When{Kind: domain.WhenEvening}},
		{startSomeday, packed(today.AddDays(3)), 0, domain.When{Kind: domain.WhenDate, Date: today.AddDays(3)}},
		{startAnytime, packed(today.AddDays(-1)), 0, domain.When{Kind: domain.WhenDate, Date: today.AddDays(-1)}},
	}
	for _, c := range cases {
		if got := decodeWhen(c.start, c.date, c.bucket, none, today); got != c.want {
			t.Errorf("decodeWhen(%d, %v, %d) = %+v, want %+v", c.start, c.date, c.bucket, got, c.want)
		}
	}
}

func TestURLEscapingPreventsParameterInjection(t *testing.T) {
	b, sim := newBackend(t)
	title := "Pay & go #1 + more=yes&auth-token=evil&list-id=X"
	if _, err := b.CreateTask(ctx, domain.NewTask{Title: title, Notes: "a\nb é"}); err != nil {
		t.Fatal(err)
	}
	call := sim.lastCall()
	if len(call) != 3 || call[0] != "open" || call[1] != "-g" {
		t.Fatalf("call = %q", call)
	}
	raw := call[2]
	if strings.Contains(raw, "+") || strings.Contains(raw, " ") {
		t.Errorf("URL must use %%20, not '+' or raw spaces: %s", raw)
	}
	u, _ := url.Parse(raw)
	q, _ := url.ParseQuery(u.RawQuery)
	if q.Get("title") != title || q.Get("notes") != "a\nb é" || q.Has("auth-token") || q.Has("list-id") {
		t.Errorf("parameters altered: %v", q)
	}
}

func TestUpdatesNeedToken(t *testing.T) {
	b, _ := newBackend(t, func(c *Config) { c.AuthToken = "" })
	task, err := b.CreateTask(ctx, domain.NewTask{Title: "no token needed to create"})
	if err != nil {
		t.Fatal(err)
	}
	title := "x"
	_, err = b.UpdateTask(ctx, task.ID, domain.TaskPatch{Title: &title})
	if !errors.Is(err, domain.ErrUnsupported) || !strings.Contains(err.Error(), "THINGS_AUTH_TOKEN") {
		t.Fatalf("UpdateTask without token = %v", err)
	}
	if _, err := b.SetTaskStatus(ctx, task.ID, domain.StatusCompleted); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("SetTaskStatus without token = %v", err)
	}
}

func TestRunnerErrorsAreRedactedAndUnavailable(t *testing.T) {
	b, sim := newBackend(t)
	task, _ := b.CreateTask(ctx, domain.NewTask{Title: "t"})
	sim.fail = errors.New("open: cannot open things:///update?auth-token=" + sim.token)
	title := "x"
	_, err := b.UpdateTask(ctx, task.ID, domain.TaskPatch{Title: &title})
	if !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), sim.token) {
		t.Fatalf("token leaked in error: %v", err)
	}
}

func TestCreateNotConfirmedReturnsWithoutID(t *testing.T) {
	b, sim := newBackend(t)
	sim.ignore = true
	task, err := b.CreateTask(ctx, domain.NewTask{Title: "lost in transit"})
	if err != nil {
		t.Fatalf("CreateTask = %v, want success without ID", err)
	}
	if task.ID != "" || task.Title != "lost in transit" {
		t.Fatalf("task = %+v", task)
	}
}

func TestIgnoredStatusChangeIsReported(t *testing.T) {
	b, sim := newBackend(t)
	task, _ := b.CreateTask(ctx, domain.NewTask{Title: "repeating"})
	sim.ignore = true
	if _, err := b.SetTaskStatus(ctx, task.ID, domain.StatusCompleted); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("SetTaskStatus ignored by Things = %v, want ErrUnsupported", err)
	}
}

func TestReadSemantics(t *testing.T) {
	b, sim := newBackend(t)
	// A repeating template must not appear; tasks of a trashed project are trashed.
	sim.exec(`INSERT INTO TMTask (uuid, type, status, trashed, title, start, rt1_recurrenceRule) VALUES ('TPL', 0, 0, 0, 'template', 1, x'00')`)
	sim.exec(`INSERT INTO TMTask (uuid, type, status, trashed, title, start) VALUES ('PX', 1, 0, 1, 'gone', 1)`)
	sim.exec(`INSERT INTO TMTask (uuid, type, status, trashed, title, start, project) VALUES ('TX', 0, 0, 0, 'child', 1, 'PX')`)

	page, err := b.ListTasks(ctx, domain.TaskQuery{Limit: domain.MaxPageLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Errorf("open tasks = %+v, want none", page.Items)
	}
	trash, _ := b.ListTasks(ctx, domain.TaskQuery{View: domain.ViewTrash})
	if len(trash.Items) != 1 || trash.Items[0].ID != "TX" {
		t.Errorf("trash = %+v", trash.Items)
	}
	if _, err := b.GetTask(ctx, "TPL"); err != nil {
		// Templates are excluded from listings, and from direct reads too.
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatal(err)
		}
	}
	if err := b.ShowInApp(ctx, "PX"); err != nil {
		t.Fatal(err)
	}
	if call := sim.lastCall(); len(call) != 2 || call[1] != "things:///show?id=PX" {
		t.Errorf("show call = %q", call)
	}
}

func TestStoreIsReadOnly(t *testing.T) {
	b, _ := newBackend(t)
	if _, err := b.store.db.Exec(`DELETE FROM TMTask`); err == nil {
		t.Fatal("the Things database must be opened read-only")
	}
}

func TestFindDatabase(t *testing.T) {
	home := t.TempDir()
	if _, err := FindDatabase(home); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("FindDatabase(empty home) = %v", err)
	}
	dir := filepath.Join(home, strings.Replace(filepath.Dir(DefaultDatabaseGlob), "ThingsData-*", "ThingsData-ABCDE", 1))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "main.sqlite")
	if err := os.WriteFile(want, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := FindDatabase(home); err != nil || got != want {
		t.Fatalf("FindDatabase = %q, %v", got, err)
	}
}
