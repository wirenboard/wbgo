package wbgo

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type DataFileFixture struct {
	*Fixture
	wd              string
	dataFileTempDir string
	rmDataFileDir   func()
}

func NewDataFileFixture(t *testing.T) (f *DataFileFixture) {
	f = &DataFileFixture{Fixture: NewFixture(t)}
	var err error
	f.wd, err = os.Getwd()
	f.Ckf("Getwd", err)
	f.dataFileTempDir, f.rmDataFileDir = SetupTempDir(f.T())
	return
}

func (f *DataFileFixture) TearDownDataFiles() {
	f.rmDataFileDir()
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

func (f *DataFileFixture) CopyDataFileToTempDir(sourceName, targetName string) (targetPath string) {
	data := f.readSourceDataFile(sourceName)
	targetPath = f.ensureTargetDirs(targetName)
	f.Ckf("WriteFile", ioutil.WriteFile(targetPath, data, 0777))
	return
}

func (f *DataFileFixture) CopyDataFilesToTempDir(names ...string) {
	for _, name := range names {
		f.CopyDataFileToTempDir(name, name)
	}
}

func (f *DataFileFixture) WriteDataFile(filename, content string) string {
	targetPath := f.ensureTargetDirs(filename)
	f.Ckf("WriteFile()", ioutil.WriteFile(targetPath, []byte(content), 0777))
	return targetPath
}
