package target

import (
	"sort"
	"strings"
)

// WorkEntry is one waiting item in a work signature: what the item is, and the
// state it was seen in. Key stays the same for as long as the item exists (a
// ticket's number, a merge request's project and iid); Version moves whenever
// something happens on it (the newest note id, the updated_at stamp).
type WorkEntry struct {
	Key     string
	Version string
}

// The shape of a signature that a host can read item by item: one
// "<key>@<version>" entry per waiting item, sorted, joined by commas. It is the
// shape the signing plugins grew into on their own — GitLab's
// "issue12!4@881", Jira's "OPS-7@2026-10-01T09:00:00.000+0000" — written down
// so that a host may rely on it.
//
// What reads the items: covey's workflows start one case per waiting item and
// hand a change on an item to the case that is already running for it. A
// plugin gets that by signing its work in this shape, through WorkSignature,
// and saying so once (ItemSigner). A signature in any other shape still works
// as a watermark, and a host then treats the whole of it as one piece of work.

// ItemSigner says that a work checker's signature is written with
// WorkSignature, one entry per waiting item. It has to be said rather than
// guessed: a signature hashed down to one entry ("3@a1b2c3…", the count and a
// digest) has the same shape as one waiting item, and a host that read it as
// one would make a case of the digest.
//
// A method rather than a bare marker so that a plugin whose behaviour lives in
// data (a manifest) can answer per definition. Use SignsItems to ask.
type ItemSigner interface {
	SignedWorkChecker
	SignsWorkItems() bool
}

// SignsItems answers whether sys signs its work item by item.
func SignsItems(sys System) bool {
	s, ok := sys.(ItemSigner)
	return ok && s.SignsWorkItems()
}

// WorkSignature writes entries as a signature: sorted, so that the order the
// target system returned the items in does not move it. No entries give "".
//
// A key must not contain a comma; a version must contain neither a comma nor
// an "@". Timestamps and ids, which is what versions are, never do.
func WorkSignature(entries []WorkEntry) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = e.Key + "@" + e.Version
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// WorkEntries reads a signature written in the shape above. It reports false
// for a signature that is not a list of items — an entry without a key or
// without an "@", or a key that appears twice — so that a host falls back to
// treating the signature as one piece of work instead of guessing at items.
// The empty signature is the empty list.
//
// The version is what follows the LAST "@", so a key may itself contain one
// (a mailbox address, say).
func WorkEntries(sig string) ([]WorkEntry, bool) {
	if sig == "" {
		return nil, true
	}
	parts := strings.Split(sig, ",")
	out := make([]WorkEntry, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		at := strings.LastIndex(p, "@")
		if at <= 0 {
			return nil, false
		}
		key := p[:at]
		if seen[key] {
			return nil, false
		}
		seen[key] = true
		out = append(out, WorkEntry{Key: key, Version: p[at+1:]})
	}
	return out, true
}
