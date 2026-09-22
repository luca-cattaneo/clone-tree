// Package hosts manages clone-tree's ownership-tagged entries in a hosts
// file: the trailing "# clone-tree:<name>" comment is the sole marker of
// ownership — every other line is preserved verbatim and in place.
package hosts

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const DefaultPath = "/etc/hosts"

// commentPrefix marks a line as owned by clone-tree; the text after it is
// the worktree name.
const commentPrefix = "# clone-tree:"

type Entry struct {
	Name string
	DNS  string
}

// Add replaces any existing line owned by name in place. Failing that, it
// takes ownership of an existing unmanaged line already bound to the same
// dns, replacing it in place rather than appending a duplicate binding.
// Only when neither is found is the line appended.
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
// binding dns as a hostname.
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

func marker(name string) string {
	return commentPrefix + name
}

func entryLine(name, dns string) string {
	return fmt.Sprintf("127.0.0.1 %s  %s", dns, marker(name))
}

// A missing file is not an error — it yields no lines.
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

func writeLines(hostsFile string, lines []string) error {
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	return writeFile(hostsFile, content)
}

// /etc/hosts is root-owned on most machines: a direct write from an
// unprivileged process fails with os.ErrPermission, in which case writeFile
// falls back to piping content through `sudo tee <path>` (stdout discarded,
// stderr inherited so a sudo password prompt is visible).
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
