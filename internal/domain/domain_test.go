package domain

import (
	"errors"
	"strings"
	"testing"
)

var today = Date{Year: 2026, Month: 10, Day: 30}

func TestParseWhen(t *testing.T) {
	tests := []struct {
		in      string
		want    When
		wantErr bool
	}{
		{in: "today", want: When{Kind: WhenToday}},
		{in: "evening", want: When{Kind: WhenEvening}},
		{in: "anytime", want: When{Kind: WhenAnytime}},
		{in: "someday", want: When{Kind: WhenSomeday}},
		{in: "tomorrow", want: When{Kind: WhenDate, Date: Date{2026, 10, 31}}},
		{in: "2026-12-24", want: When{Kind: WhenDate, Date: Date{2026, 12, 24}}},
		{in: "2026-12-24@9:05", want: When{Kind: WhenDate, Date: Date{2026, 12, 24}, Reminder: "09:05"}},
		{in: "today@21:30", want: When{Kind: WhenToday, Reminder: "21:30"}},
		{in: "someday@10:00", wantErr: true},
		{in: "today@24:00", wantErr: true},
		{in: "2026-02-30", wantErr: true},
		{in: "next monday", wantErr: true},
		{in: "", wantErr: true},
		{in: "Today", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseWhen(tt.in, today)
			if tt.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("ParseWhen(%q) error = %v, want ErrValidation", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseWhen(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseWhen(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestWhenStringRoundTrip(t *testing.T) {
	for _, s := range []string{"today", "evening@18:00", "someday", "2027-01-02", "2027-01-02@08:15"} {
		w, err := ParseWhen(s, today)
		if err != nil {
			t.Fatal(err)
		}
		if w.String() != s {
			t.Errorf("String() = %q, want %q", w.String(), s)
		}
	}
}

func TestParseDeadline(t *testing.T) {
	if d, err := ParseDeadline("2026-11-05"); err != nil || d.String() != "2026-11-05" {
		t.Fatalf("ParseDeadline = %v, %v", d, err)
	}
	if _, err := ParseDeadline("today"); !errors.Is(err, ErrValidation) {
		t.Fatalf("ParseDeadline(today) error = %v, want ErrValidation", err)
	}
}

func TestValidateID(t *testing.T) {
	for _, id := range []string{"7nHmVqT3cZ4yGk2bX9wR1a", "8C4E3A9B-1234-4F00-9ABC-0123456789AB"} {
		if err := ValidateID("id", id); err != nil {
			t.Errorf("ValidateID(%q) = %v", id, err)
		}
	}
	for _, id := range []string{"", `abc"; do shell script "rm`, "a/b", "id with space", strings.Repeat("a", 65)} {
		if err := ValidateID("id", id); !errors.Is(err, ErrValidation) {
			t.Errorf("ValidateID(%q) = %v, want ErrValidation", id, err)
		}
	}
}

func TestNewTaskValidate(t *testing.T) {
	ok := NewTask{Title: "Buy milk", Tags: []string{"errand"}, Checklist: []string{"a", "b"}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid task rejected: %v", err)
	}
	bad := map[string]NewTask{
		"empty title":         {Title: "  "},
		"multiline title":     {Title: "a\nb"},
		"title too long":      {Title: strings.Repeat("x", MaxStringLen+1)},
		"notes too long":      {Title: "t", Notes: strings.Repeat("x", MaxNotesLen+1)},
		"comma in tag":        {Title: "t", Tags: []string{"a,b"}},
		"checklist too long":  {Title: "t", Checklist: make([]string, MaxChecklistLen+1)},
		"project and area":    {Title: "t", ProjectID: "p1", AreaID: "a1"},
		"heading w/o project": {Title: "t", HeadingID: "h1"},
		"bad project id":      {Title: "t", ProjectID: "../x"},
	}
	for name, n := range bad {
		t.Run(name, func(t *testing.T) {
			if err := n.Validate(); !errors.Is(err, ErrValidation) {
				t.Fatalf("Validate() = %v, want ErrValidation", err)
			}
		})
	}
}

func TestNewProjectValidateCountsItems(t *testing.T) {
	p := NewProject{Title: "p", Tasks: make([]string, MaxProjectTasks)}
	for i := range p.Tasks {
		p.Tasks[i] = "t"
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("project at the limit rejected: %v", err)
	}
	p.Headings = []NewHeading{{Title: "h"}}
	if err := p.Validate(); !errors.Is(err, ErrValidation) {
		t.Fatalf("project over the limit accepted: %v", err)
	}
}

func TestTaskPatchValidate(t *testing.T) {
	s := func(v string) *string { return &v }
	if err := (TaskPatch{Title: s("new")}).Validate(); err != nil {
		t.Fatalf("valid patch rejected: %v", err)
	}
	bad := map[string]TaskPatch{
		"empty":                  {},
		"notes and append_notes": {Notes: s("a"), AppendNotes: "b"},
		"tags and add_tags":      {Tags: &[]string{"a"}, AddTags: []string{"b"}},
		"checklist and append":   {Checklist: &[]string{"a"}, AppendChecklist: []string{"b"}},
		"project and area":       {ProjectID: s("p"), AreaID: s("a")},
		"empty title":            {Title: s("")},
		"bad heading id":         {HeadingID: s("")},
	}
	for name, p := range bad {
		t.Run(name, func(t *testing.T) {
			if err := p.Validate(); !errors.Is(err, ErrValidation) {
				t.Fatalf("Validate() = %v, want ErrValidation", err)
			}
		})
	}
}

func TestProjectPatchValidate(t *testing.T) {
	if err := (ProjectPatch{}).Validate(); !errors.Is(err, ErrValidation) {
		t.Fatal("empty project patch accepted")
	}
	if err := (ProjectPatch{AddTags: []string{"x"}}).Validate(); err != nil {
		t.Fatalf("valid project patch rejected: %v", err)
	}
}

func TestQueryNormalize(t *testing.T) {
	q := TaskQuery{}
	if err := q.Normalize(); err != nil || q.Limit != DefaultPageLimit {
		t.Fatalf("Normalize() = %v, limit %d", err, q.Limit)
	}
	for _, q := range []TaskQuery{{Limit: -1}, {Limit: MaxPageLimit + 1}, {View: "nope"}} {
		if err := q.Normalize(); !errors.Is(err, ErrValidation) {
			t.Errorf("Normalize(%+v) = %v, want ErrValidation", q, err)
		}
	}
	pq := ProjectQuery{}
	if err := pq.Normalize(); err != nil || pq.Status != StatusOpen {
		t.Fatalf("ProjectQuery.Normalize() = %v, status %q", err, pq.Status)
	}
	for _, sq := range []SearchQuery{{}, {Text: "x", Kind: "area"}, {Text: "x", Status: "done"}} {
		if err := sq.Normalize(); !errors.Is(err, ErrValidation) {
			t.Errorf("SearchQuery.Normalize(%+v) = %v, want ErrValidation", sq, err)
		}
	}
}

func TestInView(t *testing.T) {
	inProject := Task{Status: StatusOpen, ProjectID: "p"}
	tests := []struct {
		name string
		task Task
		want []View
	}{
		{"inbox", Task{Status: StatusOpen}, []View{ViewInbox}},
		{"today", Task{Status: StatusOpen, When: When{Kind: WhenToday}}, []View{ViewToday, ViewAnytime}},
		{"evening", Task{Status: StatusOpen, When: When{Kind: WhenEvening}}, []View{ViewToday, ViewAnytime}},
		{"overdue date", Task{Status: StatusOpen, When: When{Kind: WhenDate, Date: today.AddDays(-2)}}, []View{ViewToday, ViewAnytime}},
		{"future date", Task{Status: StatusOpen, When: When{Kind: WhenDate, Date: today.AddDays(1)}}, []View{ViewUpcoming}},
		{"someday", Task{Status: StatusOpen, When: When{Kind: WhenSomeday}}, []View{ViewSomeday}},
		{"unscheduled in project", inProject, []View{ViewAnytime}},
		{"completed", Task{Status: StatusCompleted, When: When{Kind: WhenToday}}, []View{ViewLogbook}},
		{"trashed", Task{Status: StatusOpen, Trashed: true}, []View{ViewTrash}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := map[View]bool{}
			for _, v := range tt.want {
				want[v] = true
			}
			for _, v := range Views {
				if got := InView(tt.task, v, today); got != want[v] {
					t.Errorf("InView(%s) = %v, want %v", v, got, want[v])
				}
			}
		})
	}
}
