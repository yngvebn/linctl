package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/yngvebn/linctl/pkg/api"
)

// LabelPath renders a label the way Linear displays it: "Group / Child" for a
// label inside a group, bare "Name" otherwise.
//
// This matters because the two Linear endpoints disagree about groups. `issue
// get` populates `parent` on a label node, but the issue LIST query returns
// `parent: null` for the very same label — only the ID survives both. So a
// grouped label cannot be identified by name from a list payload, and anything
// filtering issues by lane has to resolve to an ID first. That is what this
// file exists to do.
func LabelPath(l api.Label) string {
	if l.Parent != nil && l.Parent.Name != "" {
		return l.Parent.Name + " / " + l.Name
	}
	return l.Name
}

// looksLikeUUID reports whether ref has the shape of a Linear object ID.
// Same test as the --project flag uses, kept deliberately identical so the two
// flags behave the same way for a caller holding an ID.
func looksLikeUUID(ref string) bool {
	return len(ref) == 36 && strings.Count(ref, "-") == 4
}

// ResolveLabelRef turns a user-supplied label reference into a label ID.
//
// Accepted forms, all case-insensitive and whitespace-tolerant:
//
//	"Queue / Now"                            group path
//	"Now"                                    bare name, when unambiguous
//	"5dc5053c-7c7c-459f-9006-8d08906e2aa3"   ID
//
// A reference that matches nothing, or that matches more than one label, is an
// error. Returning "no match" as an empty filter would be worse than useless
// here: an unknown label and a genuinely empty lane would produce byte-identical
// output, so a typo'd lane name would read as "nothing to do" forever.
func ResolveLabelRef(labels []api.Label, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("empty label reference")
	}

	if looksLikeUUID(ref) {
		for _, l := range labels {
			if strings.EqualFold(l.ID, ref) {
				return l.ID, nil
			}
		}
		return "", fmt.Errorf("no label with ID %q in this team", ref)
	}

	// A "/" means the caller spelled out the group. Match on both halves so
	// "Queue / Now" cannot accidentally select a top-level label called "Now".
	if group, child, ok := splitLabelPath(ref); ok {
		for _, l := range labels {
			if l.Parent == nil {
				continue
			}
			if strings.EqualFold(l.Parent.Name, group) && strings.EqualFold(l.Name, child) {
				return l.ID, nil
			}
		}
		return "", fmt.Errorf("no label %q in this team (looked for child %q of group %q)", ref, child, group)
	}

	var matches []api.Label
	for _, l := range labels {
		if strings.EqualFold(l.Name, ref) {
			matches = append(matches, l)
		}
	}

	switch len(matches) {
	case 1:
		return matches[0].ID, nil
	case 0:
		return "", fmt.Errorf("no label %q in this team; run 'linctl label list --team <key>' to see the available labels", ref)
	default:
		paths := make([]string, 0, len(matches))
		for _, m := range matches {
			paths = append(paths, fmt.Sprintf("%q", LabelPath(m)))
		}
		sort.Strings(paths)
		return "", fmt.Errorf("label %q is ambiguous in this team — it matches %s; qualify it with its group, e.g. %s",
			ref, strings.Join(paths, ", "), paths[0])
	}
}

// splitLabelPath splits "Group / Child" into its halves. Reports false when ref
// carries no separator, or when either half is blank ("/ Now", "Queue /"), which
// is a malformed reference rather than a bare name.
func splitLabelPath(ref string) (group, child string, ok bool) {
	idx := strings.Index(ref, "/")
	if idx < 0 {
		return "", "", false
	}
	group = strings.TrimSpace(ref[:idx])
	child = strings.TrimSpace(ref[idx+1:])
	if group == "" || child == "" {
		return "", "", false
	}
	return group, child, true
}

// resolveLabelFlag turns the --label flag into a label ID, fetching the team's
// labels to do it. Returns "" when the flag was not supplied.
//
// Resolution is a separate round-trip rather than a nested name filter on the
// issue query on purpose: it is what makes an unknown label an error instead of
// an empty result set, and it is the only way to address a grouped label, whose
// group is absent from issue list payloads.
func resolveLabelFlag(ctx context.Context, client *api.Client, labelRef, teamKey string) (string, error) {
	labelRef = strings.TrimSpace(labelRef)
	if labelRef == "" {
		return "", nil
	}
	if teamKey == "" {
		return "", fmt.Errorf("--label requires --team, because label names are scoped to a team")
	}

	labels, err := client.GetTeamLabels(ctx, teamKey)
	if err != nil {
		return "", fmt.Errorf("could not read labels for team %s: %w", teamKey, err)
	}

	return ResolveLabelRef(labels, labelRef)
}
