// Package hosts manages clone-tree's ownership-tagged entries in a hosts
// file (normally /etc/hosts): one "127.0.0.1 <dns>  # clone-tree:<name>"
// line per worktree with a DNS pattern configured (see
// internal/config.Config.DNSPattern). The trailing "# clone-tree:<name>"
// comment is the sole marker of ownership — every other line in the file
// (system entries, unrelated tools) is preserved verbatim and in place.
package hosts

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DefaultPath is the hosts file clone-tree manages on a real machine.
// Callers pass an explicit path in tests.
const DefaultPath = "/etc/hosts"

// commentPrefix marks a line as owned by clone-tree; the text after it is
// the worktree name.
const commentPrefix = "# clone-tree:"

// Entry is one clone-tree-owned hosts file line.
type Entry struct {
	Name string
	DNS  string
}

// Add writes "127.0.0.1 <dns>  # clone-tree:<name>" into hostsFile,
// replacing any existing line owned by name in place. Failing that, it
// takes ownership of an existing *unmanaged* line already bound to the same
// dns (no "# clone-tree:" marker at all — e.g. a hand-added entry, or a
// leftover from before this worktree was ct-managed), replacing it in place
// rather than appending a second, now-duplicate, binding for dns. Only when
// neither is found is the line appended. All other lines and their order
// are preserved.
func Add(hostsFile, name, dns string) error {
	lines, err := readLines(hostsFile)
	if err != nil {
		return err
	}

	m := marker(name)
	line := entryLine(name, dns)
	for i, l := range lines {
		if strings.HasSuffix(l, m) {
			lines[i] = line
			return writeLines(hostsFile, lines)
		}
	}
	for i, l := range lines {
		if !strings.Contains(l, commentPrefix) && lineHasDNS(l, dns) {
			lines[i] = line
			return writeLines(hostsFile, lines)
		}
	}
	return writeLines(hostsFile, append(lines, line))
}

// Remove drops every line owned by name from hostsFile. A no-op (no error)
// if name has no entry.
func Remove(hostsFile, name string) error {
	lines, err := readLines(hostsFile)
	if err != nil {
		return err
	}

	m := marker(name)
	kept := lines[:0]
	for _, l := range lines {
		if !strings.HasSuffix(l, m) {
			kept = append(kept, l)
		}
	}
	return writeLines(hostsFile, kept)
}

// Has reports whether hostsFile has a line owned by name.
func Has(hostsFile, name string) (bool, error) {
	lines, err := readLines(hostsFile)
	if err != nil {
		return false, err
	}

	m := marker(name)
	for _, l := range lines {
		if strings.HasSuffix(l, m) {
			return true, nil
		}
	}
	return false, nil
}

// HasDNS reports whether hostsFile has ANY line — clone-tree-owned or not —
// binding dns as a hostname. Used to tell an "unmanaged" entry (dns already
// resolves, but not via a "# clone-tree:<name>" marker) apart from a truly
// absent one; `ct hosts` surfaces the distinction, and Add uses it to take
// ownership of an unmanaged line instead of duplicating it.
func HasDNS(hostsFile, dns string) (bool, error) {
	lines, err := readLines(hostsFile)
	if err != nil {
		return false, err
	}
	for _, l := range lines {
		if lineHasDNS(l, dns) {
			return true, nil
		}
	}
	return false, nil
}

// lineHasDNS reports whether line's host-fields (everything after the IP,
// before any trailing "#" comment) include dns — a hosts line can list
// multiple hostnames after the IP, e.g. "127.0.0.1 a b c".
func lineHasDNS(line, dns string) bool {
	hostPart, _, _ := strings.Cut(line, "#")
	fields := strings.Fields(hostPart)
	if len(fields) < 2 {
		return false
	}
	for _, f := range fields[1:] {
		if f == dns {
			return true
		}
	}
	return false
}

// List returns every clone-tree-owned entry in hostsFile, in file order.
func List(hostsFile string) ([]Entry, error) {
	lines, err := readLines(hostsFile)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, l := range lines {
		idx := strings.Index(l, commentPrefix)
		if idx < 0 {
			continue
		}
		name := l[idx+len(commentPrefix):]
		hostPart, _, _ := strings.Cut(l, "#")
		fields := strings.Fields(hostPart)
		if len(fields) < 2 {
			continue
		}
		entries = append(entries, Entry{Name: name, DNS: fields[1]})
	}
	return entries, nil
}

// marker is the full ownership comment for name, matched as a line suffix
// so e.g. name "foo" never matches an entry owned by "foobar".
func marker(name string) string {
	return commentPrefix + name
}

func entryLine(name, dns string) string {
	return fmt.Sprintf("127.0.0.1 %s  %s", dns, marker(name))
}

// readLines returns hostsFile's lines with any trailing newline stripped.
// A missing file is not an error — it yields no lines, and the next write
// creates it.
func readLines(hostsFile string) ([]string, error) {
	data, err := os.ReadFile(hostsFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("hosts: read %s: %w", hostsFile, err)
	}

	content := strings.TrimRight(string(data), "\n")
	if content == "" {
		return nil, nil
	}
	return strings.Split(content, "\n"), nil
}

// writeLines joins lines with a trailing newline (or writes an empty file
// for zero lines) and persists them via writeFile.
func writeLines(hostsFile string, lines []string) error {
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	return writeFile(hostsFile, content)
}

// writeFile writes content to path. /etc/hosts is root-owned on most
// machines: a direct write from an unprivileged process fails with
// os.ErrPermission, in which case writeFile falls back to piping content
// through `sudo tee <path>` (stdout discarded, stderr inherited so a sudo
// password prompt is visible).
func writeFile(path, content string) error {
	err := os.WriteFile(path, []byte(content), 0o644)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("hosts: write %s: %w", path, err)
	}

	cmd := exec.Command("sudo", "tee", path)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hosts: write %s via sudo: %w", path, err)
	}
	return nil
}
