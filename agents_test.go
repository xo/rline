package rline

// These tests read the repository rather than the package. They hold the
// setup that every xo repository shares, which D36 adopts from dbmeta D110
// and dbmeta D111: the agent skills, the import in CLAUDE.md, the four
// documents in the root, and the decision log in docs/PLAN.md.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestSkillsAreCopies makes sure that each skill in skills-lock.json is an
// ordinary folder under .agents/skills and under .claude/skills, and that the
// two folders hold the same files.
//
// A symbolic link fails the test. A Windows checkout writes a symbolic link as
// a text file that holds the target path. Claude Code then finds a file where
// it expects a folder, and it loads no skill and reports nothing. The two
// links that 28149af committed had exactly that fault.
func TestSkillsAreCopies(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile("skills-lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Skills map[string]json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(body, &lock); err != nil {
		t.Fatalf("reading skills-lock.json: %v", err)
	}
	if len(lock.Skills) == 0 {
		t.Fatal("skills-lock.json names no skill")
	}
	roots := []string{filepath.Join(".agents", "skills"), filepath.Join(".claude", "skills")}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if _, ok := lock.Skills[e.Name()]; !ok {
				t.Errorf("%s is not in skills-lock.json, so nobody can install it again. "+
					"Add it with the command in CONTRIBUTING.md", filepath.Join(root, e.Name()))
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(lock.Skills)) {
		agents := skillFiles(t, filepath.Join(roots[0], name))
		claude := skillFiles(t, filepath.Join(roots[1], name))
		if agents == nil || claude == nil {
			continue
		}
		for _, path := range slices.Sorted(maps.Keys(agents)) {
			switch other, ok := claude[path]; {
			case !ok:
				t.Errorf("%s: %s is in %s and not in %s", name, path, roots[0], roots[1])
			case other != agents[path]:
				t.Errorf("%s: %s differs between %s and %s", name, path, roots[0], roots[1])
			}
		}
		for _, path := range slices.Sorted(maps.Keys(claude)) {
			if _, ok := agents[path]; !ok {
				t.Errorf("%s: %s is in %s and not in %s", name, path, roots[1], roots[0])
			}
		}
	}
}

// skillFiles returns the content of each file in one copy of a skill, keyed by
// its path in the copy. It reports a copy that is missing or that is a
// symbolic link, and then returns nil.
func skillFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	fi, err := os.Lstat(dir)
	switch {
	case err != nil:
		t.Errorf("%s is missing. Install the skill with the command in CONTRIBUTING.md", dir)
		return nil
	case !fi.IsDir():
		t.Errorf("%s is not a folder. A Windows checkout writes a symbolic link as a text file, "+
			"so install the skill with --copy. See CONTRIBUTING.md", dir)
		return nil
	}
	out := map[string]string{}
	err = fs.WalkDir(os.DirFS(dir), ".", func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.Type()&fs.ModeSymlink != 0:
			t.Errorf("%s is a symbolic link. Install the skill with --copy", filepath.Join(dir, path))
		case d.Type().IsRegular():
			body, err := fs.ReadFile(os.DirFS(dir), path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}
			out[path] = string(body)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	return out
}

// TestClaudeImportsAgents makes sure that CLAUDE.md is an ordinary file that
// holds one line, which imports AGENTS.md. Claude Code reads CLAUDE.md, and
// Codex and the other agents read AGENTS.md, so the import makes every agent
// read the same rules. A rule written in CLAUDE.md would reach Claude Code
// alone. A symbolic link fails for the reason in TestSkillsAreCopies.
func TestClaudeImportsAgents(t *testing.T) {
	t.Parallel()
	fi, err := os.Lstat("CLAUDE.md")
	switch {
	case err != nil:
		t.Fatal(err)
	case !fi.Mode().IsRegular():
		t.Fatalf("CLAUDE.md is not an ordinary file, its mode is %v. "+
			"Make it a file that holds @AGENTS.md. See D36", fi.Mode())
	}
	if s := readFile(t, "CLAUDE.md"); s != "@AGENTS.md\n" {
		t.Errorf("CLAUDE.md holds %q. It holds only the line @AGENTS.md, "+
			"and the rules go in AGENTS.md. See D36", s)
	}
	if _, err := os.Stat("AGENTS.md"); err != nil {
		t.Errorf("CLAUDE.md imports AGENTS.md, which is missing: %v", err)
	}
}

