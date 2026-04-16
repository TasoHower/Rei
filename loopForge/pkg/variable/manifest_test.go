package variable

import "testing"

func TestMaterializeMerge(t *testing.T) {
	persisted := &StoreSnapshot{Vars: []VarSnapshot{
		{Key: "preference_lang", Value: "zh-CN", Description: "old", Visitable: true},
		{Key: "orphan", Value: "gone", Visitable: true},
	}}
	m := &Manifest{Specs: []Spec{
		{Key: "preference_lang", Description: "语言"},
		{Key: "const_uid", Description: "用户"},
	}}
	s := Materialize(m, persisted, map[string]any{"const_uid": "u1"}, WithDropOrphans(true))
	if _, ok := s.Get("orphan"); ok {
		t.Fatal("orphan should be dropped")
	}
	v, _ := s.Get("preference_lang")
	if v != "zh-CN" {
		t.Fatalf("preference_lang = %v", v)
	}
	u, _ := s.Get("const_uid")
	if u != "u1" {
		t.Fatalf("const_uid = %v", u)
	}
}

func TestMaterializeDefineDefault(t *testing.T) {
	m := &Manifest{Specs: []Spec{
		{Key: "x", Description: "dx", Default: float64(1)},
	}}
	s := Materialize(m, nil, nil)
	v, ok := s.Get("x")
	if !ok || v != float64(1) {
		t.Fatalf("got %v %v", v, ok)
	}
}
