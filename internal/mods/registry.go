package mods

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Registry holds the in-memory state of which mods are active in
// the current process. It is the bridge between the on-disk
// mod directories and the prompt builder (which consumes the
// active mod set as `[]llm.ModDirective`).
//
// The registry is process-global. Concurrent reads after the
// startup phase are safe; concurrent Activate/Deactivate are
// serialized through a mutex. The lock is held only during
// in-memory updates — file I/O happens outside the lock.
//
// Persistence (mod_loads table) is handled by Store; the
// registry calls Store.RecordLoad() when Activate/Deactivate
// succeeds.
type Registry struct {
	mu     sync.RWMutex
	active map[string]*Loaded // keyed by Manifest.Meta.ID
	// store is the optional persistence sink. nil = in-memory only
	// (tests).
	store *Store
	// sessionID is stamped on every mod_loads row. Empty if the
	// caller did not provide one.
	sessionID string
	// constitutionID is stamped on every mod_loads row. Empty
	// means "no constitution in effect" (the LLM is not being
	// used for a judge at the moment of load).
	constitutionID string
}

// NewRegistry builds an empty registry. Pass nil for store if you
// do not want mod_loads rows written (typical for tests).
func NewRegistry(store *Store) *Registry {
	return &Registry{
		active: map[string]*Loaded{},
		store:  store,
	}
}

// SetSessionID records the session id used for subsequent
// mod_loads audit rows. Useful for grouping loads that happened
// in the same process lifetime.
func (r *Registry) SetSessionID(id string) {
	r.mu.Lock()
	r.sessionID = id
	r.mu.Unlock()
}

// SetConstitutionID records the constitution id used for
// subsequent mod_loads audit rows. Lets the user answer "which
// constitution were these mods loaded under?".
func (r *Registry) SetConstitutionID(id string) {
	r.mu.Lock()
	r.constitutionID = id
	r.mu.Unlock()
}

// Discover walks the configured mod search paths and returns the
// mod IDs it found (without loading them). The namespace prefix
// is the basename of the search path (e.g. mods under
// ~/.dark-research/mods are prefixed "user/"). Used by tooling
// that wants to list available mods without activating.
func (r *Registry) Discover() ([]string, error) {
	paths, err := defaultSearchPaths()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, root := range paths {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("mods: read %s: %w", root, err)
		}
		ns := namespaceForPath(root)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			id := ns + "/" + e.Name()
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// namespaceForPath returns the namespace prefix to use for mods
// discovered under the given search path. The convention is
// "user" for the user's home mods dir, and the basename of
// the path for any other path. The user/home case is special-
// cased so that the typical "~/.dark-research/mods/foo" install
// is always reported as "user/foo", not "mods/foo".
func namespaceForPath(root string) string {
	home, _ := os.UserHomeDir()
	if home != "" {
		// Match both ~/.dark-research/mods and any path under
		// ~/.dark-research (so users with custom layouts still
		// get the "user/" namespace).
		if root == filepath.Join(home, ".dark-research", "mods") ||
			strings.HasPrefix(root, filepath.Join(home, ".dark-research")+string(filepath.Separator)) {
			return "user"
		}
	}
	// Generic fallback: use the basename of the path.
	return filepath.Base(root)
}

// Activate loads a mod by its mod_id and marks it active. The
// mod's mod_id is the manifest's `meta.id` (e.g.
// "user/osint-cve-deepdive"). The on-disk layout is the
// conventional <user-mods-dir>/<short-name>/ directory.
//
// Returns the loaded mod or an error. Idempotent: re-activating
// a mod reloads from disk and replaces the in-memory copy.
func (r *Registry) Activate(modID string) (*Loaded, error) {
	modRoot, err := r.resolveModPath(modID)
	if err != nil {
		return nil, err
	}

	start := nowMillis()
	loaded, err := Load(modRoot)
	if err != nil {
		// Record the failure for audit.
		r.recordLoad(modID, "", 0, 0, err)
		return nil, err
	}
	dur := nowMillis() - start

	r.mu.Lock()
	r.active[modID] = loaded
	r.mu.Unlock()

	caps := len(loaded.Knowledge) + len(loaded.Directives)
	r.recordLoad(modID, loaded.SHA256, int(dur), caps, nil)
	return loaded, nil
}

// Deactivate removes a mod from the active set. Returns
// ErrNotActive if the mod is not currently active.
func (r *Registry) Deactivate(modID string) error {
	r.mu.Lock()
	_, ok := r.active[modID]
	if !ok {
		r.mu.Unlock()
		return ErrNotActive
	}
	delete(r.active, modID)
	r.mu.Unlock()
	return nil
}

// IsActive reports whether the given mod_id is currently in the
// active set.
func (r *Registry) IsActive(modID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.active[modID]
	return ok
}

// List returns the active mods in stable order (by mod_id). The
// returned slice is a snapshot; mutations to the registry after
// List returns do not affect the slice.
func (r *Registry) List() []*Loaded {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Loaded, 0, len(r.active))
	for _, m := range r.active {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Manifest.Meta.ID < out[j].Manifest.Meta.ID
	})
	return out
}

