// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for xavp package
 */

package xavp

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/str"
)

func TestNewXAVP(t *testing.T) {
	name := str.Mk("test")
	x := NewXAVP(name)
	if x == nil {
		t.Fatal("NewXAVP returned nil")
	}
	if x.Name.String() != "test" {
		t.Errorf("expected name 'test', got '%s'", x.Name.String())
	}
	if x.Type != XAVPTypeNull {
		t.Errorf("expected type Null, got %d", x.Type)
	}
}

func TestNewXAVPStr(t *testing.T) {
	name := str.Mk("name")
	val := str.Mk("value")
	x := NewXAVPStr(name, val)
	if x.Type != XAVPTypeStr {
		t.Errorf("expected type Str, got %d", x.Type)
	}
	s := x.GetStr()
	if s.String() != "value" {
		t.Errorf("expected 'value', got '%s'", s.String())
	}
}

func TestNewXAVPInt(t *testing.T) {
	name := str.Mk("count")
	x := NewXAVPInt(name, 42)
	if x.Type != XAVPTypeInt {
		t.Errorf("expected type Int, got %d", x.Type)
	}
	v, ok := x.GetInt()
	if !ok {
		t.Error("GetInt failed")
	}
	if v != 42 {
		t.Errorf("expected 42, got %d", v)
	}
}

func TestNewXAVPXAVP(t *testing.T) {
	name := str.Mk("nested")
	x := NewXAVPXAVP(name)
	if x.Type != XAVPTypeXAVP {
		t.Errorf("expected type XAVP, got %d", x.Type)
	}
}

func TestXAVPSetStr(t *testing.T) {
	x := NewXAVP(str.Mk("test"))
	x.SetStr(str.Mk("hello"))
	if x.Type != XAVPTypeStr {
		t.Error("type should be Str after SetStr")
	}
	s := x.GetStr()
	if s.String() != "hello" {
		t.Errorf("expected 'hello', got '%s'", s.String())
	}
}

func TestXAVPSetInt(t *testing.T) {
	x := NewXAVP(str.Mk("test"))
	x.SetInt(100)
	if x.Type != XAVPTypeInt {
		t.Error("type should be Int after SetInt")
	}
	v, ok := x.GetInt()
	if !ok || v != 100 {
		t.Errorf("expected 100, got %d (ok=%v)", v, ok)
	}
}

func TestXAVPAddChild(t *testing.T) {
	parent := NewXAVPXAVP(str.Mk("parent"))
	child := NewXAVPStr(str.Mk("child1"), str.Mk("val1"))
	parent.AddChild(child)

	if parent.ChildrenCount() != 1 {
		t.Errorf("expected 1 child, got %d", parent.ChildrenCount())
	}

	c := parent.GetChild(str.Mk("child1"))
	if c == nil {
		t.Fatal("GetChild returned nil")
	}
	if c.GetStr().String() != "val1" {
		t.Errorf("expected 'val1', got '%s'", c.GetStr().String())
	}
}

func TestXAVPGetAllChildren(t *testing.T) {
	parent := NewXAVPXAVP(str.Mk("parent"))
	parent.AddChild(NewXAVPStr(str.Mk("same"), str.Mk("v1")))
	parent.AddChild(NewXAVPStr(str.Mk("same"), str.Mk("v2")))
	parent.AddChild(NewXAVPStr(str.Mk("other"), str.Mk("v3")))

	children := parent.GetAllChildren(str.Mk("same"))
	if len(children) != 2 {
		t.Errorf("expected 2 children, got %d", len(children))
	}
}

func TestXAVPRemoveChild(t *testing.T) {
	parent := NewXAVPXAVP(str.Mk("parent"))
	parent.AddChild(NewXAVPStr(str.Mk("a"), str.Mk("1")))
	parent.AddChild(NewXAVPStr(str.Mk("b"), str.Mk("2")))
	parent.AddChild(NewXAVPStr(str.Mk("a"), str.Mk("3")))

	if parent.ChildrenCount() != 3 {
		t.Errorf("expected 3 children, got %d", parent.ChildrenCount())
	}

	removed := parent.RemoveChild(str.Mk("a"))
	if !removed {
		t.Error("RemoveChild should return true")
	}

	if parent.ChildrenCount() != 1 {
		t.Errorf("expected 1 child after remove, got %d", parent.ChildrenCount())
	}
}

func TestXAVPClone(t *testing.T) {
	parent := NewXAVPXAVP(str.Mk("parent"))
	parent.AddChild(NewXAVPStr(str.Mk("child"), str.Mk("val")))

	clone := parent.Clone()
	if clone.Name.String() != "parent" {
		t.Error("clone name mismatch")
	}
	if clone.ChildrenCount() != 1 {
		t.Errorf("clone should have 1 child, got %d", clone.ChildrenCount())
	}

	child := clone.GetChild(str.Mk("child"))
	if child == nil || child.GetStr().String() != "val" {
		t.Error("clone child value mismatch")
	}
}

func TestXAVPString(t *testing.T) {
	x1 := NewXAVPStr(str.Mk("s"), str.Mk("hello"))
	if x1.String() != "hello" {
		t.Errorf("expected 'hello', got '%s'", x1.String())
	}

	x2 := NewXAVPInt(str.Mk("i"), 123)
	if x2.String() != "123" {
		t.Errorf("expected '123', got '%s'", x2.String())
	}

	x3 := NewXAVPXAVP(str.Mk("x"))
	if x3.String() == "" {
		t.Error("XAVP type String should not be empty")
	}

	x4 := NewXAVP(str.Mk("n"))
	if x4.String() != "<null>" {
		t.Errorf("expected '<null>', got '%s'", x4.String())
	}
}

