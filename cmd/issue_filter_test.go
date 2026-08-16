package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// filterTestCmd builds a throwaway command carrying the same filter flags as
// `issue list`, so composition can be asserted without mutating the real
// command's flag state between tests.
func filterTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("assignee", "", "")
	cmd.Flags().String("state", "", "")
	cmd.Flags().String("team", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("label", "", "")
	cmd.Flags().String("cycle", "", "")
	cmd.Flags().Int("priority", -1, "")
	cmd.Flags().Bool("include-completed", false, "")
	cmd.Flags().String("newer-than", "all_time", "")
	return cmd
}

func setFlag(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	if err := cmd.Flags().Set(name, value); err != nil {
		t.Fatalf("set %s=%s: %v", name, value, err)
	}
}

// The label filter must key on ID. A name filter cannot address a grouped
// label, because the issue list query returns `parent: null` for group children.
func TestBuildIssueFilterLabelFiltersByID(t *testing.T) {
	filter := buildIssueFilter(filterTestCmd(t), "lbl-now")

	labels, ok := filter["labels"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a labels filter, got %#v", filter["labels"])
	}
	some, ok := labels["some"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected labels.some, got %#v", labels["some"])
	}
	id, ok := some["id"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected labels.some.id, got %#v", some["id"])
	}
	if id["eq"] != "lbl-now" {
		t.Fatalf("labels.some.id.eq = %#v, want %q", id["eq"], "lbl-now")
	}
	if _, hasName := some["name"]; hasName {
		t.Fatal("must not filter labels by name — group children are indistinguishable by name in list payloads")
	}
}

// No --label leaves the filter exactly as it was before this feature existed.
func TestBuildIssueFilterWithoutLabelIsUnchanged(t *testing.T) {
	filter := buildIssueFilter(filterTestCmd(t), "")
	if _, present := filter["labels"]; present {
		t.Fatalf("expected no labels key, got %#v", filter["labels"])
	}
}

// The label filter composes with the other filters rather than replacing them.
func TestBuildIssueFilterLabelComposesWithOtherFilters(t *testing.T) {
	cmd := filterTestCmd(t)
	setFlag(t, cmd, "team", "RET")
	setFlag(t, cmd, "state", "Todo")
	setFlag(t, cmd, "priority", "2")
	setFlag(t, cmd, "project", "58cadfb9-15bf-4b13-b067-7cc36b325c7e")

	filter := buildIssueFilter(cmd, "lbl-now")

	for _, key := range []string{"labels", "team", "state", "priority", "project"} {
		if _, present := filter[key]; !present {
			t.Fatalf("expected %q in the composed filter, got keys %v", key, filterKeys(filter))
		}
	}

	team := filter["team"].(map[string]interface{})["key"].(map[string]interface{})
	if team["eq"] != "RET" {
		t.Fatalf("team filter clobbered: %#v", team)
	}
	state := filter["state"].(map[string]interface{})["name"].(map[string]interface{})
	if state["eq"] != "Todo" {
		t.Fatalf("state filter clobbered: %#v", state)
	}
}

// --include-completed governs the state filter; the label filter must not
// interfere with it in either direction.
func TestBuildIssueFilterLabelWithIncludeCompleted(t *testing.T) {
	cmd := filterTestCmd(t)
	setFlag(t, cmd, "include-completed", "true")

	filter := buildIssueFilter(cmd, "lbl-now")

	if _, present := filter["labels"]; !present {
		t.Fatal("expected the labels filter to survive --include-completed")
	}
	if _, present := filter["state"]; present {
		t.Fatalf("--include-completed should drop the state filter, got %#v", filter["state"])
	}
}

func filterKeys(filter map[string]interface{}) []string {
	keys := make([]string, 0, len(filter))
	for k := range filter {
		keys = append(keys, k)
	}
	return keys
}
