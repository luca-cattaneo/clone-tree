package hosts_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luca-cattaneo/clone-tree/internal/hosts"
)

func writeTempHosts(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(data)
}

func TestAdd_GIVEN_noExistingEntry_WHEN_added_THEN_appendedAndOthersUntouched(t *testing.T) {
	path := writeTempHosts(t, "127.0.0.1 localhost\n::1 localhost\n")

	if err := hosts.Add(path, "feature-x", "local-feature-x.dev.tagpay.fr"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got := readFile(t, path)
	want := "127.0.0.1 localhost\n::1 localhost\n127.0.0.1 local-feature-x.dev.tagpay.fr  # clone-tree:feature-x\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAdd_GIVEN_existingEntryForName_WHEN_addedAgain_THEN_replacedInPlace(t *testing.T) {
	path := writeTempHosts(t, "127.0.0.1 localhost\n127.0.0.1 old-dns.dev.tagpay.fr  # clone-tree:feature-x\n::1 localhost\n")

	if err := hosts.Add(path, "feature-x", "new-dns.dev.tagpay.fr"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got := readFile(t, path)
	want := "127.0.0.1 localhost\n127.0.0.1 new-dns.dev.tagpay.fr  # clone-tree:feature-x\n::1 localhost\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAdd_GIVEN_nameThatIsPrefixOfAnother_WHEN_added_THEN_doesNotMatchTheOtherEntry(t *testing.T) {
	path := writeTempHosts(t, "127.0.0.1 dns-a.dev.tagpay.fr  # clone-tree:foobar\n")

	if err := hosts.Add(path, "foo", "dns-b.dev.tagpay.fr"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got := readFile(t, path)
	want := "127.0.0.1 dns-a.dev.tagpay.fr  # clone-tree:foobar\n127.0.0.1 dns-b.dev.tagpay.fr  # clone-tree:foo\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRemove_GIVEN_existingEntry_WHEN_removed_THEN_droppedAndOthersUntouched(t *testing.T) {
	path := writeTempHosts(t, "127.0.0.1 localhost\n127.0.0.1 dns-a.dev.tagpay.fr  # clone-tree:feature-x\n::1 localhost\n")

	if err := hosts.Remove(path, "feature-x"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	got := readFile(t, path)
	want := "127.0.0.1 localhost\n::1 localhost\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRemove_GIVEN_absentEntry_WHEN_removed_THEN_noopWithoutError(t *testing.T) {
	path := writeTempHosts(t, "127.0.0.1 localhost\n")

	if err := hosts.Remove(path, "does-not-exist"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	got := readFile(t, path)
	if got != "127.0.0.1 localhost\n" {
		t.Fatalf("got %q, want unchanged content", got)
	}
}

func TestHas_GIVEN_presentAndAbsentNames_WHEN_checked_THEN_reportsCorrectly(t *testing.T) {
	path := writeTempHosts(t, "127.0.0.1 dns-a.dev.tagpay.fr  # clone-tree:feature-x\n")

	present, err := hosts.Has(path, "feature-x")
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if !present {
		t.Fatalf("expected feature-x to be present")
	}

	absent, err := hosts.Has(path, "feature-y")
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if absent {
		t.Fatalf("expected feature-y to be absent")
	}
}

func TestList_GIVEN_mixOfOwnedAndForeignLines_WHEN_listed_THEN_onlyOwnedEntriesReturned(t *testing.T) {
	path := writeTempHosts(t, "127.0.0.1 localhost\n"+
		"127.0.0.1 dns-a.dev.tagpay.fr  # clone-tree:feature-a\n"+
		"127.0.0.1 dns-b.dev.tagpay.fr  # clone-tree:feature-b\n"+
		"192.168.1.1 router.local\n")

	entries, err := hosts.List(path)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := []hosts.Entry{
		{Name: "feature-a", DNS: "dns-a.dev.tagpay.fr"},
		{Name: "feature-b", DNS: "dns-b.dev.tagpay.fr"},
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(entries), len(want), entries)
	}
	for i, e := range entries {
		if e != want[i] {
			t.Fatalf("entry %d: got %+v, want %+v", i, e, want[i])
		}
	}
}

func TestAdd_GIVEN_missingFile_WHEN_added_THEN_fileCreatedWithEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")

	if err := hosts.Add(path, "feature-x", "local-feature-x.dev.tagpay.fr"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got := readFile(t, path)
	want := "127.0.0.1 local-feature-x.dev.tagpay.fr  # clone-tree:feature-x\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
