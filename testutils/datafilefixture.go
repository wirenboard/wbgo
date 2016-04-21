package testutils

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var oldRmDataDir func()

type DataFileFixture struct {
	*Fixture
	wd              string
	dataFileTempDir string
	rmDataFileDir   func()
}

func NewDataFileFixture(t *testing.T) (f *DataFileFixture) {
	if oldRmDataDir != nil {
		// don't use wbgo.Warn here to avoid unneeded test failures
		log.Printf("SetupTempDir(): WARNING: using auto cleanup for previous NewDataFileFixture() " +
			"[perhaps due to an unfinished fixture setup]")
		oldRmDataDir()
	}
	f = &DataFileFixture{Fixture: NewFixture(t)}
	var err error
	f.wd, err = os.Getwd()
	f.Ckf("Getwd", err)
	f.dataFileTempDir, f.rmDataFileDir = SetupTempDir(f.T())
	oldRmDataDir = f.rmDataFileDir
	return
}

func (f *DataFileFixture) TearDownDataFiles() {
	oldRmDataDir = nil
	f.rmDataFileDir()
	err := os.Chdir(f.wd)
	f.Ckf("Chdir", err)
}

func (f *DataFileFixture) SourceDir() string {
	return f.wd
}

func (f *DataFileFixture) DataFileTempDir() string {
	return f.dataFileTempDir
}

func (f *DataFileFixture) DataFilePath(dataFile string) string {
	return filepath.Join(f.dataFileTempDir, dataFile)
}

func (f *DataFileFixture) ensureTargetDirs(targetName string) (targetPath string) {
	targetPath = f.DataFilePath(targetName)
	if strings.Contains(targetName, "/") {
		// the target file is under a subdir
		f.Ckf("MkdirAll", os.MkdirAll(filepath.Dir(targetPath), 0777))
	}
	return
}

func (f *DataFileFixture) readSourceDataFile(sourceName string) []byte {
	data, err := ioutil.ReadFile(filepath.Join(f.wd, sourceName))
	f.Ckf("ReadFile()", err)
	return data
}

func (f *DataFileFixture) ReadSourceDataFile(sourceName string) string {
	return string(f.readSourceDataFile(sourceName))
}

func (f *DataFileFixture) CopyModifiedDataFileToTempDir(sourceName, targetName string, edit func(string) string) (targetPath string) {
	data := string(f.readSourceDataFile(sourceName))
	targetPath = f.ensureTargetDirs(targetName)
	f.Ckf("WriteFile", ioutil.WriteFile(targetPath, []byte(edit(data)), 0777))
	return
}

func (f *DataFileFixture) CopyDataFileToTempDir(sourceName, targetName string) (targetPath string) {
	data := f.readSourceDataFile(sourceName)
	targetPath = f.ensureTargetDirs(targetName)
	f.Ckf("WriteFile", ioutil.WriteFile(targetPath, data, 0777))
	return
}

func (f *DataFileFixture) CopyDataFilesToTempDir(names ...string) {
	for _, name := range names {
		srcName, dstName := name, name
		if idx := strings.Index(name, ":"); idx >= 0 {
			srcName = name[:idx]
			dstName = name[idx+1:]
		}
		f.CopyDataFileToTempDir(srcName, dstName)
	}
}

func (f *DataFileFixture) WriteDataFile(filename, content string) string {
	targetPath := f.ensureTargetDirs(filename)
	f.Ckf("WriteFile()", ioutil.WriteFile(targetPath, []byte(content), 0777))
	return targetPath
}

func (f *DataFileFixture) RmFile(filename string) {
	f.Ckf("Remove()", os.Remove(f.DataFilePath(filename)))
}
