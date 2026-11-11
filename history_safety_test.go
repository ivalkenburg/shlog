package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryWritesRefuseBackupSymlinkAliases(t *testing.T) {
	for _, layout := range []string{"direct", "chain", "directory alias", "case alias", "Unicode alias"} {
		for _, operation := range []string{"replace", "restore"} {
			t.Run(layout+"/"+operation, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "history")
				if layout == "Unicode alias" {
					path = filepath.Join(dir, "café")
				}
				backup := path + ".bak"
				original := "first\nsecond\n"
				if layout == "chain" {
					if err := os.WriteFile(filepath.Join(dir, "real"), []byte(original), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("real", backup); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(backup, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
				link := "history.bak"
				if layout == "Unicode alias" {
					link = "cafe\u0301.bak"
					if _, err := os.Stat(filepath.Join(dir, link)); os.IsNotExist(err) {
						t.Skip("filesystem distinguishes Unicode normalization aliases")
					}
				}
				if layout == "case alias" {
					link = "HISTORY.BAK"
					if _, err := os.Stat(filepath.Join(dir, link)); os.IsNotExist(err) {
						t.Skip("filesystem distinguishes case aliases")
					}
				}
				if err := os.Symlink(link, path); err != nil {
					t.Fatal(err)
				}
				if layout == "directory alias" {
					alias := filepath.Join(dir, "linked")
					if err := os.Symlink(dir, alias); err != nil {
						t.Fatal(err)
					}
					path = filepath.Join(alias, "history")
				}
				before, err := os.Lstat(backup)
				if err != nil {
					t.Fatal(err)
				}
				if operation == "replace" {
					err = ReplaceHistoryFile(path, []byte(original), parseHistoryData([]byte("first\n")))
				} else {
					err = RestoreBackup(path)
				}
				if err == nil || !strings.Contains(err.Error(), "passes through its backup") {
					t.Fatalf("unsafe %s was not refused: %v", operation, err)
				}
				assertFileContent(t, path, original)
				assertFileContent(t, backup, original)
				after, err := os.Lstat(backup)
				if err != nil || !os.SameFile(before, after) {
					t.Fatal("refused write replaced the backup directory entry")
				}
				if got, err := os.Readlink(path); err != nil || got != link {
					t.Fatal("refused write replaced the history symlink")
				}
			})
		}
	}
}

func TestHistoryBackupHardlinksRemainSafe(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "symlink"}[symlink], func(t *testing.T) {
			dir := t.TempDir()
			path, target := filepath.Join(dir, "history"), filepath.Join(dir, "history")
			if symlink {
				target = filepath.Join(dir, "real")
				if err := os.Symlink("real", path); err != nil {
					t.Fatal(err)
				}
			}
			original := "first\nsecond\n"
			if err := os.WriteFile(target, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(target, path+".bak"); err != nil {
				t.Fatal(err)
			}
			if err := ReplaceHistoryFile(path, []byte(original), parseHistoryData([]byte("first\n"))); err != nil {
				t.Fatalf("distinct hardlink entries were incorrectly refused: %v", err)
			}
			assertFileContent(t, path, "first\n")
			assertFileContent(t, path+".bak", original)
		})
	}
}

func TestFormatDetectionIgnoresContinuedMarkers(t *testing.T) {
	for _, marker := range []string{"- cmd: two", "#1700000000", ": 1000:0;two"} {
		t.Run(marker, func(t *testing.T) {
			command := "printf '%s' \\\n" + marker
			original := command + "\npwd\n"
			entries := parseHistoryData([]byte(original))
			if len(entries) != 2 || CommandText(entries[0]) != command || CommandText(entries[1]) != "pwd" {
				t.Fatalf("continuation payload was treated as a history format marker: %+v", entries)
			}
			var output strings.Builder
			if err := writeEntries(&output, entries); err != nil || output.String() != original {
				t.Fatal("continuation format detection changed raw history")
			}
			hist := writeTempHistory(t, original)
			if out, err := runGoshist(t, "--histfile", hist, "-f", "del", "1"); err != nil {
				t.Fatalf("scoped deletion failed: %v\n%s", err, out)
			}
			assertFileContent(t, hist, "pwd\n")
			assertFileContent(t, hist+".bak", original)
		})
	}
}

func TestTimestampedPreambleIgnoresContinuedMarkers(t *testing.T) {
	command := "printf '%s' \\\n#1700000000"
	original := command + "\npwd\n#1700000001\nreal\n"
	entries := parseHistoryData([]byte(original))
	if len(entries) != 3 || CommandText(entries[0]) != command || CommandText(entries[1]) != "pwd" || CommandText(entries[2]) != "real" || entries[2].Timestamp != 1700000001 {
		t.Fatal("timestamped history split a continued preamble at a fake marker")
	}
	var output strings.Builder
	if err := writeEntries(&output, entries); err != nil || output.String() != original {
		t.Fatal("timestamped preamble roundtrip changed raw history")
	}
}