// Get returns the loaded mod for modID, or nil if not active.
func (r *Registry) Get(modID string) *Loaded {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active[modID]
}

// AsModDirectives flattens the active mods into the
// `[]llm.ModDirective` shape that the prompt builder consumes.
// Order is the same as List() (sorted by mod_id). Each
// knowledge file becomes one ModDirective with Source =
// "knowledge"; each directive file becomes one with Source =
// "directive".
//
// Note: this lives in `internal/mods` and references
// `llm.ModDirective`. The reverse dependency — `llm` importing
// `mods` — is intentionally NOT used. The prompt builder
// signature is `BuildSystemPrompt(PromptContext)` where
// `PromptContext.ActiveMods` is `[]llm.ModDirective`. The ssd
// handler in `internal/tools` is the boundary that does the
// conversion (it already imports both packages).
func (r *Registry) AsModDirectives() []ModDirectiveOut {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ModDirectiveOut
	for _, m := range r.active {
		for _, k := range m.Knowledge {
			out = append(out, ModDirectiveOut{
				ModID:  m.Manifest.Meta.ID,
				Source: k.Kind, // "prompt_injection" or "data_source"
				Body:   k.Body,
			})
		}
		for _, d := range m.Directives {
			out = append(out, ModDirectiveOut{
				ModID:  m.Manifest.Meta.ID,
				Source: "directive",
				Body:   d.Body,
			})
		}
	}
	return out
}

// ModDirectiveOut is the in-memory shape of one mod-supplied
// fragment. The tools package converts these to llm.ModDirective
// when calling BuildSystemPrompt. Keeping a separate type
// (instead of importing llm here) preserves the
// constitution/mods/llm package boundaries.
type ModDirectiveOut struct {
	ModID  string
	Source string
	Body   string
}

// resolveModPath turns a mod_id into an on-disk path. The
// convention is <user-mods-dir>/<short-name>/ for user mods. The
// namespace prefix (the part before the slash) is informational;
// the directory layout uses just the short name.
func (r *Registry) resolveModPath(modID string) (string, error) {
	parts := strings.SplitN(modID, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("mods: invalid mod_id %q (want 'namespace/name')", modID)
	}
	shortName := parts[1]
	roots, err := defaultSearchPaths()
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		candidate := filepath.Join(root, shortName)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("mods: mod %q not found in any search path", modID)
}

// recordLoad writes a mod_loads row (if a Store is configured).
// Errors are logged to stderr but do not fail the caller —
// persistence is best-effort and should not break activation.
func (r *Registry) recordLoad(modID, manifestSHA string, durMs, capabilities int, loadErr error) {
	if r.store == nil {
		return
	}
	r.mu.RLock()
	sid := r.sessionID
	cid := r.constitutionID
	r.mu.RUnlock()

	errStr := ""
	if loadErr != nil {
		errStr = loadErr.Error()
	}
	if err := r.store.RecordLoad(r.store.ctx, modID, sid, durMs, capabilities, errStr, cid); err != nil {
		fmt.Fprintf(os.Stderr, "mods: failed to record mod_load: %v\n", err)
	}
}

// ErrNotActive is returned by Deactivate when the mod is not in
// the active set.
var ErrNotActive = errors.New("mods: not active")

// DefaultSearchPaths returns the directories the loader scans
// for mod roots. Precedence order (first match wins):
//  1. $DARK_MODS_PATH (colon-separated, like PATH)
//  2. $DARK_RESEARCH_HOME/mods
//  3. ~/.dark-research/mods
//
// Exported so main.go (or any other bootstrapper) can resolve
// a mod_id to an on-disk path using the same search rules as
// the registry.
func DefaultSearchPaths() ([]string, error) {
	return defaultSearchPaths()
}

func defaultSearchPaths() ([]string, error) {
	var paths []string
	if v := os.Getenv("DARK_MODS_PATH"); v != "" {
		for _, p := range strings.Split(v, string(filepath.ListSeparator)) {
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	if v := os.Getenv("DARK_RESEARCH_HOME"); v != "" {
		paths = append(paths, filepath.Join(v, "mods"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".dark-research", "mods"))
	}
	return paths, nil
}