// TestTheRootHoldsFourDocuments makes sure that the root holds README.md,
// AGENTS.md, CLAUDE.md and CONTRIBUTING.md, and no other document. Every
// other document goes in docs/. A document that turns up in the root is one
// that nobody filed.
func TestTheRootHoldsFourDocuments(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"README.md": true, "AGENTS.md": true, "CLAUDE.md": true, "CONTRIBUTING.md": true,
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if !allowed[e.Name()] {
			t.Errorf("%s is in the repository root. Only README.md, AGENTS.md, CLAUDE.md "+
				"and CONTRIBUTING.md go there, and every other document goes in docs/. "+
				"See D36", e.Name())
		}
	}
	for _, name := range slices.Sorted(maps.Keys(allowed)) {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("expected %s in the repository root", name)
		}
	}
}

// planDecision is one decision in docs/PLAN.md, read from its heading.
type planDecision struct {
	num    int
	title  string
	status string
	anchor string
}

// decisionHeading matches the heading of a decision, which ends with its
// status:
//
//	### D8. The key codes are exported from their own package, key. Proposed. Amends D1.
var decisionHeading = regexp.MustCompile(`(?m)^### (D([1-9][0-9]*)\. (.+))$`)

// decisionStatus splits a heading into its title and its status. The status
// is the last "Decided" or "Proposed" and what follows it, because a title can
// hold a full stop, as in "ansi.Code.RGBA keeps its named results".
var decisionStatus = regexp.MustCompile(`^(.+)\. ((?:Decided|Proposed)(?:\. [^.]+)?)\.$`)

// planDecisions reads every decision heading in docs/PLAN.md, in order.
func planDecisions(t *testing.T) []planDecision {
	t.Helper()
	var out []planDecision
	for _, m := range decisionHeading.FindAllStringSubmatch(readFile(t, filepath.Join("docs", "PLAN.md")), -1) {
		num, _ := strconv.Atoi(m[2])
		s := decisionStatus.FindStringSubmatch(m[3])
		if s == nil {
			t.Errorf("docs/PLAN.md: the heading of D%d does not end with its status: %q. "+
				"Write it as \"### D%d. Title. Decided.\" or \"### D%d. Title. Proposed.\", "+
				"with \"Amends Dn\" or \"Amended by Dn\" after the status", num, m[1], num, num)
			continue
		}
		out = append(out, planDecision{num: num, title: s[1], status: s[2], anchor: githubAnchor(m[1])})
	}
	if len(out) == 0 {
		t.Fatal("docs/PLAN.md holds no decision, so this guards nothing")
	}
	return out
}

