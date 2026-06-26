package manifest

import "testing"

func TestHookUnmarshalBothForms(t *testing.T) {
	data := []byte(`schema: 1
name: shop
template: plain
hooks:
  post_up:
    - echo hi
    - run: migrate
      service: app
      when: once
      ignore_failure: true
`)
	m, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Hooks.PostUp) != 2 {
		t.Fatalf("got %d post_up hooks, want 2", len(m.Hooks.PostUp))
	}
	if h := m.Hooks.PostUp[0]; h.Run != "echo hi" {
		t.Errorf("scalar form = %+v, want Run=echo hi", h)
	}
	if h := m.Hooks.PostUp[1]; h.Run != "migrate" || h.Service != "app" || h.When != "once" || !h.IgnoreFailure {
		t.Errorf("mapping form = %+v", h)
	}
}

func TestHookInvalidWhenRejected(t *testing.T) {
	data := []byte(`schema: 1
name: shop
template: plain
hooks:
  post_up:
    - run: x
      when: sometimes
`)
	if _, err := Parse(data); err == nil {
		t.Error("expected an error for an invalid when value")
	}
}

func TestHookEmptyRunRejected(t *testing.T) {
	data := []byte(`schema: 1
name: shop
template: plain
hooks:
  post_create:
    - service: app
`)
	if _, err := Parse(data); err == nil {
		t.Error("expected an error for a hook with no run command")
	}
}
