// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the module registration framework.
 */

package srmodule

import (
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testModule implements ModuleInterface for testing.
type testModule struct {
	name         string
	version      string
	exports      *ModuleExports
	initFn       func() error
	destroyFn    func() error
	initCount    int32
	destroyCount int32
	callCount    int32
	lastParams   []interface{}
	mu           sync.Mutex
}

func (m *testModule) Name() string    { return m.name }
func (m *testModule) Version() string { return m.version }

func (m *testModule) Init() error {
	atomic.AddInt32(&m.initCount, 1)
	if m.initFn != nil {
		return m.initFn()
	}
	return nil
}

func (m *testModule) Destroy() error {
	atomic.AddInt32(&m.destroyCount, 1)
	if m.destroyFn != nil {
		return m.destroyFn()
	}
	return nil
}

func (m *testModule) Exports() *ModuleExports { return m.exports }

// makeCmdFunc creates a command function that records its invocation.
func (m *testModule) makeCmdFunc(retval int) CmdFunc {
	return func(msg *parser.SIPMsg, params []interface{}) int {
		atomic.AddInt32(&m.callCount, 1)
		m.mu.Lock()
		m.lastParams = params
		m.mu.Unlock()
		return retval
	}
}

// newTestModule creates a test module with a command, parameter and item.
func newTestModule(name string) *testModule {
	m := &testModule{
		name:    name,
		version: "1.0.0",
	}
	m.exports = &ModuleExports{
		Cmds: []CmdExport{
			{
				Name:      name + "_cmd",
				Function:  m.makeCmdFunc(RetOK),
				MinParams: 0,
				MaxParams: VarParams,
				Flags:     CmdFlagRequestRoute,
			},
		},
		Params: []ParamExport{
			{
				Name:  name + "_int_param",
				Type:  ParamInt,
				Value: new(int),
			},
			{
				Name:  name + "_str_param",
				Type:  ParamStr,
				Value: new(string),
			},
			{
				Name:  name + "_bool_param",
				Type:  ParamBool,
				Value: new(bool),
			},
		},
		Items: []ItemExport{
			{
				Name: name + "_item",
				GetFunc: func(msg *parser.SIPMsg) (interface{}, error) {
					return "item_value", nil
				},
			},
		},
	}
	return m
}

// newSimpleModule creates a minimal module with no exports.
func newSimpleModule(name string) *testModule {
	return &testModule{
		name:    name,
		version: "0.1.0",
		exports: &ModuleExports{},
	}
}

// ---------------------------------------------------------------------------
// Registry creation and registration tests
// ---------------------------------------------------------------------------

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if count := r.Count(); count != 0 {
		t.Fatalf("expected 0 modules, got %d", count)
	}
	if mods := r.ListModules(); len(mods) != 0 {
		t.Fatalf("expected empty module list, got %d", len(mods))
	}
}

