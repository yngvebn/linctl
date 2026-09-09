package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/yngvebn/linctl/pkg/api"
	"github.com/yngvebn/linctl/pkg/auth"
	"github.com/yngvebn/linctl/pkg/output"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// apiRelationTypes lists the relation types in the Linear API's IssueRelationType enum.
// Note: "blocked-by" is NOT an API type — it's a CLI convenience that maps to "blocks"
// with swapped issue IDs. See the --blocked-by flag on issueRelationAddCmd.
var apiRelationTypes = []string{"blocks", "duplicate", "related", "similar"}

// issueRelationCmd is the parent command for issue relation management.
var issueRelationCmd = &cobra.Command{
	Use:   "relation",
	Short: "Manage issue relations",
	Long: `Manage relations between Linear issues (blocking, blocked-by, related, duplicate, similar).

Examples:
  linctl issue relation list LIN-123
  linctl issue relation add LIN-123 --blocks LIN-456
  linctl issue relation add LIN-123 --blocked-by LIN-456
  linctl issue relation add LIN-123 --related LIN-456
  linctl issue relation add LIN-123 --duplicate LIN-456
  linctl issue relation add LIN-123 --similar LIN-456
  linctl issue relation remove RELATION-ID`,
}

var issueRelationListCmd = &cobra.Command{
	Use:     "list ISSUE-ID",
	Aliases: []string{"ls"},
	Short:   "List relations for an issue",
	Long: `List all relations (blocking, blocked-by, related, duplicate, similar) for an issue.

Examples:
  linctl issue relation list LIN-123
  linctl issue relation ls LIN-123 -j`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		plaintext := viper.GetBool("plaintext")
		jsonOut := viper.GetBool("json")
		issueID := args[0]

		authHeader, err := auth.GetAuthHeader()
		if err != nil {
			output.Error("Not authenticated. Run 'linctl auth' first.", plaintext, jsonOut)
			os.Exit(1)
		}

		client := api.NewClient(authHeader)

		relations, err := client.GetIssueRelations(context.Background(), issueID)
		if err != nil {
			output.Error(fmt.Sprintf("Failed to fetch relations: %v", err), plaintext, jsonOut)
			os.Exit(1)
		}

		kinds, _ := cmd.Flags().GetStringSlice("kind")
		matches, err := parseRelationKinds(kinds)
		if err != nil {
			output.Error(err.Error(), plaintext, jsonOut)
			os.Exit(1)
		}
		if len(kinds) > 0 {
			filtered := make([]api.IssueRelation, 0, len(relations))
			for _, rel := range relations {
				if matches(rel) {
					filtered = append(filtered, rel)
				}
			}
			relations = filtered
		}

		// Most-actionable first: blocked-by, blocks, duplicate, similar, related.
		sortRelationsByActionability(relations)

		if len(relations) == 0 {
			if len(kinds) > 0 {
				output.Info(fmt.Sprintf("No %s relations found for %s", strings.Join(kinds, ","), issueID), plaintext, jsonOut)
			} else {
				output.Info(fmt.Sprintf("No relations found for %s", issueID), plaintext, jsonOut)
			}
			return
		}

		if jsonOut {
			output.JSON(relations)
			return
		}

		if plaintext {
			for _, rel := range relations {
				other := relationOtherIssue(&rel)
				fmt.Printf("%s\t%s\t%s\t%s\n", rel.ID, relationTypeLabel(rel.Type, rel.Inverse), other.Identifier, other.Title)
			}
			return
		}

		// Rich display
		fmt.Printf("\n%s Relations for %s (%d)\n\n",
			color.New(color.FgCyan, color.Bold).Sprint("🔗"),
			color.New(color.FgCyan).Sprint(issueID),
			len(relations))

		for _, rel := range relations {
			other := relationOtherIssue(&rel)
			typeLabel := relationTypeLabel(rel.Type, rel.Inverse)
			fmt.Printf("  %s %s %s\n",
				color.New(color.FgYellow).Sprint(typeLabel),
				color.New(color.FgCyan, color.Bold).Sprint(other.Identifier),
				other.Title)
			fmt.Printf("    %s\n\n",
				color.New(color.FgWhite, color.Faint).Sprintf("relation-id: %s", rel.ID))
		}
	},
}

