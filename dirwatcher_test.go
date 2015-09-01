package wbgo

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type DirWatcherSuite struct {
	Suite
	*Recorder
	cleanup            []func()
	dir1, dir2, subdir string
	f1, f5, f6         string
	dw                 *DirWatcher
}

func (s *DirWatcherSuite) T() *testing.T {
	return s.Suite.T()
}

func (s *DirWatcherSuite) loadFile(filePath, typ string) error {
	if bs, err := ioutil.ReadFile(filePath); err != nil {
		return err
	} else {
		s.Rec("%s: %s", typ, string(bs))
	}
	return nil
}

func (s *DirWatcherSuite) LoadFile(filePath string) error {
	return s.loadFile(filePath, "L")
}

func (s *DirWatcherSuite) LiveLoadFile(filePath string) error {
	return s.loadFile(filePath, "R")
}

func (s *DirWatcherSuite) LiveRemoveFile(filePath string) error {
	recPath := filepath.Base(filePath)
	if IsSubpath(s.subdir, filePath) {
		recPath = "subdir/" + recPath
	}
	s.Rec("D: %s", recPath)
	return nil
}

func (s *DirWatcherSuite) SetupTest() {
	s.Suite.SetupTest()
	s.cleanup = make([]func(), 2)
	s.dir1, s.cleanup[0] = SetupTempDir(s.T())
	s.dir2, s.cleanup[1] = SetupTempDir(s.T())

	s.subdir = filepath.Join(s.dir1, "subdir")
	s.Ck("Mkdir()", os.Mkdir(s.subdir, 0777))

	s.Recorder = NewRecorder(s.T())
	s.dw = NewDirWatcher("\\.js$", s)

	// make tests a bit quicker
	s.dw.SetDelay(100 * time.Millisecond)
	s.SetEmptyWaitTime(200 * time.Millisecond)

	s.f1 = s.writeFile(s.dir1, "f1.js", "// f1")
	s.writeFile(s.dir1, "f2.js", "// f2")
	s.writeFile(s.dir1, "f3.js.noload", "// f3 (not loaded)")
	s.writeFile(s.subdir, "f4.js", "// f4")

	s.f5 = s.writeFile(s.dir2, "f5.js", "// f5")
	s.f6 = s.writeFile(s.dir2, "f6.js", "// f6")
}

func (s *DirWatcherSuite) writeFile(dir, filename, content string) string {
	fullPath := filepath.Join(dir, filename)
	s.Ck("WriteFile()", ioutil.WriteFile(fullPath, []byte(content), 0777))
	return fullPath
}

func (s *DirWatcherSuite) TearDownTest() {
	s.VerifyEmpty()
	s.dw.Stop()
	for _, f := range s.cleanup {
		f()
	}
	s.Suite.TearDownTest()
}

func (s *DirWatcherSuite) loadDir1() {
	s.dw.Load(s.dir1)
	s.Verify("L: // f1", "L: // f2", "L: // f4")
}

func (s *DirWatcherSuite) loadAll() {
	s.loadDir1()

	s.dw.Load(s.f5)
	s.Verify("L: // f5")

	// direct path specification will load even non-matching files
	s.dw.Load(s.f6)
	s.Verify("L: // f6")

	s.VerifyEmpty()
}

func (s *DirWatcherSuite) TestPlainLoading() {
	s.loadAll()
}

func (s *DirWatcherSuite) TestAddingNewFile() {
	s.loadDir1()

	s.writeFile(s.dir1, "f2_1.js", "// f2_1")
	s.Verify("R: // f2_1")

	s.writeFile(s.subdir, "f4_1.js", "// f4_1")
	s.Verify("R: // f4_1")

	// add a non-matching file
	s.writeFile(s.dir1, "whatever.txt", "noload")
	s.VerifyEmpty()

	// make sure the new files are watched properly
	s.writeFile(s.dir1, "f2_1.js", "// f2_1 (changed)")
	s.writeFile(s.subdir, "f4_1.js", "// f4_1 (changed)")
	s.Verify("R: // f2_1 (changed)", "R: // f4_1 (changed)")
}

func (s *DirWatcherSuite) TestModification() {
	s.loadAll()

	s.writeFile(s.dir1, "f1.js", "// f1 (changed)")
	s.Verify("R: // f1 (changed)")

	s.writeFile(s.dir2, "f5.js", "// f5 (changed)")
	s.Verify("R: // f5 (changed)")
}

func (s *DirWatcherSuite) TestRenaming() {
	s.loadAll()

	os.Rename(s.f1, filepath.Join(s.dir1, "f1_renamed.js"))
	s.Verify(
		"D: f1.js",
		"R: // f1")

	// make sure the file is still watched after rename
	s.writeFile(s.dir1, "f1_renamed.js", "// f1_renamed (changed)")
	s.Verify(
		"R: // f1_renamed (changed)")

	// when an explicitly specified file is renamed, it's no longer watched
	os.Rename(s.f5, filepath.Join(s.dir2, "f5_renamed.js"))
	s.writeFile(s.dir2, "f5_renamed.js", "// f5_renamed (changed)")
	s.Verify("D: f5.js")

	// FIXME: should track directories of explicitly specified files
	// to see when they reappear

	newSubdir := filepath.Join(s.dir1, "subdir_renamed")
	os.Rename(s.subdir, newSubdir)
	s.Verify(
		"D: subdir/f4.js",
		"R: // f4")

	// make sure the directory is still watched after rename
	s.writeFile(newSubdir, "f4.js", "// f4 (changed)")
	s.Verify("R: // f4 (changed)")
}

func (s *DirWatcherSuite) TestFileRemoval() {
	s.loadAll()

	s.writeFile(s.dir1, "f1.js", "// f1 (should be ignored)")
	// make it likely that change event is not swallowed
	// due to the following deletion
	time.Sleep(50 * time.Millisecond)
	os.RemoveAll(s.subdir)
	os.Remove(s.f1)
	os.Remove(s.f5)
	s.VerifyUnordered(
		"D: subdir/f4.js",
		"D: f1.js",
		"D: f5.js")
}

func (s *DirWatcherSuite) TestUnreadableFiles() {
	s.Ck("Symlink()", os.Symlink(filepath.Join(s.dir1, "blabla.js"), filepath.Join(s.dir1, "test.js")))
	s.loadAll()
	s.EnsureGotWarnings()

	s.Ck("Symlink()", os.Symlink(filepath.Join(s.dir1, "blabla1.js"), filepath.Join(s.dir1, "test1.js")))

	// must have s.VerifyEmpty() here so the warnings have time to appear
	s.VerifyEmpty()
	s.EnsureGotWarnings()
}

func (s *DirWatcherSuite) TestStoppingDirWatcher() {
	s.loadAll()
	s.dw.Stop()

	s.writeFile(s.dir1, "f2_1.js", "// f2_1")
	s.writeFile(s.dir2, "f5.js", "// f5 (changed)")
	s.VerifyEmpty()
}

func TestDirWatcherSuite(t *testing.T) {
	RunSuites(t, new(DirWatcherSuite))
}