func TestRegisterAndFind(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("testmod")

	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if !r.HasModule("testmod") {
		t.Fatal("HasModule returned false for registered module")
	}
	if r.HasModule("nonexistent") {
		t.Fatal("HasModule returned true for unregistered module")
	}

	mod := r.FindModule("testmod")
	if mod == nil {
		t.Fatal("FindModule returned nil")
	}
	if mod.Name() != "testmod" {
		t.Fatalf("expected name testmod, got %s", mod.Name())
	}

	if count := r.Count(); count != 1 {
		t.Fatalf("expected 1 module, got %d", count)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	r := NewRegistry()
	m1 := newTestModule("dup")
	m2 := newTestModule("dup")

	if err := r.Register(m1); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	if err := r.Register(m2); err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestRegisterEmptyName(t *testing.T) {
	r := NewRegistry()
	m := &testModule{name: "", version: "1.0", exports: &ModuleExports{}}
	if err := r.Register(m); err == nil {
		t.Fatal("expected error for empty module name")
	}
}

func TestRegisterNilExports(t *testing.T) {
	r := NewRegistry()
	m := &testModule{name: "nilexp", version: "1.0", exports: nil}
	if err := r.Register(m); err != nil {
		t.Fatalf("Register with nil exports failed: %v", err)
	}
	if r.FindModule("nilexp") == nil {
		t.Fatal("module with nil exports not found")
	}
}

func TestRegisterOnLoad(t *testing.T) {
	r := NewRegistry()
	loaded := false
	m := &testModule{
		name:    "onloadmod",
		version: "1.0",
		exports: &ModuleExports{
			OnLoad: func() error {
				loaded = true
				return nil
			},
		},
	}
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if !loaded {
		t.Fatal("OnLoad was not called")
	}
}

func TestRegisterOnLoadError(t *testing.T) {
	r := NewRegistry()
	m := &testModule{
		name:    "onloaderr",
		version: "1.0",
		exports: &ModuleExports{
			OnLoad: func() error {
				return fmt.Errorf("onload failure")
			},
		},
	}
	if err := r.Register(m); err == nil {
		t.Fatal("expected error when OnLoad fails")
	}
	if r.HasModule("onloaderr") {
		t.Fatal("module should be removed after OnLoad failure")
	}
}

// ---------------------------------------------------------------------------
// Command lookup and invocation tests
// ---------------------------------------------------------------------------

func TestFindCmd(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("cmdmod")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	cmd := r.FindCmd("cmdmod_cmd")
	if cmd == nil {
		t.Fatal("FindCmd returned nil")
	}
	if cmd.Name != "cmdmod_cmd" {
		t.Fatalf("expected cmd name cmdmod_cmd, got %s", cmd.Name)
	}
	if cmd.MinParams != 0 {
		t.Fatalf("expected MinParams 0, got %d", cmd.MinParams)
	}
	if cmd.MaxParams != VarParams {
		t.Fatalf("expected MaxParams VarParams, got %d", cmd.MaxParams)
	}

	if r.FindCmd("nonexistent") != nil {
		t.Fatal("FindCmd should return nil for unknown command")
	}
}

func TestFindModuleCmd(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("modcmd")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	cmd := r.FindModuleCmd("modcmd", "modcmd_cmd")
	if cmd == nil {
		t.Fatal("FindModuleCmd returned nil")
	}
	if r.FindModuleCmd("modcmd", "nonexistent") != nil {
		t.Fatal("FindModuleCmd should return nil for unknown command")
	}
	if r.FindModuleCmd("nonexistent", "modcmd_cmd") != nil {
		t.Fatal("FindModuleCmd should return nil for unknown module")
	}
}

func TestCallCmd(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("callmod")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	ret, err := r.CallCmd("callmod_cmd", nil, []interface{}{"arg1", 42})
	if err != nil {
		t.Fatalf("CallCmd failed: %v", err)
	}
	if ret != RetOK {
		t.Fatalf("expected return %d, got %d", RetOK, ret)
	}
	if atomic.LoadInt32(&m.callCount) != 1 {
		t.Fatalf("expected call count 1, got %d", m.callCount)
	}
	m.mu.Lock()
	if len(m.lastParams) != 2 {
		t.Fatalf("expected 2 params, got %d", len(m.lastParams))
	}
	m.mu.Unlock()
}

func TestCallCmdUnknown(t *testing.T) {
	r := NewRegistry()
	ret, err := r.CallCmd("nonexistent", nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if ret != RetError {
		t.Fatalf("expected return %d, got %d", RetError, ret)
	}
}

func TestCallCmdParamCount(t *testing.T) {
	r := NewRegistry()
	m := &testModule{name: "parammod", version: "1.0"}
	m.exports = &ModuleExports{
		Cmds: []CmdExport{
			{
				Name:      "exact_cmd",
				Function:  m.makeCmdFunc(RetOK),
				MinParams: 2,
				MaxParams: 2,
			},
			{
				Name:      "range_cmd",
				Function:  m.makeCmdFunc(RetOK),
				MinParams: 1,
				MaxParams: 3,
			},
			{
				Name:      "var_cmd",
				Function:  m.makeCmdFunc(RetOK),
				MinParams: 0,
				MaxParams: VarParams,
			},
		},
	}
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Exact params
	if _, err := r.CallCmd("exact_cmd", nil, []interface{}{1, 2}); err != nil {
		t.Fatalf("exact_cmd with 2 params failed: %v", err)
	}
	if _, err := r.CallCmd("exact_cmd", nil, []interface{}{1}); err == nil {
		t.Fatal("expected error for too few params")
	}
	if _, err := r.CallCmd("exact_cmd", nil, []interface{}{1, 2, 3}); err == nil {
		t.Fatal("expected error for too many params")
	}

	// Range params
	if _, err := r.CallCmd("range_cmd", nil, []interface{}{1}); err != nil {
		t.Fatalf("range_cmd with 1 param failed: %v", err)
	}
	if _, err := r.CallCmd("range_cmd", nil, []interface{}{1, 2, 3}); err != nil {
		t.Fatalf("range_cmd with 3 params failed: %v", err)
	}
	if _, err := r.CallCmd("range_cmd", nil, nil); err == nil {
		t.Fatal("expected error for too few params")
	}
	if _, err := r.CallCmd("range_cmd", nil, []interface{}{1, 2, 3, 4}); err == nil {
		t.Fatal("expected error for too many params")
	}

	// Variable params
	if _, err := r.CallCmd("var_cmd", nil, nil); err != nil {
		t.Fatalf("var_cmd with 0 params failed: %v", err)
	}
	if _, err := r.CallCmd("var_cmd", nil, []interface{}{1, 2, 3, 4, 5}); err != nil {
		t.Fatalf("var_cmd with 5 params failed: %v", err)
	}
}

func TestCallCmdNilParams(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("nilparammod")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	ret, err := r.CallCmd("nilparammod_cmd", nil, nil)
	if err != nil {
		t.Fatalf("CallCmd with nil params failed: %v", err)
	}
	if ret != RetOK {
		t.Fatalf("expected return %d, got %d", RetOK, ret)
	}
}

// ---------------------------------------------------------------------------
// Parameter lookup and setting tests
// ---------------------------------------------------------------------------

func TestFindParam(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("parammod")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	p := r.FindParam("parammod_int_param")
	if p == nil {
		t.Fatal("FindParam returned nil")
	}
	if p.Type != ParamInt {
		t.Fatalf("expected type %s, got %s", ParamInt, p.Type)
	}

	if r.FindParam("nonexistent") != nil {
		t.Fatal("FindParam should return nil for unknown param")
	}
}

func TestFindModuleParam(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("mparammod")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	p := r.FindModuleParam("mparammod", "mparammod_int_param")
	if p == nil {
		t.Fatal("FindModuleParam returned nil")
	}
	if r.FindModuleParam("mparammod", "nonexistent") != nil {
		t.Fatal("FindModuleParam should return nil for unknown param")
	}
	if r.FindModuleParam("nonexistent", "x") != nil {
		t.Fatal("FindModuleParam should return nil for unknown module")
	}
}

func TestSetParamInt(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("setintmod")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Set via int
	if err := r.SetParam("setintmod_int_param", 42); err != nil {
		t.Fatalf("SetParam failed: %v", err)
	}
	p := r.FindParam("setintmod_int_param")
	val := p.Value.(*int)
	if *val != 42 {
		t.Fatalf("expected 42, got %d", *val)
	}

	// Set via string
	if err := r.SetParam("setintmod_int_param", "100"); err != nil {
		t.Fatalf("SetParam string failed: %v", err)
	}
	if *val != 100 {
		t.Fatalf("expected 100, got %d", *val)
	}

	// Set via hex string
	if err := r.SetParam("setintmod_int_param", "0xff"); err != nil {
		t.Fatalf("SetParam hex failed: %v", err)
	}
	if *val != 255 {
		t.Fatalf("expected 255, got %d", *val)
	}
}

func TestSetParamStr(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("setstrmod")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if err := r.SetParam("setstrmod_str_param", "hello world"); err != nil {
		t.Fatalf("SetParam failed: %v", err)
	}
	p := r.FindParam("setstrmod_str_param")
	val := p.Value.(*string)
	if *val != "hello world" {
		t.Fatalf("expected 'hello world', got %q", *val)
	}
}

func TestSetParamBool(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("setboolmod")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	cases := []struct {
		input  interface{}
		expect bool
	}{
		{true, true},
		{false, false},
		{1, true},
		{0, false},
		{"true", true},
		{"false", false},
		{"yes", true},
		{"no", false},
		{"on", true},
		{"off", false},
		{"1", true},
		{"0", false},
	}

	for _, tc := range cases {
		if err := r.SetParam("setboolmod_bool_param", tc.input); err != nil {
			t.Fatalf("SetParam(%v) failed: %v", tc.input, err)
		}
		p := r.FindParam("setboolmod_bool_param")
		val := p.Value.(*bool)
		if *val != tc.expect {
			t.Fatalf("input %v: expected %v, got %v", tc.input, tc.expect, *val)
		}
	}
}

func TestSetParamUnknown(t *testing.T) {
	r := NewRegistry()
	if err := r.SetParam("nonexistent", 42); err == nil {
		t.Fatal("expected error for unknown parameter")
	}
}

func TestSetParamInvalidConversion(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("invalidmod")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	// "abc" is not a valid int
	if err := r.SetParam("invalidmod_int_param", "abc"); err == nil {
		t.Fatal("expected error for invalid int conversion")
	}
}

func TestSetModuleParam(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("setmparammod")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if err := r.SetModuleParam("setmparammod", "setmparammod_int_param", 77); err != nil {
		t.Fatalf("SetModuleParam failed: %v", err)
	}
	p := r.FindModuleParam("setmparammod", "setmparammod_int_param")
	if *p.Value.(*int) != 77 {
		t.Fatalf("expected 77, got %d", *p.Value.(*int))
	}

	if err := r.SetModuleParam("nonexistent", "x", 1); err == nil {
		t.Fatal("expected error for unknown module")
	}
}

// ---------------------------------------------------------------------------
// Item lookup tests
// ---------------------------------------------------------------------------

func TestFindItem(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("itemmod")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	item := r.FindItem("itemmod_item")
	if item == nil {
		t.Fatal("FindItem returned nil")
	}
	if item.GetFunc == nil {
		t.Fatal("item GetFunc is nil")
	}
	val, err := item.GetFunc(nil)
	if err != nil {
		t.Fatalf("GetFunc failed: %v", err)
	}
	if val != "item_value" {
		t.Fatalf("expected 'item_value', got %v", val)
	}

	if r.FindItem("nonexistent") != nil {
		t.Fatal("FindItem should return nil for unknown item")
	}
}

// ---------------------------------------------------------------------------
// ListModules tests
// ---------------------------------------------------------------------------

func TestListModules(t *testing.T) {
	r := NewRegistry()
	m1 := newTestModule("mod1")
	m2 := newTestModule("mod2")
	m3 := newSimpleModule("mod3")

	for _, m := range []ModuleInterface{m1, m2, m3} {
		if err := r.Register(m); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	}

	mods := r.ListModules()
	if len(mods) != 3 {
		t.Fatalf("expected 3 modules, got %d", len(mods))
	}

	// Verify registration order is preserved
	if mods[0].Name != "mod1" || mods[1].Name != "mod2" || mods[2].Name != "mod3" {
		t.Fatalf("unexpected order: %s, %s, %s",
			mods[0].Name, mods[1].Name, mods[2].Name)
	}

	// Verify counts
	if mods[0].CmdCount != 1 || mods[0].ParamCount != 3 || mods[0].ItemCount != 1 {
		t.Fatalf("unexpected counts for mod1: cmds=%d params=%d items=%d",
			mods[0].CmdCount, mods[0].ParamCount, mods[0].ItemCount)
	}
	if mods[2].CmdCount != 0 || mods[2].ParamCount != 0 {
		t.Fatalf("unexpected counts for mod3: cmds=%d params=%d",
			mods[2].CmdCount, mods[2].ParamCount)
	}
}

// ---------------------------------------------------------------------------
// InitAll / DestroyAll / LoadModule tests
// ---------------------------------------------------------------------------

func TestInitAll(t *testing.T) {
	r := NewRegistry()
	m1 := newTestModule("init1")
	m2 := newTestModule("init2")
	m3 := newTestModule("init3")

	for _, m := range []ModuleInterface{m1, m2, m3} {
		if err := r.Register(m); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	}

	if err := r.InitAll(); err != nil {
		t.Fatalf("InitAll failed: %v", err)
	}

	for _, m := range []*testModule{m1, m2, m3} {
		if atomic.LoadInt32(&m.initCount) != 1 {
			t.Fatalf("module %s: expected init count 1, got %d",
				m.name, m.initCount)
		}
	}

	// InitAll again should be a no-op
	if err := r.InitAll(); err != nil {
		t.Fatalf("second InitAll failed: %v", err)
	}
	for _, m := range []*testModule{m1, m2, m3} {
		if atomic.LoadInt32(&m.initCount) != 1 {
			t.Fatalf("module %s: expected init count 1 after double init, got %d",
				m.name, m.initCount)
		}
	}
}

func TestDestroyAll(t *testing.T) {
	r := NewRegistry()
	m1 := newTestModule("dest1")
	m2 := newTestModule("dest2")

	for _, m := range []ModuleInterface{m1, m2} {
		if err := r.Register(m); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	}

	if err := r.InitAll(); err != nil {
		t.Fatalf("InitAll failed: %v", err)
	}
	if err := r.DestroyAll(); err != nil {
		t.Fatalf("DestroyAll failed: %v", err)
	}

	for _, m := range []*testModule{m1, m2} {
		if atomic.LoadInt32(&m.destroyCount) != 1 {
			t.Fatalf("module %s: expected destroy count 1, got %d",
				m.name, m.destroyCount)
		}
	}

	// DestroyAll again should be a no-op
	if err := r.DestroyAll(); err != nil {
		t.Fatalf("second DestroyAll failed: %v", err)
	}
	for _, m := range []*testModule{m1, m2} {
		if atomic.LoadInt32(&m.destroyCount) != 1 {
			t.Fatalf("module %s: expected destroy count 1 after double destroy, got %d",
				m.name, m.destroyCount)
		}
	}
}

func TestDestroyAllReverseOrder(t *testing.T) {
	r := NewRegistry()
	var order []string

	m1 := &testModule{name: "rev1", version: "1.0", exports: &ModuleExports{}}
	m1.destroyFn = func() error {
		order = append(order, "rev1")
		return nil
	}
	m2 := &testModule{name: "rev2", version: "1.0", exports: &ModuleExports{}}
	m2.destroyFn = func() error {
		order = append(order, "rev2")
		return nil
	}

	r.Register(m1)
	r.Register(m2)
	r.InitAll()
	r.DestroyAll()

	// Destroy should happen in reverse registration order
	if len(order) != 2 {
		t.Fatalf("expected 2 destroy calls, got %d", len(order))
	}
	if order[0] != "rev2" || order[1] != "rev1" {
		t.Fatalf("expected reverse order [rev2, rev1], got %v", order)
	}
}

func TestLoadModule(t *testing.T) {
	r := NewRegistry()
	m := newTestModule("loadmod")
	if err := r.Register(m); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if err := r.LoadModule("loadmod"); err != nil {
		t.Fatalf("LoadModule failed: %v", err)
	}
	if atomic.LoadInt32(&m.initCount) != 1 {
		t.Fatalf("expected init count 1, got %d", m.initCount)
	}

	// LoadModule again should be a no-op
	if err := r.LoadModule("loadmod"); err != nil {
		t.Fatalf("second LoadModule failed: %v", err)
	}
	if atomic.LoadInt32(&m.initCount) != 1 {
		t.Fatalf("expected init count 1 after double load, got %d", m.initCount)
	}

	// InitAll after LoadModule should be a no-op for this module
	if err := r.InitAll(); err != nil {
		t.Fatalf("InitAll failed: %v", err)
	}
	if atomic.LoadInt32(&m.initCount) != 1 {
		t.Fatalf("expected init count 1 after InitAll, got %d", m.initCount)
	}
}

func TestLoadModuleUnknown(t *testing.T) {
	r := NewRegistry()
	if err := r.LoadModule("nonexistent"); err == nil {
		t.Fatal("expected error for unknown module")
	}
}

func TestInitAllError(t *testing.T) {
	r := NewRegistry()
	m1 := newSimpleModule("okmod")
	m2 := &testModule{name: "errmod", version: "1.0", exports: &ModuleExports{}}
	m2.initFn = func() error { return fmt.Errorf("init failure") }

	r.Register(m1)
	r.Register(m2)

	err := r.InitAll()
	if err == nil {
		t.Fatal("expected error from InitAll")
	}
}

func TestInitAllPanic(t *testing.T) {
	r := NewRegistry()
	m := &testModule{name: "panicmod", version: "1.0", exports: &ModuleExports{}}
	m.initFn = func() error { panic("init panic") }
	r.Register(m)

	err := r.InitAll()
	if err == nil {
		t.Fatal("expected error from panic recovery")
	}
}

func TestDestroyAllPanic(t *testing.T) {
	r := NewRegistry()
	m := &testModule{name: "panicdest", version: "1.0", exports: &ModuleExports{}}
	m.destroyFn = func() error { panic("destroy panic") }
	r.Register(m)
	r.InitAll()

	err := r.DestroyAll()
	if err == nil {
		t.Fatal("expected error from panic recovery")
	}
}

// ---------------------------------------------------------------------------
// Default registry singleton tests
// ---------------------------------------------------------------------------

func TestDefaultRegistry(t *testing.T) {
	Init()
	r1 := DefaultRegistry()
	r2 := DefaultRegistry()
	if r1 != r2 {
		t.Fatal("DefaultRegistry should return same instance")
	}

	// After Init, should be a new instance
	Init()
	r3 := DefaultRegistry()
	if r1 == r3 {
		t.Fatal("DefaultRegistry should be different after Init")
	}
}

func TestPackageLevelHelpers(t *testing.T) {
	Init()
	m := newTestModule("pkgmod")
	if err := Register(m); err != nil {
		t.Fatalf("package Register failed: %v", err)
	}
	if !HasModule("pkgmod") {
		t.Fatal("HasModule returned false")
	}
	if FindCmd("pkgmod_cmd") == nil {
		t.Fatal("FindCmd returned nil")
	}
	if FindParam("pkgmod_int_param") == nil {
		t.Fatal("FindParam returned nil")
	}
	if FindItem("pkgmod_item") == nil {
		t.Fatal("FindItem returned nil")
	}
	if err := SetParam("pkgmod_int_param", 99); err != nil {
		t.Fatalf("SetParam failed: %v", err)
	}
	if ListModules() == nil {
		t.Fatal("ListModules returned nil")
	}
	if err := InitAll(); err != nil {
		t.Fatalf("InitAll failed: %v", err)
	}
	if err := DestroyAll(); err != nil {
		t.Fatalf("DestroyAll failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Fixup registry tests
// ---------------------------------------------------------------------------

func TestFixupRegistry(t *testing.T) {
	fr := NewFixupRegistry()
	if fr.Count() != 0 {
		t.Fatalf("expected 0 fixups, got %d", fr.Count())
	}

	called := false
	fr.RegisterFixup("custom", func(p interface{}) (interface{}, error) {
		called = true
		return p, nil
	})
	if fr.Count() != 1 {
		t.Fatalf("expected 1 fixup, got %d", fr.Count())
	}
	if fr.FindFixup("custom") == nil {
		t.Fatal("FindFixup returned nil")
	}
	if fr.FindFixup("nonexistent") != nil {
		t.Fatal("FindFixup should return nil for unknown fixup")
	}

	param := &ParamExport{
		Name:      "test",
		Type:      ParamFunc,
		Value:     "raw",
		FixupName: "custom",
	}
	if err := fr.ApplyFixup(param); err != nil {
		t.Fatalf("ApplyFixup failed: %v", err)
	}
	if !called {
		t.Fatal("fixup was not called")
	}
	if param.FixedValue != "raw" {
		t.Fatalf("expected FixedValue 'raw', got %v", param.FixedValue)
	}
}

func TestApplyFixupNoName(t *testing.T) {
	fr := NewFixupRegistry()
	param := &ParamExport{
		Name: "test",
		Type: ParamInt,
	}
	if err := fr.ApplyFixup(param); err != nil {
		t.Fatalf("ApplyFixup with no FixupName should be no-op: %v", err)
	}
}

func TestApplyFixupNotFound(t *testing.T) {
	fr := NewFixupRegistry()
	param := &ParamExport{
		Name:      "test",
		FixupName: "nonexistent",
	}
	if err := fr.ApplyFixup(param); err == nil {
		t.Fatal("expected error for unknown fixup")
	}
}

func TestDefaultFixupRegistry(t *testing.T) {
	Init()
	fr := DefaultFixupRegistry()
	if fr == nil {
		t.Fatal("DefaultFixupRegistry returned nil")
	}
	// Built-in fixups should be registered
	for _, name := range []string{"str", "int", "bool", "regex", "pvar"} {
		if fr.FindFixup(name) == nil {
			t.Fatalf("fixup %q not registered", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Built-in fixup function tests
// ---------------------------------------------------------------------------

func TestFixupStr(t *testing.T) {
	cases := []struct {
		input  interface{}
		expect string
	}{
		{"hello", "hello"},
		{[]byte("world"), "world"},
		{42, "42"},
		{int64(100), "100"},
		{true, "true"},
	}
	for _, tc := range cases {
		result, err := FixupStr(tc.input)
		if err != nil {
			t.Fatalf("FixupStr(%v) failed: %v", tc.input, err)
		}
		if result.(string) != tc.expect {
			t.Fatalf("FixupStr(%v): expected %q, got %q", tc.input, tc.expect, result)
		}
	}

	if _, err := FixupStr(nil); err == nil {
		t.Fatal("expected error for nil")
	}
	if _, err := FixupStr(complex(1, 2)); err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestFixupInt(t *testing.T) {
	cases := []struct {
		input  interface{}
		expect int
	}{
		{42, 42},
		{int8(8), 8},
		{int16(16), 16},
		{int32(32), 32},
		{int64(64), 64},
		{uint(10), 10},
		{uint8(8), 8},
		{"100", 100},
		{"0xff", 255},
		{"0b1010", 10},
		{[]byte("50"), 50},
	}
	for _, tc := range cases {
		result, err := FixupInt(tc.input)
		if err != nil {
			t.Fatalf("FixupInt(%v) failed: %v", tc.input, err)
		}
		if result.(int) != tc.expect {
			t.Fatalf("FixupInt(%v): expected %d, got %v", tc.input, tc.expect, result)
		}
	}

	if _, err := FixupInt(nil); err == nil {
		t.Fatal("expected error for nil")
	}
	if _, err := FixupInt("abc"); err == nil {
		t.Fatal("expected error for invalid int string")
	}
}

func TestFixupBool(t *testing.T) {
	cases := []struct {
		input  interface{}
		expect bool
	}{
		{true, true},
		{false, false},
		{1, true},
		{0, false},
		{int64(1), true},
		{int64(0), false},
		{"true", true},
		{"false", false},
		{"True", true},
		{"FALSE", false},
		{"yes", true},
		{"no", false},
		{"on", true},
		{"off", false},
		{"1", true},
		{"0", false},
		{[]byte("true"), true},
	}
	for _, tc := range cases {
		result, err := FixupBool(tc.input)
		if err != nil {
			t.Fatalf("FixupBool(%v) failed: %v", tc.input, err)
		}
		if result.(bool) != tc.expect {
			t.Fatalf("FixupBool(%v): expected %v, got %v", tc.input, tc.expect, result)
		}
	}

	if _, err := FixupBool(nil); err == nil {
		t.Fatal("expected error for nil")
	}
	if _, err := FixupBool("maybe"); err == nil {
		t.Fatal("expected error for invalid bool string")
	}
}

func TestFixupRegex(t *testing.T) {
	result, err := FixupRegex("^test[0-9]+$")
	if err != nil {
		t.Fatalf("FixupRegex failed: %v", err)
	}
	re, ok := result.(*regexp.Regexp)
	if !ok {
		t.Fatalf("expected *regexp.Regexp, got %T", result)
	}
	if !re.MatchString("test123") {
		t.Fatal("regex should match 'test123'")
	}
	if re.MatchString("nope") {
		t.Fatal("regex should not match 'nope'")
	}

	// Byte slice input
	result2, err := FixupRegex([]byte("[a-z]+"))
	if err != nil {
		t.Fatalf("FixupRegex []byte failed: %v", err)
	}
	if _, ok := result2.(*regexp.Regexp); !ok {
		t.Fatalf("expected *regexp.Regexp, got %T", result2)
	}

	if _, err := FixupRegex("[invalid"); err == nil {
		t.Fatal("expected error for invalid regex")
	}
	if _, err := FixupRegex(nil); err == nil {
		t.Fatal("expected error for nil")
	}
}

func TestFixupPVar(t *testing.T) {
	cases := []string{
		"$avp(name)",
		"$var(x)",
		"$rU",
		"  $fu  ",
	}
	for _, tc := range cases {
		result, err := FixupPVar(tc)
		if err != nil {
			t.Fatalf("FixupPVar(%q) failed: %v", tc, err)
		}
		s, ok := result.(string)
		if !ok {
			t.Fatalf("expected string, got %T", result)
		}
		if s == "" {
			t.Fatalf("FixupPVar(%q) returned empty string", tc)
		}
	}

	if _, err := FixupPVar("no_dollar"); err == nil {
		t.Fatal("expected error for missing $ prefix")
	}
	if _, err := FixupPVar(""); err == nil {
		t.Fatal("expected error for empty string")
	}
	if _, err := FixupPVar(nil); err == nil {
		t.Fatal("expected error for nil")
	}
}

func TestFixupFree(t *testing.T) {
	// FixupFree is a no-op in Go; just verify it doesn't panic.
	FixupFree(nil)
	FixupFree("string")
	FixupFree(42)
	FixupFree(regexp.MustCompile("test"))
}

// ---------------------------------------------------------------------------
// End-to-end fixup integration test
// ---------------------------------------------------------------------------

func TestApplyFixupEndToEnd(t *testing.T) {
	Init()
	fr := DefaultFixupRegistry()

	param := &ParamExport{
		Name:      "regex_param",
		Type:      ParamFunc,
		Value:     "^test[0-9]+$",
		FixupName: "regex",
	}
	if err := fr.ApplyFixup(param); err != nil {
		t.Fatalf("ApplyFixup failed: %v", err)
	}
	re, ok := param.FixedValue.(*regexp.Regexp)
	if !ok {
		t.Fatalf("expected *regexp.Regexp, got %T", param.FixedValue)
	}
	if !re.MatchString("test42") {
		t.Fatal("regex should match 'test42'")
	}
}

// ---------------------------------------------------------------------------
// Concurrency tests (run with -race)
// ---------------------------------------------------------------------------

func TestConcurrentRegisterAndFind(t *testing.T) {
	Init()
	r := DefaultRegistry()

	// Pre-register modules
	var modules []*testModule
	for i := 0; i < 20; i++ {
		m := newTestModule(fmt.Sprintf("cmod%d", i))
		modules = append(modules, m)
		if err := r.Register(m); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	}

	var wg sync.WaitGroup
	// Concurrent readers: FindCmd + CallCmd
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				name := fmt.Sprintf("cmod%d_cmd", idx)
				cmd := r.FindCmd(name)
				if cmd == nil {
					t.Errorf("FindCmd(%q) returned nil", name)
					return
				}
				r.CallCmd(name, nil, nil)
			}
		}(i)
	}
	// Concurrent readers: FindParam
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				name := fmt.Sprintf("cmod%d_int_param", idx)
				p := r.FindParam(name)
				if p == nil {
					t.Errorf("FindParam(%q) returned nil", name)
					return
				}
			}
		}(i)
	}
	// Concurrent readers: ListModules
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mods := r.ListModules()
				if len(mods) != 20 {
					t.Errorf("expected 20 modules, got %d", len(mods))
					return
				}
			}
		}()
	}
	wg.Wait()

	// Verify call counts
	for i, m := range modules {
		if cc := atomic.LoadInt32(&m.callCount); cc != 100 {
			t.Fatalf("module %d: expected call count 100, got %d", i, cc)
		}
	}
}

func TestConcurrentSetParam(t *testing.T) {
	Init()
	r := DefaultRegistry()

	// Register modules with int params
	for i := 0; i < 10; i++ {
		m := newTestModule(fmt.Sprintf("smod%d", i))
		if err := r.Register(m); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	}

	var wg sync.WaitGroup
	// Each goroutine writes to a different module's param
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("smod%d_int_param", idx)
			for j := 0; j < 100; j++ {
				if err := r.SetParam(name, j); err != nil {
					t.Errorf("SetParam(%q, %d) failed: %v", name, j, err)
					return
				}
			}
		}(i)
	}
	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("smod%d_int_param", idx)
			for j := 0; j < 100; j++ {
				r.FindParam(name)
			}
		}(i)
	}
	wg.Wait()
}