var issueRelationAddCmd = &cobra.Command{
	Use:     "add ISSUE-ID",
	Aliases: []string{"create", "new"},
	Short:   "Add a relation to an issue",
	Long: `Create a relation between two issues.

Relation types:
  --blocks ISSUE-ID      This issue blocks the specified issue
  --blocked-by ISSUE-ID  This issue is blocked by the specified issue
  --related ISSUE-ID     Mark issues as related
  --duplicate ISSUE-ID   Mark this issue as a duplicate of the specified issue
  --similar ISSUE-ID     Mark issues as similar

Examples:
  linctl issue relation add LIN-123 --blocks LIN-456
  linctl issue relation add LIN-123 --blocked-by LIN-456
  linctl issue relation add LIN-123 --related LIN-456
  linctl issue relation add LIN-123 --duplicate LIN-456
  linctl issue relation add LIN-123 --similar LIN-456`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		plaintext := viper.GetBool("plaintext")
		jsonOut := viper.GetBool("json")
		issueID := args[0]

		authHeader, err := auth.GetAuthHeader()
		if err != nil {
			output.Error("Not authenticated. Run 'linctl auth' first.", plaintext, jsonOut)
			os.Exit(1)
		}

		// Determine which relation type was specified
		blocks, _ := cmd.Flags().GetString("blocks")
		blockedBy, _ := cmd.Flags().GetString("blocked-by")
		related, _ := cmd.Flags().GetString("related")
		duplicate, _ := cmd.Flags().GetString("duplicate")
		similar, _ := cmd.Flags().GetString("similar")

		var relatedIssueID string
		var relationType string

		// Count how many flags were set
		flagCount := 0
		if blocks != "" {
			flagCount++
			relatedIssueID = blocks
			relationType = "blocks"
		}
		if blockedBy != "" {
			flagCount++
			relatedIssueID = blockedBy
			relationType = "blocked-by"
		}
		if related != "" {
			flagCount++
			relatedIssueID = related
			relationType = "related"
		}
		if duplicate != "" {
			flagCount++
			relatedIssueID = duplicate
			relationType = "duplicate"
		}
		if similar != "" {
			flagCount++
			relatedIssueID = similar
			relationType = "similar"
		}

		if flagCount == 0 {
			output.Error("Must specify one of: --blocks, --blocked-by, --related, --duplicate, --similar", plaintext, jsonOut)
			os.Exit(1)
		}
		if flagCount > 1 {
			output.Error("Only one relation type can be specified at a time", plaintext, jsonOut)
			os.Exit(1)
		}

		client := api.NewClient(authHeader)

		// Resolve both issue IDs to UUIDs
		issue, err := client.GetIssue(context.Background(), issueID)
		if err != nil {
			output.Error(fmt.Sprintf("Failed to fetch issue %s: %v", issueID, err), plaintext, jsonOut)
			os.Exit(1)
		}
		relatedIssue, err := client.GetIssue(context.Background(), relatedIssueID)
		if err != nil {
			output.Error(fmt.Sprintf("Failed to fetch issue %s: %v", relatedIssueID, err), plaintext, jsonOut)
			os.Exit(1)
		}

		// Map the CLI relation type to the Linear API type.
		// Linear's issueRelationCreate uses:
		//   issueId        = the SUBJECT of the relation
		//   relatedIssueId = the OBJECT of the relation
		//   type = "blocks" means issueId BLOCKS relatedIssueId
		//
		// The subject/object reading is the one that matters, and it used to be
		// documented here the other way round ("issueId is blocked by
		// relatedIssueId"). Both blocking cases below were derived from that
		// premise, so both were written inverted: `add A --blocked-by B` stored
		// "A blocks B". Verified against the live API on 2026-08-27 by reading
		// back a relation created through this command and comparing it with the
		// same relation as Linear's own UI renders it.
		//
		// CLI semantics:
		//   --blocks TARGET     => "this issue blocks TARGET"
		//                       => API: issueId=THIS, relatedIssueId=TARGET, type=blocks
		//   --blocked-by SOURCE => "this issue is blocked by SOURCE"
		//                       => SOURCE blocks THIS
		//                       => API: issueId=SOURCE, relatedIssueId=THIS, type=blocks
		//   --related TARGET    => API: issueId=THIS, relatedIssueId=TARGET, type=related
		//   --duplicate TARGET  => API: issueId=THIS, relatedIssueId=TARGET, type=duplicate
		//
		// Only the two blocking cases were wrong. "duplicate" reads correctly
		// under the corrected premise — issueId is a duplicate OF relatedIssueId —
		// so it is deliberately left as it was.

		var apiIssueID, apiRelatedIssueID, apiType string

		switch relationType {
		case "blocks":
			// "LIN-123 blocks LIN-456" => subject LIN-123, object LIN-456
			apiIssueID = issue.ID
			apiRelatedIssueID = relatedIssue.ID
			apiType = "blocks"
		case "blocked-by":
			// "LIN-123 is blocked by LIN-456" => subject LIN-456, object LIN-123
			apiIssueID = relatedIssue.ID
			apiRelatedIssueID = issue.ID
			apiType = "blocks"
		case "related":
			apiIssueID = issue.ID
			apiRelatedIssueID = relatedIssue.ID
			apiType = "related"
		case "duplicate":
			apiIssueID = issue.ID
			apiRelatedIssueID = relatedIssue.ID
			apiType = "duplicate"
		case "similar":
			apiIssueID = issue.ID
			apiRelatedIssueID = relatedIssue.ID
			apiType = "similar"
		}

		relation, err := client.CreateIssueRelation(context.Background(), apiIssueID, apiRelatedIssueID, apiType)
		if err != nil {
			output.Error(fmt.Sprintf("Failed to create relation: %v", err), plaintext, jsonOut)
			os.Exit(1)
		}

		if jsonOut {
			output.JSON(relation)
			return
		}

		if plaintext {
			fmt.Printf("Created %s relation between %s and %s\n", relationType, issueID, relatedIssueID)
			fmt.Printf("Relation ID: %s\n", relation.ID)
			return
		}

		// Rich display
		fmt.Printf("%s Created relation: %s %s %s\n",
			color.New(color.FgGreen).Sprint("✓"),
			color.New(color.FgCyan, color.Bold).Sprint(issueID),
			color.New(color.FgYellow).Sprint(relationType),
			color.New(color.FgCyan, color.Bold).Sprint(relatedIssueID))
	},
}

