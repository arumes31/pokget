package db

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var migrationFilenamePattern = regexp.MustCompile(`^(\d{6})_(.+)\.(up|down)\.sql$`)

type migrationFiles struct {
	up   bool
	down bool
}

func TestMigrationRecoveryPolicy(t *testing.T) {
	t.Parallel()

	migrationsDirectory := filepath.Join("..", "..", "migrations")
	entries, err := os.ReadDir(migrationsDirectory)
	if err != nil {
		t.Fatalf("read migrations directory: %v", err)
	}

	filesByStem := make(map[string]migrationFiles, len(entries))
	versions := make(map[int]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		matches := migrationFilenamePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			t.Fatalf("migration has unsupported filename %q", entry.Name())
		}
		version, parseErr := strconv.Atoi(matches[1])
		if parseErr != nil {
			t.Fatalf("parse migration version %q: %v", matches[1], parseErr)
		}
		versions[version] = struct{}{}
		stem := matches[1] + "_" + matches[2]
		files := filesByStem[stem]
		if matches[3] == "up" {
			if files.up {
				t.Fatalf("migration %q has duplicate up files", stem)
			}
			files.up = true
		} else {
			if files.down {
				t.Fatalf("migration %q has duplicate down files", stem)
			}
			files.down = true
		}
		filesByStem[stem] = files
	}

	assertContiguousMigrationVersions(t, versions)
	irreversible := loadIrreversibleMigrations(t, filepath.Join(migrationsDirectory, "irreversible.txt"))
	for stem, files := range filesByStem {
		if !files.up {
			t.Errorf("migration %q has a down file but no up file", stem)
			continue
		}
		_, declaredIrreversible := irreversible[stem]
		switch {
		case files.down && declaredIrreversible:
			t.Errorf("migration %q has a down file and must not be declared irreversible", stem)
		case !files.down && !declaredIrreversible:
			t.Errorf("migration %q needs a down file or an irreversible declaration", stem)
		}
		delete(irreversible, stem)
	}
	for stem := range irreversible {
		t.Errorf("irreversible declaration %q has no matching up migration", stem)
	}
}

func TestSchemaReconciliationMigration(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "migrations", "000028_reconcile_legacy_schema.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema reconciliation migration: %v", err)
	}

	sql := strings.ToLower(string(contents))
	for _, required := range []string{
		"alter table cards add column if not exists rarity text",
		"create table if not exists price_history",
		"create table if not exists price_alerts",
		"create index if not exists idx_price_alerts_user_id",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("schema reconciliation migration does not contain %q", required)
		}
	}
}

func assertContiguousMigrationVersions(t *testing.T, versions map[int]struct{}) {
	t.Helper()

	ordered := make([]int, 0, len(versions))
	for version := range versions {
		ordered = append(ordered, version)
	}
	slices.Sort(ordered)
	for index, version := range ordered {
		expected := index + 1
		if version != expected {
			t.Fatalf("migration version %d is missing before version %d", expected, version)
		}
	}
}

func loadIrreversibleMigrations(t *testing.T, path string) map[string]struct{} {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open irreversible migration manifest: %v", err)
	}
	defer file.Close()

	entries := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		value := strings.TrimSpace(scanner.Text())
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		if migrationFilenamePattern.MatchString(value) || !regexp.MustCompile(`^\d{6}_.+$`).MatchString(value) {
			t.Fatalf("invalid irreversible migration entry on line %d: %q", line, value)
		}
		if _, duplicate := entries[value]; duplicate {
			t.Fatalf("duplicate irreversible migration entry %q", value)
		}
		entries[value] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read irreversible migration manifest: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("irreversible migration manifest is empty")
	}
	return entries
}
