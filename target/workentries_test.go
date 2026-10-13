package target

import (
	"reflect"
	"testing"
)

// The signature is the heartbeat's watermark, so writing it must not depend on
// the order the target system listed the items in.
func TestWorkSignatureIsSortedAndReadsBack(t *testing.T) {
	entries := []WorkEntry{{"mr3!9", "120"}, {"issue3!4", "118"}}
	sig := WorkSignature(entries)
	if sig != "issue3!4@118,mr3!9@120" {
		t.Fatalf("signature = %q", sig)
	}
	if again := WorkSignature([]WorkEntry{entries[1], entries[0]}); again != sig {
		t.Fatalf("order moved the signature: %q vs %q", again, sig)
	}
	got, ok := WorkEntries(sig)
	want := []WorkEntry{{"issue3!4", "118"}, {"mr3!9", "120"}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("read back %v, %v; want %v", got, ok, want)
	}
}

func TestNoWorkIsTheEmptySignature(t *testing.T) {
	if sig := WorkSignature(nil); sig != "" {
		t.Fatalf("signature of nothing = %q", sig)
	}
	if got, ok := WorkEntries(""); !ok || len(got) != 0 {
		t.Fatalf("empty signature read as %v, %v", got, ok)
	}
}

// Signatures the plugins already write, read item by item.
func TestWorkEntriesReadsWhatThePluginsAlreadyWrite(t *testing.T) {
	for _, c := range []struct {
		sig  string
		keys []string
	}{
		{"issue12!4@881,mr12!9@903", []string{"issue12!4", "mr12!9"}},
		{"pr:acme/app#3@77+0123456789ab", []string{"pr:acme/app#3"}},
		{"OPS-7@2026-10-01T09:00:00.000+0000", []string{"OPS-7"}},
		{"ada@example.org@1712", []string{"ada@example.org"}},
	} {
		got, ok := WorkEntries(c.sig)
		if !ok {
			t.Errorf("%q: not read as items", c.sig)
			continue
		}
		var keys []string
		for _, e := range got {
			keys = append(keys, e.Key)
		}
		if !reflect.DeepEqual(keys, c.keys) {
			t.Errorf("%q: keys %v, want %v", c.sig, keys, c.keys)
		}
	}
}

// A signature that is not a list of items is not taken apart: a host treats it
// as one piece of work.
func TestWorkEntriesRefusesWhatIsNotAList(t *testing.T) {
	for _, sig := range []string{
		"3 open",             // no version
		"@881",               // no key
		"issue1@2,issue1@3",  // one item twice
		"issue1@2,,issue3@4", // an empty entry
	} {
		if got, ok := WorkEntries(sig); ok {
			t.Errorf("%q read as items: %v", sig, got)
		}
	}
}