var issueRelationRemoveCmd = &cobra.Command{
	Use:     "remove RELATION-ID",
	Aliases: []string{"delete", "rm"},
	Short:   "Remove a relation by its ID",
	Long: `Remove a relation between two issues.

Use 'linctl issue relation list ISSUE-ID' to find relation IDs.

Examples:
  linctl issue relation remove abc123-def456
  linctl issue relation rm abc123-def456`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		plaintext := viper.GetBool("plaintext")
		jsonOut := viper.GetBool("json")
		relationID := args[0]

		authHeader, err := auth.GetAuthHeader()
		if err != nil {
			output.Error("Not authenticated. Run 'linctl auth' first.", plaintext, jsonOut)
			os.Exit(1)
		}

		client := api.NewClient(authHeader)

		err = client.DeleteIssueRelation(context.Background(), relationID)
		if err != nil {
			output.Error(fmt.Sprintf("Failed to remove relation: %v", err), plaintext, jsonOut)
			os.Exit(1)
		}

		if jsonOut {
			output.JSON(map[string]interface{}{
				"deleted":    true,
				"relationId": relationID,
			})
			return
		}

		if plaintext {
			fmt.Printf("Removed relation %s\n", relationID)
			return
		}

		fmt.Printf("%s Removed relation %s\n",
			color.New(color.FgGreen).Sprint("✓"),
			color.New(color.FgCyan, color.Bold).Sprint(relationID))
	},
}

// relationOtherIssue returns the issue at the far end of a relation, from the
// point of view of the issue that was queried.
//
// A forward row (issue.relations) has Issue == the queried issue, so the far end
// is RelatedIssue. An inverse row (issue.inverseRelations) has RelatedIssue ==
// the queried issue, so the far end is Issue. Preferring RelatedIssue
// unconditionally — as this did — made every inverse row name the issue you were
// already looking at, which rendered as nonsense like "RET-1061 blocks RET-1061"
// on RET-1061's own listing.
func relationOtherIssue(rel *api.IssueRelation) *api.Issue {
	if rel.Inverse {
		if rel.Issue != nil {
			return rel.Issue
		}
		if rel.RelatedIssue != nil {
			return rel.RelatedIssue
		}
		return &api.Issue{Identifier: "?", Title: "unknown"}
	}
	if rel.RelatedIssue != nil {
		return rel.RelatedIssue
	}
	if rel.Issue != nil {
		return rel.Issue
	}
	return &api.Issue{Identifier: "?", Title: "unknown"}
}

// relationTypeLabel returns a human-readable label for a relation type, phrased
// from the queried issue's point of view.
//
// A forward "blocks" row means the queried issue is the subject: it BLOCKS the
// other one. An inverse row means it is the object: it is BLOCKED BY the other
// one. These two were the wrong way round, which cancelled out the inverted
// write path in `add` and made the CLI look self-consistent while disagreeing
// with Linear.
func relationTypeLabel(t string, inverse bool) string {
	switch strings.ToLower(t) {
	case "blocks":
		if inverse {
			return "blocked by"
		}
		return "blocks"
	case "duplicate":
		if inverse {
			return "has duplicate"
		}
		return "duplicate of"
	case "related":
		return "related to"
	case "similar":
		return "similar to"
	default:
		return t
	}
}

