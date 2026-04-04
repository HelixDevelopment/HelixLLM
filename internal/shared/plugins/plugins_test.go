package plugins_test

import (
	"errors"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/plugins"
)

// fakePlugin is a test double for Plugin.
type fakePlugin struct {
	name        string
	startErr    error
	stopErr     error
	healthErr   error
	startCalled bool
	stopCalled  bool
}

func (f *fakePlugin) Name() string { return f.name }
func (f *fakePlugin) Init(_ map[string]interface{}) error {
	return nil
}
func (f *fakePlugin) Start() error {
	f.startCalled = true
	return f.startErr
}
func (f *fakePlugin) Stop() error {
	f.stopCalled = true
	return f.stopErr
}
func (f *fakePlugin) HealthCheck() error {
	return f.healthErr
}

func TestManager_Register(t *testing.T) {
	m := plugins.NewManager()

	p := &fakePlugin{name: "alpha"}
	if err := m.Register(p); err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}

	names := m.List()
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("List() = %v, want [alpha]", names)
	}
}

func TestManager_Register_Duplicate(t *testing.T) {
	m := plugins.NewManager()

	p := &fakePlugin{name: "dup"}
	if err := m.Register(p); err != nil {
		t.Fatalf("first Register() error: %v", err)
	}
	if err := m.Register(p); err == nil {
		t.Error("second Register() should fail for duplicate name")
	}
}

func TestManager_Register_Nil(t *testing.T) {
	m := plugins.NewManager()
	if err := m.Register(nil); err == nil {
		t.Error("Register(nil) should return an error")
	}
}

func TestManager_List_Empty(t *testing.T) {
	m := plugins.NewManager()
	if names := m.List(); len(names) != 0 {
		t.Errorf("List() on empty manager = %v, want []", names)
	}
}

func TestManager_List_Order(t *testing.T) {
	m := plugins.NewManager()
	for _, name := range []string{"a", "b", "c"} {
		if err := m.Register(&fakePlugin{name: name}); err != nil {
			t.Fatalf("Register(%q) error: %v", name, err)
		}
	}

	names := m.List()
	want := []string{"a", "b", "c"}
	if len(names) != len(want) {
		t.Fatalf("List() len = %d, want %d", len(names), len(want))
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("List()[%d] = %q, want %q", i, n, want[i])
		}
	}
}

func TestManager_StartAll(t *testing.T) {
	m := plugins.NewManager()
	p1 := &fakePlugin{name: "p1"}
	p2 := &fakePlugin{name: "p2"}
	m.Register(p1) //nolint:errcheck
	m.Register(p2) //nolint:errcheck

	if err := m.StartAll(); err != nil {
		t.Fatalf("StartAll() unexpected error: %v", err)
	}
	if !p1.startCalled {
		t.Error("p1.Start() was not called")
	}
	if !p2.startCalled {
		t.Error("p2.Start() was not called")
	}
}

func TestManager_StartAll_Error(t *testing.T) {
	m := plugins.NewManager()
	errBoom := errors.New("boom")
	m.Register(&fakePlugin{name: "bad", startErr: errBoom}) //nolint:errcheck
	m.Register(&fakePlugin{name: "ok"})                     //nolint:errcheck

	err := m.StartAll()
	if err == nil {
		t.Fatal("StartAll() should return error when a plugin fails to start")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("StartAll() error = %v, want to wrap %v", err, errBoom)
	}
}

func TestManager_StopAll(t *testing.T) {
	m := plugins.NewManager()
	p1 := &fakePlugin{name: "p1"}
	p2 := &fakePlugin{name: "p2"}
	m.Register(p1) //nolint:errcheck
	m.Register(p2) //nolint:errcheck

	if err := m.StopAll(); err != nil {
		t.Fatalf("StopAll() unexpected error: %v", err)
	}
	if !p1.stopCalled {
		t.Error("p1.Stop() was not called")
	}
	if !p2.stopCalled {
		t.Error("p2.Stop() was not called")
	}
}

func TestManager_StopAll_Error(t *testing.T) {
	m := plugins.NewManager()
	errStop := errors.New("stop-fail")
	m.Register(&fakePlugin{name: "bad", stopErr: errStop}) //nolint:errcheck

	err := m.StopAll()
	if err == nil {
		t.Fatal("StopAll() should return error when a plugin fails to stop")
	}
	if !errors.Is(err, errStop) {
		t.Errorf("StopAll() error = %v, want to wrap %v", err, errStop)
	}
}

func TestManager_HealthCheckAll(t *testing.T) {
	m := plugins.NewManager()
	errSick := errors.New("sick")
	m.Register(&fakePlugin{name: "healthy"})                  //nolint:errcheck
	m.Register(&fakePlugin{name: "sick", healthErr: errSick}) //nolint:errcheck

	results := m.HealthCheckAll()

	if results["healthy"] != nil {
		t.Errorf("healthy plugin result = %v, want nil", results["healthy"])
	}
	if !errors.Is(results["sick"], errSick) {
		t.Errorf("sick plugin result = %v, want %v", results["sick"], errSick)
	}
}

func TestManager_HealthCheckAll_Empty(t *testing.T) {
	m := plugins.NewManager()
	results := m.HealthCheckAll()
	if len(results) != 0 {
		t.Errorf("HealthCheckAll() on empty manager = %v, want empty map", results)
	}
}