// githubAnchor returns the fragment that GitHub gives a heading: lower case,
// with every character removed that is not a letter, a digit, a space, a
// hyphen or an underscore, and each space made a hyphen.
func githubAnchor(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// TestTheDecisionIndexIsComplete checks the index at the top of docs/PLAN.md
// against the decision headings below it. A reader finds a decision by its
// row, so a missing row, a stale title or a stale status hides it. The row
// also has to link to the heading, and the test prints the row to write.
func TestTheDecisionIndexIsComplete(t *testing.T) {
	t.Parallel()
	plan := readFile(t, filepath.Join("docs", "PLAN.md"))
	rows := make(map[int]string)
	for _, m := range regexp.MustCompile(`(?m)^\| \[D([0-9]+)\]\(.*$`).FindAllStringSubmatch(plan, -1) {
		num, _ := strconv.Atoi(m[1])
		if _, ok := rows[num]; ok {
			t.Errorf("docs/PLAN.md has two rows for D%d in its index", num)
		}
		rows[num] = m[0]
	}
	written := make(map[int]bool)
	for i, d := range planDecisions(t) {
		if d.num != i+1 {
			t.Errorf("docs/PLAN.md: decision %d is numbered D%d. Decisions are numbered "+
				"from D1 in order, with no gap", i+1, d.num)
		}
		written[d.num] = true
		want := fmt.Sprintf("| [D%d](#%s) | %s | %s |", d.num, d.anchor, d.title, d.status)
		switch got, ok := rows[d.num]; {
		case !ok:
			t.Errorf("D%d has no row in the index of docs/PLAN.md. Add:\n%s", d.num, want)
		case got != want:
			t.Errorf("D%d: the row in the index of docs/PLAN.md is\n%s\nand the heading says\n%s",
				d.num, got, want)
		}
	}
	for _, num := range slices.Sorted(maps.Keys(rows)) {
		if !written[num] {
			t.Errorf("the index of docs/PLAN.md has a row for D%d, and no heading holds it", num)
		}
	}
}

// TestAnAmendmentPointsBothWays makes sure that when one decision amends
// another, both headings say so. A reader who lands on the older decision
// must see that it no longer holds as written, and the heading is the first
// thing anyone reads.
func TestAnAmendmentPointsBothWays(t *testing.T) {
	t.Parallel()
	status := make(map[int]string)
	for _, d := range planDecisions(t) {
		status[d.num] = d.status
	}
	amends := regexp.MustCompile(`\bAmends D([0-9]+)`)
	amendedBy := regexp.MustCompile(`\bAmended by D([0-9]+)`)
	for _, num := range slices.Sorted(maps.Keys(status)) {
		for _, c := range []struct {
			re   *regexp.Regexp
			back string
		}{{amends, "Amended by"}, {amendedBy, "Amends"}} {
			for _, m := range c.re.FindAllStringSubmatch(status[num], -1) {
				other, _ := strconv.Atoi(m[1])
				want := fmt.Sprintf("%s D%d", c.back, num)
				switch s, ok := status[other]; {
				case !ok:
					t.Errorf("D%d says %q, and D%d is not a decision", num, m[0], other)
				case !regexp.MustCompile(`\b` + want + `\b`).MatchString(s):
					t.Errorf("D%d says %q, and the heading of D%d does not say %q. "+
						"An amendment has to be visible from both decisions", num, m[0], other, want)
				}
			}
		}
	}
}

// decisionRef matches a decision named by number, with the word before it,
// so that a reference to another repository's decision, such as dbmeta D110,
// can be told from a reference to one of ours. It also takes a U+ before it,
// which makes the number a code point.
var decisionRef = regexp.MustCompile(`(?:([A-Za-z0-9]+)\s+|(U\+))?\bD([1-9][0-9]*)\b`)

// otherRepositories are the repositories whose decisions a document here can
// name. A reference to one of those names the repository first.
var otherRepositories = map[string]bool{
	"dbmeta": true, "dbimp": true, "usql": true, "dburl": true, "tblfmt": true,
	"transit": true,
}

// TestEveryDecisionReferenceExists makes sure that a bare decision number in a
// document or in the Go code names a decision in docs/PLAN.md. A reference to
// a number that nobody wrote leads a reader to trust a rule that does not
// exist, and a decision of another repository must name that repository.
func TestEveryDecisionReferenceExists(t *testing.T) {
	t.Parallel()
	written := make(map[string]bool)
	for _, d := range planDecisions(t) {
		written[strconv.Itoa(d.num)] = true
	}
	for _, path := range repoFiles(t, ".md", ".go") {
		for _, m := range decisionRef.FindAllStringSubmatch(readFile(t, path), -1) {
			// A code point such as U+D800 is not a decision.
			if otherRepositories[m[1]] || m[2] != "" || written[m[3]] {
				continue
			}
			t.Errorf("%s: %q names D%s, which is not in docs/PLAN.md. A decision of "+
				"another repository names the repository, such as dbmeta D110", path, m[0], m[3])
		}
	}
}

// markdownLink matches a relative link to a document, and leaves an absolute
// one alone.
var markdownLink = regexp.MustCompile(`\]\((?:\./)?([^)#:]+\.md)(?:#[^)]*)?\)`)

// docMention matches a document in docs/ named in running text, which is how
// a comment in code points at one: See docs/PORT.md.
var docMention = regexp.MustCompile(`\bdocs/[A-Z][A-Z_]*\.md\b`)

// TestEveryDocumentReferenceResolves makes sure that each document that a
// link or a comment names exists. The documents moved into docs/ in D36, and
// sixteen comments in the code named the old PLAN.md. A move that breaks a
// reference fails here rather than leaving a reader at a file that is gone.
func TestEveryDocumentReferenceResolves(t *testing.T) {
	t.Parallel()
	for _, path := range repoFiles(t, ".md") {
		dir := filepath.Dir(path)
		for _, m := range markdownLink.FindAllStringSubmatch(readFile(t, path), -1) {
			if _, err := os.Stat(filepath.Join(dir, m[1])); err != nil {
				t.Errorf("%s: the link to %s does not resolve", path, m[1])
			}
		}
	}
	for _, path := range repoFiles(t, ".go", ".sh", ".ps1", ".yml") {
		for _, m := range docMention.FindAllString(readFile(t, path), -1) {
			if _, err := os.Stat(m); err != nil {
				t.Errorf("%s: names %s, which is not a file", path, m)
			}
		}
	}
}

// repoFiles returns every file in the repository with one of the given
// extensions. It skips the hidden folders other than .github, and isocline/,
// which is the C source and a separate repository.
func repoFiles(t *testing.T, exts ...string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && path != "." && (d.Name() == "isocline" ||
			strings.HasPrefix(d.Name(), ".") && d.Name() != ".github"):
			return filepath.SkipDir
		case d.IsDir():
			return nil
		}
		if slices.Contains(exts, filepath.Ext(path)) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// readFile returns the content of a file in the repository.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
