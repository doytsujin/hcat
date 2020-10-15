package tfunc

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/hcat"
	"github.com/hashicorp/hcat/dep"
	idep "github.com/hashicorp/hcat/internal/dependency"
	"github.com/pkg/errors"
)

////////// Template Function

// fileFunc returns or accumulates file dependencies.
func fileFunc(r hcat.Recaller, used, missing *hcat.DepSet, sandboxPath string) func(string) (string, error) {
	return func(s string) (string, error) {
		if len(s) == 0 {
			return "", nil
		}
		err := pathInSandbox(sandboxPath, s)
		if err != nil {
			return "", err
		}
		d, err := idep.NewFileQuery(s)
		if err != nil {
			return "", err
		}

		used.Add(d)

		if value, ok := r.Recall(d.String()); ok {
			if value == nil {
				return "", nil
			}
			return value.(string), nil
		}

		missing.Add(d)

		return "", nil
	}
}

// pathInSandbox returns an error if the provided path doesn't fall within the
// sandbox or if the file can't be evaluated (missing, invalid symlink, etc.)
func pathInSandbox(sandbox, path string) error {
	if sandbox != "" {
		s, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		s, err = filepath.Rel(sandbox, s)
		if err != nil {
			return err
		}
		if strings.HasPrefix(s, "..") {
			return fmt.Errorf("'%s' is outside of sandbox", path)
		}
	}
	return nil
}

////////// Dependency Interface
// Template function registers this with watcher to monitor the FileQuery
// and trigger events

var (
	// Ensure implements
	_ dep.Dependency = (*FileQuery)(nil)

	// PollInterval is the amount of time to sleep between queries, since
	// the fsnotify library is not compatible with solaris and other OSes yet.
	PollInterval = 2 * time.Second
)

// FileQuery represents a local file dependency.
type FileQuery struct {
	stopCh chan struct{}

	path string
	stat os.FileInfo
}

// NewFileQuery creates a file dependency from the given path.
func NewFileQuery(s string) (*FileQuery, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("file: invalid format: %q", s)
	}

	return &FileQuery{
		stopCh: make(chan struct{}, 1),
		path:   s,
	}, nil
}

// Fetch retrieves this dependency and returns the result or any errors that
// occur in the process.
func (d *FileQuery) Fetch(clients dep.Clients) (interface{}, *dep.ResponseMetadata, error) {

	select {
	case <-d.stopCh:
		return "", nil, dep.ErrStopped
	case r := <-d.watch(d.stat):
		if r.err != nil {
			return "", nil, errors.Wrap(r.err, d.String())
		}

		data, err := ioutil.ReadFile(d.path)
		if err != nil {
			return "", nil, errors.Wrap(err, d.String())
		}

		d.stat = r.stat

		return string(data), &dep.ResponseMetadata{
			LastContact: 0,
			LastIndex:   uint64(time.Now().Unix()),
		}, nil
	}
}

// Stop halts the dependency's fetch function.
func (d *FileQuery) Stop() {
	close(d.stopCh)
}

// String returns the human-friendly version of this dependency.
func (d *FileQuery) String() string {
	return fmt.Sprintf("file(%s)", d.path)
}

type watchResult struct {
	stat os.FileInfo
	err  error
}

// watch watchers the file for changes
func (d *FileQuery) watch(lastStat os.FileInfo) <-chan *watchResult {
	ch := make(chan *watchResult, 1)

	go func(lastStat os.FileInfo) {
		for {
			stat, err := os.Stat(d.path)
			if err != nil {
				select {
				case <-d.stopCh:
					return
				case ch <- &watchResult{err: err}:
					return
				}
			}

			changed := lastStat == nil ||
				lastStat.Size() != stat.Size() ||
				lastStat.ModTime() != stat.ModTime()

			if changed {
				select {
				case <-d.stopCh:
					return
				case ch <- &watchResult{stat: stat}:
					return
				}
			}

			time.Sleep(PollInterval)
		}
	}(lastStat)

	return ch
}
