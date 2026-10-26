package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
)

// Folder servers (F10): the settings file says which folders should be
// open; the reconciler makes the running servers match, every few seconds
// and right after the phone changes a setting. So `folders serve` on the
// PC and PUT /v1/folders/{id}/server from the phone both just edit the
// settings.

const reconcileEvery = 5 * time.Second

type reconciler struct {
	dir    string
	srv    core.Server
	logger *slog.Logger
	kick   chan struct{}
	done   chan struct{}
	errs   map[string]string // last error per folder, logged once
}

func newReconciler(dir string, srv core.Server, logger *slog.Logger) *reconciler {
	return &reconciler{dir: dir, srv: srv, logger: logger, kick: make(chan struct{}, 1), done: make(chan struct{}), errs: map[string]string{}}
}

// trigger asks for a pass now.
func (r *reconciler) trigger() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

func (r *reconciler) run(ctx context.Context) {
	defer close(r.done)
	t := time.NewTicker(reconcileEvery)
	defer t.Stop()
	r.once()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.kick:
		case <-t.C:
		}
		r.once()
	}
}

func pathKey(p string) string {
	k := filepath.Clean(p)
	if runtime.GOOS == "windows" {
		k = strings.ToLower(k)
	}
	return k
}

func (r *reconciler) once() {
	cfg, err := config.Load(r.dir)
	if err != nil {
		return // keep what runs; a broken file is reported elsewhere
	}
	want := map[string]config.Folder{}
	for _, f := range cfg.Folders {
		if f.Server.Enabled {
			want[pathKey(f.Path)] = f
		}
	}
	for _, p := range r.srv.ServerFolders() {
		if _, ok := want[pathKey(p)]; !ok {
			_ = r.srv.StopServer(p)
			r.logger.Info("folder server stopped", "folder", p)
		}
	}
	for k, f := range want {
		err := r.srv.StartServer(f.Path, core.ServeOptions{Spawn: f.Server.Spawn, Capacity: f.Server.Capacity})
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		if msg != r.errs[k] {
			if msg != "" {
				r.logger.Warn("folder server not started", "folder", f.Path, "err", msg)
			}
			r.errs[k] = msg
		}
	}
}

// setFolderServer saves one folder's server setting.
func setFolderServer(dir string) func(id string, fs config.FolderServer) (config.Folder, error) {
	return func(id string, fs config.FolderServer) (config.Folder, error) {
		cfg, err := config.Load(dir)
		if err != nil {
			return config.Folder{}, err
		}
		for i := range cfg.Folders {
			if cfg.Folders[i].ID == id {
				cfg.Folders[i].Server = fs
				if err := config.Save(dir, cfg); err != nil {
					return config.Folder{}, err
				}
				return cfg.Folders[i], nil
			}
		}
		return config.Folder{}, core.Fail(core.ErrCodeFolderNotFound, "")
	}
}