// relationRank orders a relation by how much it should change what you do next,
// phrased from the queried issue's point of view. Blockers come first because they
// gate work; duplicates next because they decide whether to work at all; `related`
// last because it is context rather than an instruction.
//
// `related` is deliberately shown rather than filtered out. It is 73% of the edges in
// the retrospective.fun workspace and carries provenance — "this fell out of that
// epic" — plus the prior-art graph: RET-853's six `related` edges are its six sibling
// GDPR tickets, which is exactly what you want when picking it up cold. Ranking
// de-emphasises it by position without hiding it. Use --kind when you want less.
func relationRank(t string, inverse bool) int {
	switch strings.ToLower(t) {
	case "blocks":
		if inverse {
			return 0 // blocked by — someone else gates this
		}
		return 1 // blocks — this gates someone else
	case "duplicate":
		return 2
	case "similar":
		return 3
	case "related":
		return 4
	default:
		return 5
	}
}

// sortRelationsByActionability sorts in place, most-actionable first, keeping the
// server's order within a rank so repeated runs read the same.
func sortRelationsByActionability(rels []api.IssueRelation) {
	sort.SliceStable(rels, func(i, j int) bool {
		return relationRank(rels[i].Type, rels[i].Inverse) < relationRank(rels[j].Type, rels[j].Inverse)
	})
}

// relationKindAliases maps what a caller would type to Linear's stored relation type.
// "blocked-by" and "blocks" are the same stored type in opposite directions, so they
// resolve to the type plus a direction the filter has to check.
var relationKindAliases = map[string]struct {
	Type           string
	RequireInverse *bool
}{
	"blocks":     {Type: "blocks", RequireInverse: boolPtr(false)},
	"blocked-by": {Type: "blocks", RequireInverse: boolPtr(true)},
	"blocked":    {Type: "blocks", RequireInverse: boolPtr(true)},
	"duplicate":  {Type: "duplicate"},
	"related":    {Type: "related"},
	"similar":    {Type: "similar"},
}

func boolPtr(b bool) *bool { return &b }

// parseRelationKinds turns --kind values into a matcher. An unknown kind is an error
// rather than an empty result: silently matching nothing would read as "no relations",
// which is the failure mode this whole area keeps producing.
func parseRelationKinds(kinds []string) (func(api.IssueRelation) bool, error) {
	if len(kinds) == 0 {
		return func(api.IssueRelation) bool { return true }, nil
	}

	type want struct {
		Type           string
		RequireInverse *bool
	}
	var wants []want
	for _, raw := range kinds {
		for _, k := range strings.Split(raw, ",") {
			k = strings.TrimSpace(strings.ToLower(k))
			if k == "" {
				continue
			}
			alias, ok := relationKindAliases[k]
			if !ok {
				valid := make([]string, 0, len(relationKindAliases))
				for name := range relationKindAliases {
					valid = append(valid, name)
				}
				sort.Strings(valid)
				return nil, fmt.Errorf("unknown relation kind %q (valid: %s)", k, strings.Join(valid, ", "))
			}
			wants = append(wants, want{Type: alias.Type, RequireInverse: alias.RequireInverse})
		}
	}
	if len(wants) == 0 {
		return func(api.IssueRelation) bool { return true }, nil
	}

	return func(rel api.IssueRelation) bool {
		relType := strings.ToLower(rel.Type)
		for _, w := range wants {
			if relType != w.Type {
				continue
			}
			if w.RequireInverse == nil || *w.RequireInverse == rel.Inverse {
				return true
			}
		}
		return false
	}, nil
}

func init() {
	issueCmd.AddCommand(issueRelationCmd)
	issueRelationCmd.AddCommand(issueRelationListCmd)
	issueRelationCmd.AddCommand(issueRelationAddCmd)
	issueRelationCmd.AddCommand(issueRelationRemoveCmd)

	// Add command flags
	issueRelationAddCmd.Flags().String("blocks", "", "Issue that this issue blocks (issue identifier)")
	issueRelationAddCmd.Flags().String("blocked-by", "", "Issue that blocks this issue (issue identifier)")
	issueRelationAddCmd.Flags().String("related", "", "Related issue (issue identifier)")
	issueRelationAddCmd.Flags().String("duplicate", "", "Issue that this is a duplicate of (issue identifier)")
	issueRelationAddCmd.Flags().String("similar", "", "Issue that is similar to this issue (issue identifier)")

	// Opt-in narrowing for scripted callers. Off by default: nothing is hidden unless
	// asked for, because a relation you did not know existed is the one worth seeing.
	issueRelationListCmd.Flags().StringSlice("kind", nil, "Only show these relation kinds (blocks, blocked-by, duplicate, related, similar); comma-separated or repeated")
}
