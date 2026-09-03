package main

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"net/url"
	"strings"

	memorytext "github.com/tedkulp/drops/internal/memory"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// keyEncoding matches internal/model: unpadded Base32, lowercased, over 16
// bytes. A derived key has to satisfy model.ParseProjectKey exactly, so the
// encoding is not a choice here.
var keyEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// projectKeyDomain separates this derivation from any other use of the same
// hash. It is part of the wire format: changing it changes every key, and two
// machines that disagree on it cannot merge.
const projectKeyDomain = "drops.project:"

// ProjectKeyFor derives a Project key from a slug, so that two machines
// converting their own store independently agree on the identity of a Project
// they both hold.
//
// internal/store already does this for the reserved slugs, and says why:
// "the reserved slugs always take their fixed keys, so independently converted
// stores agree on them". Random keys are correct for a Project minted once, on
// one machine, and wrong for the cutover, which runs once per machine against
// stores that were the same store. The key stays opaque and immutable; it stops
// being unpredictable, which it never needed to be.
//
// Reserved slugs are deliberately absent: projectKeys.forSlug pins them before
// it consults the plan, and shadowing them here would put a second spelling of
// model.GlobalProjectKey in the tree.
func ProjectKeyFor(slug string) (model.ProjectKey, error) {
	if slug == model.GlobalProjectSlug || slug == model.InboxProjectSlug {
		return "", fmt.Errorf("slug %q is reserved and takes its fixed key", slug)
	}
	if slug == "" {
		return "", fmt.Errorf("empty Project slug")
	}
	sum := sha256.Sum256([]byte(projectKeyDomain + slug))
	raw := strings.ToLower(keyEncoding.EncodeToString(sum[:16]))
	return model.ParseProjectKey(raw)
}

// ProjectKeys derives the whole slug-to-key table for one conversion.
func ProjectKeys(slugs []string) (map[string]model.ProjectKey, error) {
	keys := map[string]model.ProjectKey{}
	for _, slug := range slugs {
		if slug == model.GlobalProjectSlug || slug == model.InboxProjectSlug || slug == "" {
			continue
		}
		key, err := ProjectKeyFor(slug)
		if err != nil {
			return nil, err
		}
		keys[slug] = key
	}
	return keys, nil
}

// NormalizeLocator turns a legacy remote_url into the repository locator v7
// stores: host and path, with no scheme, credentials, port or .git suffix, so
// the same repository reached over SSH and HTTPS is one locator.
//
// An empty result means "no locator", which the conversion reports as a skipped
// remote rather than guessing at.
func NormalizeLocator(remote string) string {
	trimmed := strings.TrimSpace(remote)
	if trimmed == "" {
		return ""
	}
	// scp-like syntax (git@host:owner/repo) is not a URL and url.Parse reads
	// it as an opaque path, so it is rewritten before parsing.
	if !strings.Contains(trimmed, "://") {
		if at := strings.LastIndex(trimmed, "@"); at >= 0 {
			trimmed = trimmed[at+1:]
		}
		trimmed = strings.Replace(trimmed, ":", "/", 1)
		return cleanLocator(trimmed)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	if host == "" {
		return ""
	}
	return cleanLocator(host + "/" + strings.TrimPrefix(parsed.Path, "/"))
}

func cleanLocator(raw string) string {
	cleaned := strings.Trim(raw, "/")
	cleaned = strings.TrimSuffix(cleaned, ".git")
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "" || !strings.Contains(cleaned, "/") {
		return ""
	}
	host, path, _ := strings.Cut(cleaned, "/")
	// A machine-local remote is never a locator (dw32p.21). A filesystem path
	// survives every step above — "/home/ted/src/drops" cleans to
	// "home/ted/src/drops", which is shaped exactly like host/owner/repo — so
	// the host segment has to be required to look like a host.
	if !strings.Contains(host, ".") {
		return ""
	}
	return strings.ToLower(host) + "/" + path
}

// Locators builds the plan's slug-to-locator table from the legacy remote_urls.
func Locators(remotes map[string]string) map[string]string {
	locators := map[string]string{}
	for slug, remote := range remotes {
		if locator := NormalizeLocator(remote); locator != "" {
			locators[slug] = locator
		}
	}
	return locators
}

// Curate is the plan's memory judgement: dw32p.11's curation for the 149
// memories it measured, and default-retain for anything it never saw.
//
// Default-retain matters because the second machine's store has moved on since
// that measurement. An unmeasured memory is kept under its legacy scope, which
// is the rule dw32p.11 applied to the rows it could not verify: "absence of
// evidence is not evidence of obsolescence".
func Curate(legacy store.LegacyMemory) (store.MemoryPlan, bool) {
	if _, dropped := droppedMemories[legacy.ID]; dropped {
		return store.MemoryPlan{}, false
	}
	// A memory the curation never saw and the legacy store had deleted stays
	// deleted. v7 omission is not a tombstone, so the alternative is not
	// "migrate the deletion" but "resurrect the row as live", which is worse
	// than dropping it. None of the 149 measured rows is deleted, so this only
	// reaches the second machine's later writes.
	if _, measured := retainedMemories[legacy.ID]; !measured && legacy.Deleted {
		return store.MemoryPlan{}, false
	}

	slug := legacy.ProjectSlug
	if owner, known := retainedMemories[legacy.ID]; known {
		slug = owner
	}
	if slug == "" {
		slug = model.GlobalProjectSlug
	}

	body := memorytext.MigrateBody(legacy.Body, legacy.Kind)
	curated := store.MemoryPlan{
		ProjectSlug: slug,
		Title:       memorytext.DeriveTitle(body),
		Body:        body,
		Provenance:  memorytext.NormalizeProvenance(legacy.Source),
	}
	if next, ok := supersession(legacy); ok {
		curated.SupersededBy = &next
	}
	return curated, true
}

// supersession decides which supersession link survives.
//
// For a memory dw32p.11 measured, only the eight edges it named are written:
// "no other legacy supersession edge is written", because every other legacy
// link pointed at a row the curation drops. For an unmeasured memory the legacy
// link is kept, but only when its target also survives — a link into a dropped
// row is what makes the conversion refuse, and it is not the unmeasured
// memory's fault.
func supersession(legacy store.LegacyMemory) (model.ID, bool) {
	if next, ok := retainedSupersessions[legacy.ID]; ok {
		return next, true
	}
	if _, measured := retainedMemories[legacy.ID]; measured {
		return "", false
	}
	if legacy.SupersededBy == nil {
		return "", false
	}
	target := *legacy.SupersededBy
	if _, dropped := droppedMemories[target]; dropped {
		return "", false
	}
	return target, true
}
