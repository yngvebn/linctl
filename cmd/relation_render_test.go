package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/yngvebn/linctl/pkg/api"
)

// The relation rendering in `issue get` used to be a hand-rolled copy of what
// relation.go already did correctly: it read relation.RelatedIssue unconditionally
// and mapped the type with a local switch. On an inverse row RelatedIssue IS the
// queried issue, so every inverse relation named the issue you were already looking
// at, with the direction backwards.
//
// The consequence was worse than cosmetic. On RET-1047 — blocked by RET-1048 and
// blocking RET-1046 — the old output read:
//
//   - Blocks: RET-1046 ...
//   - Blocks: RET-1047 ...   <- itself, and RET-1048 nowhere to be seen
//
// so an agent deciding whether RET-1047 could be worked saw no blocker at all.
// This test is shaped like that issue and goes red on the original code.
func TestIssueGetRendersRelationCounterpartyNotItself(t *testing.T) {
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()
	t.Setenv("LINCTL_API_KEY", "test-key")
	viper.Set("plaintext", true)
	viper.Set("json", false)
	defer viper.Set("plaintext", false)

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var gqlReq gqlCommandTestRequest
		if err := json.NewDecoder(req.Body).Decode(&gqlReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		// Forward row: LIN-100 blocks LIN-46.
		// Inverse row: LIN-48 blocks LIN-100, i.e. LIN-100 is blocked by LIN-48.
		body := `{"data":{"issue":{
			"id":"uuid-100","identifier":"LIN-100","title":"Backfill the topic variant","number":100,
			"state":{"name":"Backlog","type":"backlog"},
			"relations":{"nodes":[
				{"id":"rel-1","type":"blocks",
				 "issue":{"id":"uuid-100","identifier":"LIN-100","title":"Backfill the topic variant","state":{"name":"Backlog","type":"backlog"}},
				 "relatedIssue":{"id":"uuid-46","identifier":"LIN-46","title":"Build the topic embedding variant","state":{"name":"Backlog","type":"backlog"}}}
			]},
			"inverseRelations":{"nodes":[
				{"id":"rel-2","type":"blocks",
				 "issue":{"id":"uuid-48","identifier":"LIN-48","title":"Ratchet every embeddable row","state":{"name":"Todo","type":"unstarted"}},
				 "relatedIssue":{"id":"uuid-100","identifier":"LIN-100","title":"Backfill the topic variant","state":{"name":"Backlog","type":"backlog"}}}
			]}
		}}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	issueGetCmd.Run(issueGetCmd, []string{"LIN-100"})
	w.Close()
	var buf strings.Builder
	io.Copy(&buf, r)
	os.Stdout = oldStdout
	got := buf.String()

	section := relatedIssuesSection(got)
	if section == "" {
		t.Fatalf("expected a Related Issues section, got:\n%s", got)
	}

	// The blocker must be named. Its absence is the defect that hid RET-1048.
	if !strings.Contains(section, "LIN-48") {
		t.Fatalf("expected the blocker LIN-48 to be named, got:\n%s", section)
	}
	if !strings.Contains(section, "blocked by") {
		t.Fatalf("expected the inverse row phrased 'blocked by', got:\n%s", section)
	}
	if !strings.Contains(section, "LIN-46") || !strings.Contains(section, "blocks") {
		t.Fatalf("expected the forward row to read 'blocks LIN-46', got:\n%s", section)
	}
	// No row may name the queried issue: that is the self-referential bug.
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "-") && strings.Contains(line, "LIN-100") {
			t.Fatalf("a relation row named the queried issue LIN-100:\n%s", section)
		}
	}
	// Blocked-by outranks blocks, so the thing gating this issue reads first.
	if strings.Index(section, "LIN-48") > strings.Index(section, "LIN-46") {
		t.Fatalf("expected blocked-by ordered before blocks, got:\n%s", section)
	}
	// State comes off the counterparty, not the queried issue. The GetIssue query
	// selected state on relatedIssue but not on issue, so an inverse row used to
	// arrive with state:null — invisible while the renderer read the wrong node.
	if !strings.Contains(section, "[Todo]") {
		t.Fatalf("expected LIN-48's own state [Todo] on the inverse row, got:\n%s", section)
	}
}

func relatedIssuesSection(out string) string {
	start := strings.Index(out, "## Related Issues")
	if start < 0 {
		return ""
	}
	rest := out[start:]
	if end := strings.Index(rest[1:], "\n## "); end >= 0 {
		return rest[:end+1]
	}
	return rest
}

func TestRelationRankOrdersByWhatChangesYourNextMove(t *testing.T) {
	// blocked-by first (someone gates you), then blocks, duplicate, similar, related.
	ordered := []struct {
		Type    string
		Inverse bool
		Name    string
	}{
		{"blocks", true, "blocked by"},
		{"blocks", false, "blocks"},
		{"duplicate", false, "duplicate of"},
		{"similar", false, "similar to"},
		{"related", false, "related to"},
	}
	for i := 1; i < len(ordered); i++ {
		prev := relationRank(ordered[i-1].Type, ordered[i-1].Inverse)
		cur := relationRank(ordered[i].Type, ordered[i].Inverse)
		if prev >= cur {
			t.Fatalf("%s (rank %d) should outrank %s (rank %d)",
				ordered[i-1].Name, prev, ordered[i].Name, cur)
		}
	}
	if got := relationRank("BLOCKS", true); got != relationRank("blocks", true) {
		t.Fatalf("rank should be case-insensitive, got %d", got)
	}
}

func TestSortRelationsIsStableWithinARank(t *testing.T) {
	rels := []api.IssueRelation{
		{ID: "a", Type: "related"},
		{ID: "b", Type: "duplicate"},
		{ID: "c", Type: "related"},
		{ID: "d", Type: "blocks", Inverse: true},
		{ID: "e", Type: "blocks"},
	}
	sortRelationsByActionability(rels)
	var order []string
	for _, r := range rels {
		order = append(order, r.ID)
	}
	want := []string{"d", "e", "b", "a", "c"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("expected order %v (server order kept within a rank), got %v", want, order)
		}
	}
}

func TestParseRelationKinds(t *testing.T) {
	blockedBy := api.IssueRelation{Type: "blocks", Inverse: true}
	blocks := api.IssueRelation{Type: "blocks"}
	dup := api.IssueRelation{Type: "duplicate"}
	rel := api.IssueRelation{Type: "related"}

	cases := []struct {
		name  string
		kinds []string
		want  map[string]bool // label -> should match
	}{
		{"no kinds matches everything", nil,
			map[string]bool{"blocked-by": true, "blocks": true, "duplicate": true, "related": true}},
		// blocks and blocked-by are one stored type in two directions; the filter
		// has to discriminate on direction, not just type.
		{"blocked-by excludes forward blocks", []string{"blocked-by"},
			map[string]bool{"blocked-by": true, "blocks": false, "duplicate": false, "related": false}},
		{"blocks excludes inverse", []string{"blocks"},
			map[string]bool{"blocked-by": false, "blocks": true, "duplicate": false, "related": false}},
		{"comma-separated", []string{"duplicate,related"},
			map[string]bool{"blocked-by": false, "blocks": false, "duplicate": true, "related": true}},
		{"repeated flag", []string{"duplicate", "related"},
			map[string]bool{"blocked-by": false, "blocks": false, "duplicate": true, "related": true}},
		{"both blocks directions", []string{"blocks", "blocked-by"},
			map[string]bool{"blocked-by": true, "blocks": true, "duplicate": false, "related": false}},
		{"case and whitespace tolerated", []string{" DUPLICATE , related "},
			map[string]bool{"duplicate": true, "related": true, "blocks": false}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			match, err := parseRelationKinds(tc.kinds)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			subjects := map[string]api.IssueRelation{
				"blocked-by": blockedBy, "blocks": blocks, "duplicate": dup, "related": rel,
			}
			for label, want := range tc.want {
				if got := match(subjects[label]); got != want {
					t.Fatalf("%s: match(%s) = %v, want %v", tc.name, label, got, want)
				}
			}
		})
	}
}

// An unknown kind must fail loudly. Matching nothing would render as "no relations
// found", which is the one wrong answer that reads like a valid one.
func TestParseRelationKindsRejectsUnknownKind(t *testing.T) {
	_, err := parseRelationKinds([]string{"duplicate", "bogus"})
	if err == nil {
		t.Fatal("expected an error for an unknown relation kind, got nil")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("error should name the offending kind, got: %v", err)
	}
	if !strings.Contains(err.Error(), "blocked-by") {
		t.Fatalf("error should list the valid kinds, got: %v", err)
	}
}

func TestRelationListKindFlagRegistered(t *testing.T) {
	if issueRelationListCmd.Flags().Lookup("kind") == nil {
		t.Fatal("expected --kind on `issue relation list`")
	}
}
