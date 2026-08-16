package cmd

import (
	"strings"
	"testing"

	"github.com/yngvebn/linctl/pkg/api"
)

// A team carrying a label group ("Queue" with children Now/Next/Later), a
// top-level label whose name collides with one of those children ("Now"), and
// two ordinary labels.
func labelFixture() []api.Label {
	queue := &api.Label{ID: "grp-queue", Name: "Queue"}
	return []api.Label{
		{ID: "lbl-now", Name: "Now", Parent: queue},
		{ID: "lbl-next", Name: "Next", Parent: queue},
		{ID: "lbl-later", Name: "Later", Parent: queue},
		{ID: "lbl-toplevel-now", Name: "Now"},
		{ID: "lbl-bug", Name: "bug"},
		{ID: "lbl-fleet-safe", Name: "fleet-safe"},
	}
}

func TestResolveLabelRefByGroupPath(t *testing.T) {
	for _, ref := range []string{"Queue / Now", "queue / now", "Queue/Now", "  Queue /  Now  "} {
		got, err := ResolveLabelRef(labelFixture(), ref)
		if err != nil {
			t.Fatalf("ResolveLabelRef(%q): unexpected error: %v", ref, err)
		}
		if got != "lbl-now" {
			t.Fatalf("ResolveLabelRef(%q) = %q, want %q", ref, got, "lbl-now")
		}
	}
}

// The group path must not fall through to a same-named top-level label.
func TestResolveLabelRefGroupPathDoesNotMatchTopLevel(t *testing.T) {
	labels := []api.Label{{ID: "lbl-toplevel-now", Name: "Now"}}
	if _, err := ResolveLabelRef(labels, "Queue / Now"); err == nil {
		t.Fatal("expected an error when only a top-level 'Now' exists, got none")
	}
}

func TestResolveLabelRefByBareName(t *testing.T) {
	got, err := ResolveLabelRef(labelFixture(), "fleet-safe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "lbl-fleet-safe" {
		t.Fatalf("got %q, want %q", got, "lbl-fleet-safe")
	}
}

// A bare name matching both a group child and a top-level label is ambiguous.
// Guessing either one would silently select the wrong pool of issues.
func TestResolveLabelRefAmbiguousBareNameFails(t *testing.T) {
	_, err := ResolveLabelRef(labelFixture(), "Now")
	if err == nil {
		t.Fatal("expected an ambiguity error, got none")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error should explain the ambiguity, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Queue / Now") {
		t.Fatalf("error should list the qualified candidate, got: %v", err)
	}
}

// A bare child name is fine when nothing else shares it.
func TestResolveLabelRefUnambiguousGroupChildByBareName(t *testing.T) {
	got, err := ResolveLabelRef(labelFixture(), "Later")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "lbl-later" {
		t.Fatalf("got %q, want %q", got, "lbl-later")
	}
}

func TestResolveLabelRefByID(t *testing.T) {
	labels := []api.Label{{ID: "5dc5053c-7c7c-459f-9006-8d08906e2aa3", Name: "Now"}}
	got, err := ResolveLabelRef(labels, "5dc5053c-7c7c-459f-9006-8d08906e2aa3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "5dc5053c-7c7c-459f-9006-8d08906e2aa3" {
		t.Fatalf("got %q", got)
	}
}

// An ID that is not in the team is an error, not an empty result set — the
// whole point of resolving before querying.
func TestResolveLabelRefUnknownIDFails(t *testing.T) {
	_, err := ResolveLabelRef(labelFixture(), "5dc5053c-7c7c-459f-9006-8d08906e2aa3")
	if err == nil {
		t.Fatal("expected an error for an unknown label ID, got none")
	}
}

func TestResolveLabelRefUnknownNameFails(t *testing.T) {
	_, err := ResolveLabelRef(labelFixture(), "Queue / Nowe")
	if err == nil {
		t.Fatal("expected an error for a typo'd label, got none")
	}
	if !strings.Contains(err.Error(), "Queue / Nowe") {
		t.Fatalf("error should quote the reference it failed on, got: %v", err)
	}
}

func TestResolveLabelRefEmptyFails(t *testing.T) {
	if _, err := ResolveLabelRef(labelFixture(), "   "); err == nil {
		t.Fatal("expected an error for an empty reference, got none")
	}
}

// "/ Now" and "Queue /" are malformed rather than bare names; they must not
// silently degrade into a name lookup.
func TestResolveLabelRefMalformedPathFails(t *testing.T) {
	for _, ref := range []string{"/ Now", "Queue /", "/"} {
		if _, err := ResolveLabelRef(labelFixture(), ref); err == nil {
			t.Fatalf("expected an error for malformed reference %q, got none", ref)
		}
	}
}

func TestLabelPath(t *testing.T) {
	queue := &api.Label{ID: "grp-queue", Name: "Queue"}
	if got := LabelPath(api.Label{Name: "Now", Parent: queue}); got != "Queue / Now" {
		t.Fatalf("got %q, want %q", got, "Queue / Now")
	}
	if got := LabelPath(api.Label{Name: "bug"}); got != "bug" {
		t.Fatalf("got %q, want %q", got, "bug")
	}
}

func TestResolveLabelFlagWithoutTeamFails(t *testing.T) {
	_, err := resolveLabelFlag(t.Context(), nil, "Queue / Now", "")
	if err == nil {
		t.Fatal("expected an error when --label is given without --team, got none")
	}
	if !strings.Contains(err.Error(), "--team") {
		t.Fatalf("error should name the missing flag, got: %v", err)
	}
}

// No --label means no resolution and no round-trip, so a nil client is safe.
func TestResolveLabelFlagEmptyIsNoop(t *testing.T) {
	got, err := resolveLabelFlag(t.Context(), nil, "", "RET")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
