package memory

import (
	"errors"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/model"
)

// link builds one Link; sup == "" means live (no successor).
func link(id, project, sup string) Link {
	var s *model.ID
	if sup != "" {
		v := model.ID(sup)
		s = &v
	}
	return Link{ID: model.ID(id), Project: model.ProjectKey(project), SupersededBy: s}
}

// TestValidateSupersessionAccept verifies the shapes that are valid. Each one
// guards a specific over-strict rule: a validator that forbade "target is also
// superseded" would reject the linear chain, and one that forbade a shared head
// would reject the diamond.
func TestValidateSupersessionAccept(t *testing.T) {
	tests := []struct {
		name  string
		links []Link
	}{
		{"empty set", nil},
		{"all live", []Link{
			link("a", "p1", ""),
			link("b", "p1", ""),
		}},
		{"single link", []Link{
			link("a", "p1", "b"),
			link("b", "p1", ""),
		}},
		{"linear chain", []Link{
			link("a", "p1", "b"),
			link("b", "p1", "c"),
			link("c", "p1", ""),
		}},
		{"shared head is allowed", []Link{
			link("a", "p1", "c"),
			link("b", "p1", "c"),
			link("c", "p1", ""),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateSupersession(tt.links); err != nil {
				t.Errorf("ValidateSupersession(%v) = %v, want nil", tt.links, err)
			}
		})
	}
}

// TestValidateSupersessionReject pins the four refusals, each reachable only
// through its own rule: self, dangling target, cross-project link, and cycle.
// The error must wrap model.ErrInvalid and name the offending id.
func TestValidateSupersessionReject(t *testing.T) {
	tests := []struct {
		name    string
		links   []Link
		wantSub string
	}{
		{
			name: "self supersession",
			links: []Link{
				link("a", "p1", "a"),
			},
			wantSub: "a",
		},
		{
			name: "dangling target",
			links: []Link{
				link("a", "p1", "b"),
			},
			wantSub: "b",
		},
		{
			name: "cross project",
			links: []Link{
				link("a", "p1", "b"),
				link("b", "p2", ""),
			},
			wantSub: "a",
		},
		{
			name: "two node cycle",
			links: []Link{
				link("a", "p1", "b"),
				link("b", "p1", "a"),
			},
			wantSub: "a",
		},
		{
			name: "three node cycle",
			links: []Link{
				link("a", "p1", "b"),
				link("b", "p1", "c"),
				link("c", "p1", "a"),
			},
			wantSub: "a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSupersession(tt.links)
			if err == nil {
				t.Fatalf("ValidateSupersession(%v) = nil, want an error", tt.links)
			}
			if !errors.Is(err, model.ErrInvalid) {
				t.Errorf("error %q does not wrap model.ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q does not name %q", err, tt.wantSub)
			}
		})
	}
}
