package api

import "testing"

func TestForgetSelf(t *testing.T) {
	e := newEnv(t, fakeAdapter{}, nil)
	other := e.addDevice(t, "tablet")

	if w := e.do("DELETE", "/v1/devices/self", nil); w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/v1/info", nil); w.Code != 401 {
		t.Errorf("token still works after unpairing: %d", w.Code)
	}
	// Only the caller is removed.
	if w := e.do("GET", "/v1/info", map[string]string{"Authorization": "Bearer " + other}); w.Code != 200 {
		t.Errorf("other device: status %d", w.Code)
	}
	list, _ := e.store.List()
	if len(list) != 1 || list[0].Name != "tablet" {
		t.Errorf("devices = %+v", list)
	}
}

func TestForgetSelfNeedsToken(t *testing.T) {
	e := newEnv(t, fakeAdapter{}, nil)
	if w := e.do("DELETE", "/v1/devices/self", map[string]string{"Authorization": ""}); w.Code != 401 {
		t.Errorf("status %d", w.Code)
	}
	if list, _ := e.store.List(); len(list) != 1 {
		t.Errorf("a request without a token removed a device: %+v", list)
	}
}
