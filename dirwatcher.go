package wbgo

import (
	"gopkg.in/fsnotify.v1"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

type dirWatcherOpType int

const (
	RELOAD_DELAY                = 1000 * time.Millisecond
	DIRWATCHER_OP_LIST_CAPACITY = 128
	DIRWATCHER_OP_CHANGE        = dirWatcherOpType(iota)
	DIRWATCHER_OP_REMOVE
)

type DirWatcherClient interface {
	LoadFile(path string) error
	LiveLoadFile(path string) error
	LiveRemoveFile(path string) error
}

type dirWatcherFile struct {
	explicit bool
}

type dirWatcherOp struct {
	typ  dirWatcherOpType
	path string
}

type DirWatcher struct {
	initMtx   sync.Mutex
	rx        *regexp.Regexp
	client    DirWatcherClient
	watcher   *fsnotify.Watcher
	started   bool
	quit      chan struct{}
	delay     time.Duration
	loaded    map[string]*dirWatcherFile
	opsByPath map[string]*dirWatcherOp
	opList    []*dirWatcherOp
	timer     *time.Timer
	c         <-chan time.Time
}

func NewDirWatcher(pattern string, client DirWatcherClient) *DirWatcher {
	if rx, err := regexp.Compile(pattern); err != nil {
		log.Panicf("invalid loader regexp: %s", pattern)
		return nil
	} else {
		return &DirWatcher{
			rx:      rx,
			client:  client,
			watcher: nil,
			started: false,
			quit:    make(chan struct{}),
			delay:   RELOAD_DELAY,
			loaded:  make(map[string]*dirWatcherFile),
		}
	}
}

func (dw *DirWatcher) SetDelay(delay time.Duration) {
	dw.delay = delay
}

func (dw *DirWatcher) resetOps() {
	dw.opsByPath = make(map[string]*dirWatcherOp)
	dw.opList = make([]*dirWatcherOp, 0, DIRWATCHER_OP_LIST_CAPACITY)
}

func (dw *DirWatcher) registerFSEvent(ev fsnotify.Event) {
	opType := DIRWATCHER_OP_CHANGE
	if ev.Op == fsnotify.Remove || ev.Op == fsnotify.Rename {
		opType = DIRWATCHER_OP_REMOVE
	}
	op, found := dw.opsByPath[ev.Name]
	if found {
		op.typ = opType
	} else {
		op = &dirWatcherOp{opType, ev.Name}
		dw.opsByPath[ev.Name] = op
		dw.opList = append(dw.opList, op)
	}

	if dw.timer != nil {
		dw.timer.Reset(dw.delay)
	} else {
		dw.timer = time.NewTimer(dw.delay)
		dw.c = dw.timer.C
	}
}

func (dw *DirWatcher) processEvents() {
	for _, op := range dw.opList {
		switch op.typ {
		case DIRWATCHER_OP_CHANGE:
			Debug.Printf("(re)load: %s", op.path)
			// need to check whether the file that possibly doesn't
			// satisfy the loader pattern was explicitly loaded
			explicit := false
			if entry, found := dw.loaded[op.path]; found {
				explicit = entry.explicit
			}
			if err := dw.doLoad(op.path, explicit, true); err != nil {
				Warn.Printf(
					"warning: failed to load %s: %s", op.path, err)
			}
		case DIRWATCHER_OP_REMOVE:
			Debug.Printf("file removed: %s", op.path)
			dw.removePath(op.path)
		default:
			log.Panicf("invalid loader op %d", op.typ)
		}
	}
	dw.resetOps()
}

func (dw *DirWatcher) startWatching() {
	dw.initMtx.Lock()
	defer dw.initMtx.Unlock()
	if dw.started {
		return
	}

	var err error
	if dw.watcher, err = fsnotify.NewWatcher(); err != nil {
		Warn.Printf("failed to create filesystem watcher: %s", err)
		return
	}

	dw.started = true
	dw.resetOps()
	go func() {
		for {
			select {
			case ev := <-dw.watcher.Events:
				Debug.Printf("fs change event: %s", ev)
				dw.registerFSEvent(ev)
			case err := <-dw.watcher.Errors:
				Error.Printf("watcher error: %s", err)
			case <-dw.c:
				Debug.Printf("reload timer fired")
				dw.processEvents()
			case <-dw.quit:
				return
			}
		}
	}()
}

func (dw *DirWatcher) shouldLoadFile(fileName string) bool {
	return dw.rx.MatchString(fileName)
}

func (dw *DirWatcher) loadDir(filePath string, reloaded bool) error {
	Debug.Printf("loadDir: %s", filePath)
	entries, err := ioutil.ReadDir(filePath)
	if err != nil {
		return err
	}
	if err = dw.watcher.Add(filePath); err != nil {
		Debug.Printf("loadDir: failed to watch %s: %s", filePath, err)
	}
	for _, fi := range entries {
		fullPath := filepath.Join(filePath, fi.Name())
		Debug.Printf("loadDir: entry: %s", fullPath)
		var err error
		switch {
		case fi.IsDir():
			err = dw.loadDir(fullPath, reloaded)
		case dw.shouldLoadFile(fi.Name()):
			err = dw.loadFile(fullPath, reloaded, false)
		}
		if err != nil {
			Warn.Printf("couldn't load %s: %s", fullPath, err)
		}
	}
	return nil
}

func (dw *DirWatcher) loadFile(filePath string, reloaded, explicit bool) error {
	dw.loaded[filePath] = &dirWatcherFile{explicit}
	if reloaded {
		Info.Printf("reloading file: %s", filePath)
		return dw.client.LiveLoadFile(filePath)
	} else {
		return dw.client.LoadFile(filePath)
	}
}

func (dw *DirWatcher) doLoad(filePath string, explicit bool, reloaded bool) error {
	dw.startWatching()
	fi, err := os.Stat(filePath)
	switch {
	case err != nil:
		return err
	case fi.IsDir():
		return dw.loadDir(filePath, reloaded)
	case explicit:
		dw.watcher.Add(filePath)
		fallthrough
	case dw.shouldLoadFile(fi.Name()):
		return dw.loadFile(filePath, reloaded, explicit)
	default:
		Debug.Printf("skipping loading of non-matching file %s", filePath)
		return nil
	}
}

func (dw *DirWatcher) removePath(filePath string) {
	if dw.loaded[filePath] != nil {
		dw.client.LiveRemoveFile(filePath)
		delete(dw.loaded, filePath)
		return
	}
	// assume it's directory removal and try to find subpaths
	pathList := make([]string, 0)
	for p, _ := range dw.loaded {
		if IsSubpath(filePath, p) {
			pathList = append(pathList, p)
		}
	}
	sort.Strings(pathList)
	for _, p := range pathList {
		dw.removePath(p)
	}
}

func (dw *DirWatcher) Load(filePath string) error {
	return dw.doLoad(filePath, true, false)
}

func (dw *DirWatcher) Stop() {
	dw.initMtx.Lock()
	defer dw.initMtx.Unlock()
	if !dw.started {
		return
	}
	dw.quit <- struct{}{}
	dw.watcher.Close()
	dw.started = false
}