func TestConcurrentInitDestroy(t *testing.T) {
	Init()
	r := DefaultRegistry()

	for i := 0; i < 10; i++ {
		m := newTestModule(fmt.Sprintf("idmod%d", i))
		if err := r.Register(m); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	}

	var wg sync.WaitGroup

	// Init all
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.InitAll()
	}()

	// Concurrently call commands (should be safe even during init)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				name := fmt.Sprintf("idmod%d_cmd", idx)
				r.CallCmd(name, nil, nil)
			}
		}(i)
	}
	wg.Wait()

	// Destroy all
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.DestroyAll()
	}()

	// Concurrently list modules
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				r.ListModules()
			}
		}()
	}
	wg.Wait()
}

func TestConcurrentFixupRegistry(t *testing.T) {
	fr := NewFixupRegistry()

	var wg sync.WaitGroup
	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				name := fmt.Sprintf("fixup_%d_%d", idx, j)
				fr.RegisterFixup(name, FixupStr)
			}
		}(i)
	}
	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				fr.FindFixup("str")
				fr.Count()
			}
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Multiple modules with same command name (first wins)
// ---------------------------------------------------------------------------

func TestFirstWinsCommand(t *testing.T) {
	r := NewRegistry()

	m1 := &testModule{name: "first", version: "1.0"}
	m1.exports = &ModuleExports{
		Cmds: []CmdExport{
			{Name: "shared_cmd", Function: m1.makeCmdFunc(1), MaxParams: VarParams},
		},
	}
	m2 := &testModule{name: "second", version: "1.0"}
	m2.exports = &ModuleExports{
		Cmds: []CmdExport{
			{Name: "shared_cmd", Function: m2.makeCmdFunc(2), MaxParams: VarParams},
		},
	}

	r.Register(m1)
	r.Register(m2)

	ret, err := r.CallCmd("shared_cmd", nil, nil)
	if err != nil {
		t.Fatalf("CallCmd failed: %v", err)
	}
	// First registered module's command should be used
	if ret != 1 {
		t.Fatalf("expected return 1 from first module, got %d", ret)
	}
	if atomic.LoadInt32(&m1.callCount) != 1 {
		t.Fatalf("expected first module call count 1, got %d", m1.callCount)
	}
	if atomic.LoadInt32(&m2.callCount) != 0 {
		t.Fatalf("expected second module call count 0, got %d", m2.callCount)
	}

	// Module-scoped lookup should find the second module's command
	cmd2 := r.FindModuleCmd("second", "shared_cmd")
	if cmd2 == nil {
		t.Fatal("FindModuleCmd should find second module's command")
	}
}
