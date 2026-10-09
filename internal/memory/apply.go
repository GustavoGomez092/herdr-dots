package memory

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"herdr-dots/internal/backend"
)

// ApplyResult counts what Store.Apply did. Rejected holds one reason per
// refused op and never the op's text (it may hold a secret).
type ApplyResult struct {
	Added, Updated, Forgotten int
	Rejected                  []string
}

func (r ApplyResult) Applied() int { return r.Added + r.Updated + r.Forgotten }

// Apply validates and applies a dot's model-written memory ops (spec §6.5):
//   - text that looks like a secret, is empty or is too long is refused;
//   - add: dot scope lands active; project scope lands proposed (active with
//     AutoApproveProject); only convention, decision, finding and note; never
//     dismissed, preference or global;
//   - update/forget: only the dot's own model-written dot entries and its own
//     still-proposed project entries.
//
// The error is only for store failures (lock timeout, newer file version);
// per-op problems go to Rejected.
func (s *Store) Apply(p, dot string, ops []backend.MemoryOp, ev Evidence) (ApplyResult, error) {
	var res ApplyResult
	if len(ops) > backend.MaxPassOps {
		ops = ops[:backend.MaxPassOps]
	}
	for i, op := range ops {
		err := s.applyOne(p, dot, op, ev, &res)
		switch {
		case err == nil:
		case errors.Is(err, ErrLocked), errors.Is(err, ErrVersion):
			return res, err
		default:
			res.Rejected = append(res.Rejected, fmt.Sprintf("op %d (%s): %v", i+1, op.Op, err))
		}
	}
	return res, nil
}

func (s *Store) applyOne(p, dot string, op backend.MemoryOp, ev Evidence, res *ApplyResult) error {
	text := strings.TrimSpace(op.Text)
	if op.Op == "add" || op.Op == "update" {
		if found, kind := ScanSecrets(text); found {
			return fmt.Errorf("%w (%s)", ErrSecret, kind)
		}
	}
	switch op.Op {
	case "add":
		sc := Scope(op.Scope)
		if sc == "" {
			sc = ScopeDot
		}
		if sc != ScopeDot && sc != ScopeProject {
			return fmt.Errorf("%w: models can't write %q memory", ErrDenied, op.Scope)
		}
		k := Kind(op.Kind)
		if k == "" {
			k = KindNote
		}
		if !slices.Contains(ModelKinds, k) {
			return fmt.Errorf("%w: models can't write %q entries", ErrDenied, op.Kind)
		}
		e := Entry{Scope: sc, Dot: dot, Kind: k, Text: text, Paths: op.Paths, Status: StatusActive, Source: SourceModel, Evidence: ev}
		if sc == ScopeProject && !s.Limits.AutoApproveProject {
			e.Status = StatusProposed
		}
		var r insertResult
		err := s.mutate(p, sc, dotFor(sc, dot), func(f *scopeFile) error {
			var err error
			_, r, err = s.insert(f, e)
			return err
		})
		if err != nil {
			return err
		}
		if r == merged {
			res.Updated++
		} else {
			res.Added++
		}
		return nil
	case "update", "forget":
		ref, err := ParseRef(op.ID, dot)
		if err != nil {
			return err
		}
		if ref.Scope == ScopeGlobal {
			return fmt.Errorf("%w: global memory is written by the developer only", ErrDenied)
		}
		err = s.mutate(p, ref.Scope, ref.Dot, func(f *scopeFile) error {
			i := f.find(ref.ID)
			if i < 0 {
				return fmt.Errorf("%w: %s", ErrNotFound, ref)
			}
			x := f.Entries[i]
			if x.Source != SourceModel || x.Dot != dot || (x.Scope == ScopeProject && x.Status != StatusProposed) {
				return fmt.Errorf("%w: %s", ErrDenied, ref)
			}
			if op.Op == "forget" {
				f.Entries = slices.Delete(f.Entries, i, i+1)
				return nil
			}
			x.Text = text
			if len(op.Paths) > 0 {
				x.Paths = op.Paths
			}
			x, err := s.prepare(x)
			if err != nil {
				return err
			}
			x.UpdatedAt = s.now()
			f.Entries[i] = x
			return nil
		})
		if err != nil {
			return err
		}
		if op.Op == "forget" {
			res.Forgotten++
		} else {
			res.Updated++
		}
		return nil
	}
	return fmt.Errorf("%w: unknown op %q", ErrInvalid, op.Op)
}
