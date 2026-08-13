package goserver

import (
	"net/http"
	"sort"
)

type HookFunc func(s *Server)

type hookEntry struct {
	fn       HookFunc
	priority int
}

type BeforeRequestFunc func(w http.ResponseWriter, r *http.Request)
type AfterRequestFunc func(w http.ResponseWriter, r *http.Request)
type OnErrorFunc func(w http.ResponseWriter, r *http.Request, err error)
type OnShutdownFunc func()

var hooks []hookEntry

var (
	beforeHooks   []BeforeRequestFunc
	afterHooks    []AfterRequestFunc
	errorHooks    []OnErrorFunc
	shutdownHooks []OnShutdownFunc
)

func RegisterHook(h HookFunc) {
	hooks = append(hooks, hookEntry{
		priority: 50,
		fn:       h,
	})
}

func RegisterHookWithPriority(priority int, h HookFunc) {
	hooks = append(hooks, hookEntry{
		priority: priority,
		fn:       h,
	})
}

func (s *Server) ApplyHooks() {
	sort.SliceStable(hooks, func(i, j int) bool {
		return hooks[i].priority < hooks[j].priority
	})

	for _, h := range hooks {
		h.fn(s)
	}

	hooks = nil
}

func RegisterBeforeRequest(h BeforeRequestFunc) {
	beforeHooks = append(beforeHooks, h)
}

func RegisterAfterRequest(h AfterRequestFunc) {
	afterHooks = append(afterHooks, h)
}

func RegisterOnError(h OnErrorFunc) {
	errorHooks = append(errorHooks, h)
}

func RegisterOnShutdown(h OnShutdownFunc) {
	shutdownHooks = append(shutdownHooks, h)
}

func applyBeforeHooks(w http.ResponseWriter, r *http.Request) {
	for _, h := range beforeHooks {
		h(w, r)
	}
}

func applyAfterHooks(w http.ResponseWriter, r *http.Request) {
	for _, h := range afterHooks {
		h(w, r)
	}
}

func applyErrorHooks(w http.ResponseWriter, r *http.Request, err error) {
	for _, h := range errorHooks {
		h(w, r, err)
	}
}

func applyShutdownHooks() {
	for _, h := range shutdownHooks {
		h()
	}
}