func TestXAVPList(t *testing.T) {
	list := NewXAVPList()
	if list.Len() != 0 {
		t.Error("new list should be empty")
	}

	list.Add(NewXAVPStr(str.Mk("a"), str.Mk("1")))
	list.Add(NewXAVPStr(str.Mk("b"), str.Mk("2")))

	if list.Len() != 2 {
		t.Errorf("expected length 2, got %d", list.Len())
	}

	a := list.Get(str.Mk("a"))
	if a == nil || a.GetStr().String() != "1" {
		t.Error("Get(a) failed")
	}

	head := list.Head()
	if head == nil || head.Name.String() != "a" {
		t.Error("Head should be 'a'")
	}
}

func TestXAVPListRemove(t *testing.T) {
	list := NewXAVPList()
	list.Add(NewXAVPStr(str.Mk("a"), str.Mk("1")))
	list.Add(NewXAVPStr(str.Mk("b"), str.Mk("2")))
	list.Add(NewXAVPStr(str.Mk("a"), str.Mk("3")))

	removed := list.Remove(str.Mk("a"))
	if !removed {
		t.Error("Remove should return true")
	}

	if list.Len() != 1 {
		t.Errorf("expected length 1, got %d", list.Len())
	}

	b := list.Get(str.Mk("b"))
	if b == nil {
		t.Error("b should still exist")
	}
}

func TestXAVPListClear(t *testing.T) {
	list := NewXAVPList()
	list.Add(NewXAVPStr(str.Mk("a"), str.Mk("1")))
	list.Clear()

	if list.Len() != 0 {
		t.Errorf("expected length 0 after clear, got %d", list.Len())
	}
	if list.Head() != nil {
		t.Error("head should be nil after clear")
	}
}

func TestXAVPStore(t *testing.T) {
	store := NewXAVPStore()

	name := str.Mk("mylist")
	x := NewXAVPStr(str.Mk("item"), str.Mk("value"))
	store.AddXAVP(name, x)

	list := store.GetList(name)
	if list == nil {
		t.Fatal("GetList returned nil")
	}
	if list.Len() != 1 {
		t.Errorf("expected 1 item, got %d", list.Len())
	}

	retrieved := store.GetXAVP(name)
	if retrieved == nil {
		t.Error("GetXAVP returned nil")
	}
}

func TestXAVPStoreRemoveList(t *testing.T) {
	store := NewXAVPStore()
	name := str.Mk("test")
	store.AddXAVP(name, NewXAVP(str.Mk("x")))

	removed := store.RemoveList(name)
	if !removed {
		t.Error("RemoveList should return true")
	}

	removed2 := store.RemoveList(name)
	if removed2 {
		t.Error("RemoveList should return false for non-existent list")
	}
}

func TestXAVPCheckName(t *testing.T) {
	x := NewXAVP(str.Mk("myname"))
	if !XAVPCheckName(x, str.Mk("myname")) {
		t.Error("XAVPCheckName should return true")
	}
	if XAVPCheckName(x, str.Mk("other")) {
		t.Error("XAVPCheckName should return false for wrong name")
	}
	if XAVPCheckName(nil, str.Mk("x")) {
		t.Error("XAVPCheckName should return false for nil xavp")
	}
}

func TestXAVPSetValue(t *testing.T) {
	x := NewXAVP(str.Mk("test"))

	if XAVPSetValue(x, str.Mk("hello")) != 0 {
		t.Error("SetValue with str.Str should succeed")
	}

	if XAVPSetValue(x, "world") != 0 {
		t.Error("SetValue with string should succeed")
	}

	if XAVPSetValue(x, 123) != 0 {
		t.Error("SetValue with int should succeed")
	}

	if XAVPSetValue(x, int64(456)) != 0 {
		t.Error("SetValue with int64 should succeed")
	}

	if XAVPSetValue(x, 3.14) == 0 {
		t.Error("SetValue with float should fail")
	}

	if XAVPSetValue(nil, "test") == 0 {
		t.Error("SetValue with nil xavp should fail")
	}
}

func TestXAVPGetValue(t *testing.T) {
	x1 := NewXAVPStr(str.Mk("s"), str.Mk("hello"))
	v1 := XAVPGetValue(x1)
	if v1 == nil {
		t.Error("GetValue should not return nil for str xavp")
	}

	x2 := NewXAVPInt(str.Mk("i"), 789)
	v2 := XAVPGetValue(x2)
	if v2 == nil {
		t.Error("GetValue should not return nil for int xavp")
	}

	x3 := NewXAVP(str.Mk("n"))
	v3 := XAVPGetValue(x3)
	if v3 != nil {
		t.Error("GetValue should return nil for null xavp")
	}

	v4 := XAVPGetValue(nil)
	if v4 != nil {
		t.Error("GetValue should return nil for nil xavp")
	}
}

func TestXAVPGetIntWrongType(t *testing.T) {
	x := NewXAVPStr(str.Mk("s"), str.Mk("hello"))
	_, ok := x.GetInt()
	if ok {
		t.Error("GetInt on str xavp should return false")
	}
}

func TestXAVPGetStrWrongType(t *testing.T) {
	x := NewXAVPInt(str.Mk("i"), 42)
	s := x.GetStr()
	if !s.IsEmpty() {
		t.Error("GetStr on int xavp should return empty")
	}
}
